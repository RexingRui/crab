package service

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf8"

	"crab-order/internal/errs"
	"crab-order/internal/model"
	"crab-order/internal/timex"
)

// 买家自助登记。和卖家录单最大的差别只有一条：
//
// **单价绝不接受买家传值**，只收 spec_id + quantity，价格由服务端回查 specs 表填进快照。
// 录单接口的 unit_price 是客户端给的（卖家可以临时改价），这条路径照抄过来就等于买家自己定价。

const (
	// regMaxItems 一次登记最多几档。自由搭配也就是混几档，10 档够了。
	regMaxItems = 10
	// regMaxQuantity 单档的数量上限（套餐是盒数），挡住手滑多按几个 0。
	regMaxQuantity = 200
	// regDedupWindow 手机号查重的时间窗口：一天内同号只收一次。
	regDedupWindow = 24 * 60 * 60
)

// ErrDuplicateRegistration 同一手机号一天内重复登记。
// 只告诉买家「已经登记过」，不回单号：链接可能被转发，不能让持链接的人拿任意手机号
// 反查出别人的单号（拿到单号 + 手机号就能在查单页看到脱敏详情）。
var ErrDuplicateRegistration = errs.New(errs.CodeIdempotent,
	"这个手机号今天已经登记过了，要改或者要再订一份，说一声就行")

// RegistrationItemInput 买家选的一档。没有 unit_price，故意的。
type RegistrationItemInput struct {
	SpecID int64
	// Quantity 套餐是几盒，按只卖的档是几只。
	Quantity int
	// MaleCount 套餐里公的只数，母的 = PackSize - MaleCount。
	// nil 表示买家没动过比例，用默认的一半一半。非套餐的档忽略这个值。
	MaleCount *int
}

// RegistrationInput 买家提交的登记内容。JTI 与 Issuer 来自链接里的 token，不是买家填的。
type RegistrationInput struct {
	JTI    string
	Issuer string

	ReceiverName string
	Phone        string
	Address      string
	WechatNick   string
	// WechatRemark 卖家生成链接时预填的备注名，买家改不了。
	WechatRemark string

	Items          []RegistrationItemInput
	ExpectShipDate string
	Remark         string
}

// regRequestID 把 token 的 jti 变成建单幂等键。
// 一个链接只能落一单靠的就是 orders 上 request_id 的唯一索引：
// 同一条链接第二次提交会命中幂等，返回第一次那笔单，而不是再建一笔。
func regRequestID(jti string) string { return "reg:" + jti }

// CreateRegistration 受理买家自助登记，落成一笔待发货订单（source=web）。
// 第二个返回值表示是否命中幂等（同一条链接重复提交）。
func (s *OrderService) CreateRegistration(ctx context.Context, in RegistrationInput) (*model.Order, bool, error) {
	if in.JTI == "" {
		return nil, false, errs.InvalidParam("登记链接不完整")
	}
	if err := validateReceiver(in.ReceiverName, in.Phone, in.Address); err != nil {
		return nil, false, err
	}
	if n := utf8.RuneCountInString(in.WechatNick); n > 32 {
		return nil, false, errs.InvalidParam("wechat_nick 最长 32 字符")
	}
	if n := utf8.RuneCountInString(in.Remark); n > 200 {
		return nil, false, errs.InvalidParam("remark 最长 200 字符")
	}
	if err := validateExpectDate(in.ExpectShipDate); err != nil {
		return nil, false, err
	}
	// 买家填的希望发货日不能是过去的日子。卖家补录昨天发的货是正常操作，买家往回填不是。
	if in.ExpectShipDate != "" && in.ExpectShipDate < timex.DateStr(s.now()) {
		return nil, false, errs.InvalidParam("希望发货日期不能早于今天")
	}

	items, err := s.resolveRegItems(ctx, in.Items)
	if err != nil {
		return nil, false, err
	}

	// 幂等要排在查重前面：同一条链接重复提交（刷新成功页、连点两下）应该拿回原来那笔单，
	// 而不是被手机号查重拦下来报「已经登记过」。
	requestID := regRequestID(in.JTI)
	existing, err := s.st.GetOrderByRequestID(ctx, requestID)
	if err == nil {
		if err := s.loadDetail(ctx, s.st, existing); err != nil {
			return nil, false, err
		}
		return existing, true, nil
	} else if !errors.Is(err, errs.ErrNotFound) {
		return nil, false, errs.Internal(err)
	}

	// 手机号查重：挡的是「拿了两条链接重复填」和误提交。
	n, err := s.st.CountOrdersByPhoneSince(ctx, in.Phone, s.now()-regDedupWindow)
	if err != nil {
		return nil, false, errs.Internal(err)
	}
	if n > 0 {
		return nil, false, ErrDuplicateRegistration
	}

	return s.CreateOrder(ctx, CreateOrderInput{
		RequestID:      requestID,
		ReceiverName:   in.ReceiverName,
		Phone:          in.Phone,
		Address:        in.Address,
		WechatNick:     in.WechatNick,
		WechatRemark:   in.WechatRemark,
		Items:          items,
		ExpectShipDate: in.ExpectShipDate,
		Remark:         in.Remark,
		Source:         model.SourceWeb,
		Operator:       regOperator(in.Issuer),
	})
}

