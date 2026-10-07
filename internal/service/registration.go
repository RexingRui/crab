package service

import (
	"context"
	"errors"
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
	// regMaxQuantity 单档的只数上限，挡住手滑多按几个 0。
	regMaxQuantity = 200
	// regDedupWindow 手机号查重的时间窗口：一天内同号只收一次。
	regDedupWindow = 24 * 60 * 60
	// RegMinCrabs 买家自助登记的起订只数，卡的是**整单**不是每档。
	//
	// 只卡买家这条路径：卖家给熟客记一只也是正常业务，录单接口不受这条限制。
	RegMinCrabs = 5
)

// ErrDuplicateRegistration 同一手机号一天内重复登记。
// 只告诉买家「已经登记过」，不回单号：链接可能被转发，不能让持链接的人拿任意手机号
// 反查出别人的单号（拿到单号 + 手机号就能在查单页看到脱敏详情）。
var ErrDuplicateRegistration = errs.New(errs.CodeIdempotent,
	"这个手机号今天已经登记过了，要改或者要再订一份，说一声就行")

// ErrRegistrationRevoked 这条链接登记的单已被卖家删掉。一条链接只落一单，删了也不能再用。
var ErrRegistrationRevoked = errs.New(errs.CodeStateConflict,
	"这条链接登记的订单已经被卖家撤销了，要重新订请找卖家要一条新链接")

// RegistrationItemInput 买家选的一档：哪种蟹、几只。没有单价，故意的。
type RegistrationItemInput struct {
	SpecID   int64
	Quantity int // 只数
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
	if err := checkMinCrabs(items); err != nil {
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
	if deleted, err := s.st.RequestIDDeleted(ctx, requestID); err != nil {
		return nil, false, errs.Internal(err)
	} else if deleted {
		return nil, false, ErrRegistrationRevoked
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

// resolveRegItems 把买家选的 spec_id + 只数翻译成明细快照，一档一行。
// 单价、规格名、克数、性别、品相全部取自 specs 表当前值，买家传什么都不看。
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
		out = append(out, ItemInput{
			Gender:         sp.Gender,
			SpecGram:       sp.SpecGram,
			Grade:          sp.Grade,
			SpecLabel:      sp.SpecLabel,
			Quantity:       it.Quantity,
			UnitPriceMilli: sp.UnitPriceMilli,
		})
	}
	return out, nil
}

// checkMinCrabs 起订量：**整单**至少 RegMinCrabs 只，不是每档。
//
// 逐档卡会把「这档挑 1 只、那档挑 4 只」这种正常搭配挡住，而它本来就够 5 只。
func checkMinCrabs(items []ItemInput) error {
	n := 0
	for _, it := range items {
		n += it.Quantity
	}
	if n >= RegMinCrabs {
		return nil
	}
	return errs.InvalidParam("一共 %d 只起，现在只有 %d 只", RegMinCrabs, n)
}
