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
	// RegMinCrabs 买家自助登记的起订只数。
	//
	// 只卡买家这条路径：卖家在小程序里给熟客记一只也是正常业务，不受这条限制。
	// 8 只装的套餐一盒就过线了，所以这条实际上是给「按只卖」的档兜底的。
	RegMinCrabs = 5
)

// ErrDuplicateRegistration 同一手机号一天内重复登记。
// 只告诉买家「已经登记过」，不回单号：链接可能被转发，不能让持链接的人拿任意手机号
// 反查出别人的单号（拿到单号 + 手机号就能在查单页看到脱敏详情）。
var ErrDuplicateRegistration = errs.New(errs.CodeIdempotent,
	"这个手机号今天已经登记过了，要改或者要再订一份，说一声就行")

// RegistrationItemInput 买家选的一档。没有 unit_price，故意的。
type RegistrationItemInput struct {
	SpecID int64
	// Quantity 这一档要几只（按斤卖的档就是几斤）。
	//
	// 买家只填只数，不填盒数：拆成几盒加几只散的是服务端的事。
	// 买家不该为了用这个页面，先自己算清楚凑不凑得满一盒。
	Quantity int
	// MaleCount 这一档里公的只数，母的 = Quantity - MaleCount。
	// nil 表示买家没动过比例，用默认的一半一半。按斤卖的档忽略这个值。
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

// resolveRegItems 把买家选的 spec_id + 只数翻译成明细快照。
// 单价、规格名、克数、单位全部取自 specs 表当前值，买家传什么都不看。
func (s *OrderService) resolveRegItems(ctx context.Context, in []RegistrationItemInput) ([]ItemInput, error) {
	if len(in) == 0 {
		return nil, errs.InvalidParam("至少选一档")
	}
	if len(in) > regMaxItems {
		return nil, errs.InvalidParam("最多选 %d 档", regMaxItems)
	}
	out := make([]ItemInput, 0, len(in)*2) // 一档可能拆成整盒 + 散只两条
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
		rows, err := splitPack(*sp, it.Quantity, it.MaleCount, i)
		if err != nil {
			return nil, err
		}
		out = append(out, rows...)
	}
	return out, nil
}

// LoosePrice 散买一只多少钱：整盒价按盒里的只数摊开，**向上取整到元**。
//
// 189 / 8 = 23.625 → 24 元。取到元而不是到分，一来报价好说出口，二来天然保证
// 「整盒比散买划算」（8 只散买 192 元 > 整盒 189 元），不会出现凑不满盒反而更便宜。
// 不是套餐的档没有散买这回事，返回它自己的单价。
func LoosePrice(sp model.Spec) int64 {
	if !sp.IsPack() {
		return sp.UnitPrice
	}
	perYuan := int64(sp.PackSize) * 100 // 一元 = 100 分
	return ((sp.UnitPrice + perYuan - 1) / perYuan) * 100
}

// splitPack 把「这一档要 n 只」拆成整盒 + 散只两条明细。
//
// 整盒部分走套餐价，凑不满一盒的零头走散买价。零头不是 0 就必须够起订量——
// 「最低 5 只」只管这种不按整盒买的情况，整盒买多少都行。
// 不是套餐的档没有「整盒」，整条按它自己的单价走，同样受起订量约束。
func splitPack(sp model.Spec, n int, maleCount *int, idx int) ([]ItemInput, error) {
	male := n / 2 // 买家没动过就是默认的一半一半
	if maleCount != nil {
		male = *maleCount
	}
	if male < 0 || male > n {
		return nil, errs.InvalidParam("items[%d].male_count 应在 0-%d 之间", idx, n)
	}

	base := ItemInput{
		Gender:    sp.Gender,
		SpecGram:  sp.SpecGram,
		SpecLabel: sp.SpecLabel,
		Unit:      sp.Unit,
		UnitPrice: sp.UnitPrice,
		PackSize:  sp.PackSize,
	}

	if !sp.IsPack() {
		// 按斤卖的档不论只，起订量与公母比例都不适用
		if sp.Unit != model.UnitPiece {
			row := base
			row.Quantity = n
			return []ItemInput{row}, nil
		}
		if n < RegMinCrabs {
			return nil, errs.InvalidParam("「%s」最少 %d 只起，现在只有 %d 只", sp.SpecLabel, RegMinCrabs, n)
		}
		row := base
		row.Quantity = n
		row.SpecLabel = withRatio(sp.SpecLabel, male, n-male)
		return []ItemInput{row}, nil
	}

	boxes := n / sp.PackSize
	loose := n % sp.PackSize
	if loose > 0 && loose < RegMinCrabs {
		// 顺手把最近的两个合法只数算给买家，省得他自己试
		return nil, errs.InvalidParam("「%s」散买最少 %d 只起：%d 只里有 %d 只凑不满一盒，改成 %d 只或 %d 只",
			sp.SpecLabel, RegMinCrabs, n, loose, boxes*sp.PackSize, boxes*sp.PackSize+RegMinCrabs)
	}

	boxCrabs := boxes * sp.PackSize
	maleInBox := splitMale(male, n, boxCrabs, loose)

	rows := make([]ItemInput, 0, 2)
	if boxes > 0 {
		row := base
		row.Quantity = boxes
		row.SpecLabel = withRatio(sp.SpecLabel, maleInBox, boxCrabs-maleInBox)
		rows = append(rows, row)
	}
	if loose > 0 {
		row := base
		row.Unit = model.UnitPiece // 散只按只记，单价是散买价
		row.Quantity = loose
		row.UnitPrice = LoosePrice(sp)
		row.PackSize = 0
		row.SpecLabel = withRatio(sp.SpecLabel+" 散只", male-maleInBox, loose-(male-maleInBox))
		rows = append(rows, row)
	}
	return rows, nil
}

// splitMale 把公的只数按比例分摊到整盒与散只两部分，整盒那份四舍五入。
// 卖家配货只关心这一档一共几公几母，分摊纯粹是为了两条明细各自写得出比例。
func splitMale(male, total, boxCrabs, loose int) int {
	if total <= 0 {
		return 0
	}
	inBox := (male*boxCrabs + total/2) / total
	// 夹回合法区间：盒里装不下那么多公的，散只那份也不能是负数
	if inBox > boxCrabs {
		inBox = boxCrabs
	}
	if male-inBox > loose {
		inBox = male - loose
	}
	if inBox < 0 {
		inBox = 0
	}
	return inBox
}

// withRatio 把公母只数写进明细快照。比例不影响金额，只是配货信息，
// 而 spec_label 本来就是快照——日后改价改档都不会回头动历史订单。
func withRatio(label string, male, female int) string {
	return fmt.Sprintf("%s（公%d母%d）", label, male, female)
}