// regOperator 操作流水里记清楚这笔单是买家自己填的，以及是谁发的链接。
func regOperator(issuer string) string {
	if issuer == "" {
		return "buyer"
	}
	return "buyer via " + issuer
}

// resolveRegItems 把买家选的 spec_id + quantity 翻译成明细快照。
// 单价、规格名、克数、单位全部取自 specs 表当前值，买家传什么都不看。
func (s *OrderService) resolveRegItems(ctx context.Context, in []RegistrationItemInput) ([]ItemInput, error) {
	if len(in) == 0 {
		return nil, errs.InvalidParam("至少选一档")
	}
	if len(in) > regMaxItems {
		return nil, errs.InvalidParam("最多选 %d 档", regMaxItems)
	}
	out := make([]ItemInput, 0, len(in))
	for i, it := range in {
		if it.Quantity < 1 || it.Quantity > regMaxQuantity {
			return nil, errs.InvalidParam("items[%d].quantity 应在 1-%d 之间", i, regMaxQuantity)
		}
		sp, err := s.st.GetSpecByID(ctx, it.SpecID)
		if err != nil {
			// 规格不存在只说「选的不在了」，不回显 id，也不区分停用与不存在。
			return nil, errs.InvalidParam("选的规格已经不在了，刷新一下再试")
		}
		if !sp.Enabled {
			return nil, errs.InvalidParam("选的规格已经不在了，刷新一下再试")
		}
		label, err := packLabel(*sp, it.MaleCount, i)
		if err != nil {
			return nil, err
		}
		out = append(out, ItemInput{
			Gender:    sp.Gender,
			SpecGram:  sp.SpecGram,
			SpecLabel: label,
			Unit:      sp.Unit,
			Quantity:  it.Quantity,
			UnitPrice: sp.UnitPrice,
		})
	}
	return out, nil
}

// packLabel 把套餐的公母比例写进明细快照。
//
// 价格不随比例变（一盒就是一盒的价），比例只是配货信息，所以它只影响 spec_label——
// 而 spec_label 本来就是快照，卖家发货时照着配就行，日后改价改档都不会动到历史订单。
// 不是套餐的档原样返回，比例传了也不看。
func packLabel(sp model.Spec, maleCount *int, idx int) (string, error) {
	if !sp.IsPack() {
		return sp.SpecLabel, nil
	}
	male := sp.PackSize / 2 // 买家没动过就是默认的一半一半
	if maleCount != nil {
		male = *maleCount
	}
	if male < 0 || male > sp.PackSize {
		return "", errs.InvalidParam("items[%d].male_count 应在 0-%d 之间", idx, sp.PackSize)
	}
	return fmt.Sprintf("%s（公%d母%d）", sp.SpecLabel, male, sp.PackSize-male), nil
}
