package api

import (
	"strings"

	"crab-order/internal/model"
	"crab-order/internal/timex"
)

// 这里集中构造对外的 JSON 视图：所有 *_yuan 与 *_text 派生字段都由后端统一提供，
// 避免小程序端到处硬编码枚举翻译和分转元。

type ItemDTO struct {
	ID            int64  `json:"id"`
	Gender        string `json:"gender"`
	GenderText    string `json:"gender_text"`
	SpecGram      int    `json:"spec_gram"`
	SpecLabel     string `json:"spec_label"`
	Unit          string `json:"unit"`
	UnitText      string `json:"unit_text"`
	Quantity      int    `json:"quantity"`
	UnitPrice     int64  `json:"unit_price"`
	UnitPriceYuan string `json:"unit_price_yuan"`
	Amount        int64  `json:"amount"`
	AmountYuan    string `json:"amount_yuan"`
}

type PaymentDTO struct {
	ID            int64  `json:"id"`
	Amount        int64  `json:"amount"`
	AmountYuan    string `json:"amount_yuan"`
	PayMethod     string `json:"pay_method"`
	PayMethodText string `json:"pay_method_text"`
	PaidAt        string `json:"paid_at"`
	Remark        string `json:"remark"`
}

type LogDTO struct {
	Action    string `json:"action"`
	Field     string `json:"field"`
	FromValue string `json:"from_value"`
	ToValue   string `json:"to_value"`
	Operator  string `json:"operator"`
	Remark    string `json:"remark"`
	CreatedAt string `json:"created_at"`
}

type OrderDTO struct {
	ID           int64  `json:"id"`
	OrderNo      string `json:"order_no"`
	ReceiverName string `json:"receiver_name"`
	Phone        string `json:"phone"`
	Address      string `json:"address"`
	WechatNick   string `json:"wechat_nick"`
	WechatRemark string `json:"wechat_remark"`

	Items []ItemDTO `json:"items"`

	GoodsAmount       int64  `json:"goods_amount"`
	GoodsAmountYuan   string `json:"goods_amount_yuan"`
	FreightFee        int64  `json:"freight_fee"`
	FreightFeeYuan    string `json:"freight_fee_yuan"`
	Discount          int64  `json:"discount"`
	DiscountYuan      string `json:"discount_yuan"`
	PayableAmount     int64  `json:"payable_amount"`
	PayableAmountYuan string `json:"payable_amount_yuan"`
	PaidAmount        int64  `json:"paid_amount"`
	PaidAmountYuan    string `json:"paid_amount_yuan"`
	UnpaidAmount      int64  `json:"unpaid_amount"`
	UnpaidAmountYuan  string `json:"unpaid_amount_yuan"`

	ShipStatus     string `json:"ship_status"`
	ShipStatusText string `json:"ship_status_text"`
	PayStatus      string `json:"pay_status"`
	PayStatusText  string `json:"pay_status_text"`

	ShipCompany string `json:"ship_company"`
	TrackingNo  string `json:"tracking_no"`

	ExpectShipDate *string `json:"expect_ship_date"`
	ShipTime       *string `json:"ship_time"`
	ReceiveTime    *string `json:"receive_time"`
	FirstPayTime   *string `json:"first_pay_time"`
	SettledTime    *string `json:"settled_time"`

	Remark     string `json:"remark"`
	Source     string `json:"source"`
	SourceText string `json:"source_text"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`

	Payments []PaymentDTO `json:"payments"`
	Logs     []LogDTO     `json:"logs"`

	// Idempotent 仅在建单命中幂等（request_id 重复）时出现。
	Idempotent bool `json:"idempotent,omitempty"`
}

// OrderSummaryDTO 是列表用的订单摘要：不含 items / payments / logs 全量，
// 只带一个 items_summary 字符串，显著减少列表体积。
type OrderSummaryDTO struct {
	ID           int64  `json:"id"`
	OrderNo      string `json:"order_no"`
	ReceiverName string `json:"receiver_name"`
	Phone        string `json:"phone"`
	Address      string `json:"address"`
	WechatNick   string `json:"wechat_nick"`
	WechatRemark string `json:"wechat_remark"`
	ItemsSummary string `json:"items_summary"`

	GoodsAmount       int64  `json:"goods_amount"`
	GoodsAmountYuan   string `json:"goods_amount_yuan"`
	FreightFee        int64  `json:"freight_fee"`
	FreightFeeYuan    string `json:"freight_fee_yuan"`
	Discount          int64  `json:"discount"`
	DiscountYuan      string `json:"discount_yuan"`
	PayableAmount     int64  `json:"payable_amount"`
	PayableAmountYuan string `json:"payable_amount_yuan"`
	PaidAmount        int64  `json:"paid_amount"`
	PaidAmountYuan    string `json:"paid_amount_yuan"`
	UnpaidAmount      int64  `json:"unpaid_amount"`
	UnpaidAmountYuan  string `json:"unpaid_amount_yuan"`

	ShipStatus     string `json:"ship_status"`
	ShipStatusText string `json:"ship_status_text"`
	PayStatus      string `json:"pay_status"`
	PayStatusText  string `json:"pay_status_text"`

	ShipCompany string `json:"ship_company"`
	TrackingNo  string `json:"tracking_no"`

	ExpectShipDate *string `json:"expect_ship_date"`
	ShipTime       *string `json:"ship_time"`
	ReceiveTime    *string `json:"receive_time"`
	FirstPayTime   *string `json:"first_pay_time"`
	SettledTime    *string `json:"settled_time"`

	Remark     string `json:"remark"`
	Source     string `json:"source"`
	SourceText string `json:"source_text"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

// PublicOrderDTO 是买家免登录查单的脱敏视图：不含金额、备注与详细地址。
type PublicOrderDTO struct {
	OrderNo        string  `json:"order_no"`
	ReceiverName   string  `json:"receiver_name"`
	Phone          string  `json:"phone"`
	Address        string  `json:"address"`
	ItemsSummary   string  `json:"items_summary"`
	ShipStatus     string  `json:"ship_status"`
	ShipStatusText string  `json:"ship_status_text"`
	ShipCompany    string  `json:"ship_company"`
	TrackingNo     string  `json:"tracking_no"`
	ExpectShipDate *string `json:"expect_ship_date"`
	ShipTime       *string `json:"ship_time"`
}

// PublicSpecDTO 是买家登记页看到的规格：只给选规格必需的字段，
// 不含 sort_no / updated_at 这类内部信息。
type PublicSpecDTO struct {
	ID            int64  `json:"id"`
	Gender        string `json:"gender"`
	GenderText    string `json:"gender_text"`
	SpecGram      int    `json:"spec_gram"`
	SpecLabel     string `json:"spec_label"`
	Unit          string `json:"unit"`
	UnitText      string `json:"unit_text"`
	UnitPrice     int64  `json:"unit_price"`
	UnitPriceYuan string `json:"unit_price_yuan"`
	// PackSize 一盒几只，0 表示不是套餐（按只卖）。大于 0 时登记页会让买家调公母比例。
	PackSize int `json:"pack_size"`
}

// RegistrationDTO 是买家提交登记后的回执：够他记住单号、核对自己填了什么就行，
// 不回显手机号与完整地址（页面上本来就是他自己刚填的），也不含收款信息。
type RegistrationDTO struct {
	OrderNo           string  `json:"order_no"`
	ReceiverName      string  `json:"receiver_name"`
	ItemsSummary      string  `json:"items_summary"`
	GoodsAmount       int64   `json:"goods_amount"`
	GoodsAmountYuan   string  `json:"goods_amount_yuan"`
	PayableAmount     int64   `json:"payable_amount"`
	PayableAmountYuan string  `json:"payable_amount_yuan"`
	ExpectShipDate    *string `json:"expect_ship_date"`
	CreatedAt         string  `json:"created_at"`
	// Idempotent 为 true 表示这条链接之前已经提交过，返回的是原来那笔单。
	Idempotent bool `json:"idempotent,omitempty"`
}

type SpecDTO struct {
	ID            int64  `json:"id"`
	Gender        string `json:"gender"`
	GenderText    string `json:"gender_text"`
	SpecGram      int    `json:"spec_gram"`
	SpecLabel     string `json:"spec_label"`
	Unit          string `json:"unit"`
	UnitText      string `json:"unit_text"`
	UnitPrice     int64  `json:"unit_price"`
	UnitPriceYuan string `json:"unit_price_yuan"`
	PackSize      int    `json:"pack_size"`
	Enabled       bool   `json:"enabled"`
	SortNo        int    `json:"sort_no"`
	UpdatedAt     string `json:"updated_at"`
}

type AddressDTO struct {
	Phone        string `json:"phone"`
	ReceiverName string `json:"receiver_name"`
	Address      string `json:"address"`
	WechatNick   string `json:"wechat_nick"`
	WechatRemark string `json:"wechat_remark"`
	OrderCount   int    `json:"order_count"`
	LastOrderAt  string `json:"last_order_at"`
}

// ---------- 构造函数 ----------

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func toItemDTO(it model.OrderItem) ItemDTO {
	return ItemDTO{
		ID:            it.ID,
		Gender:        string(it.Gender),
		GenderText:    it.Gender.Text(),
		SpecGram:      it.SpecGram,
		SpecLabel:     it.SpecLabel,
		Unit:          string(it.Unit),
		UnitText:      it.Unit.Text(),
		Quantity:      it.Quantity,
		UnitPrice:     it.UnitPrice,
		UnitPriceYuan: model.FormatYuan(it.UnitPrice),
		Amount:        it.Amount,
		AmountYuan:    model.FormatYuan(it.Amount),
	}
}

func toPaymentDTO(p model.Payment) PaymentDTO {
	return PaymentDTO{
		ID:            p.ID,
		Amount:        p.Amount,
		AmountYuan:    model.FormatYuan(p.Amount),
		PayMethod:     string(p.PayMethod),
		PayMethodText: p.PayMethod.Text(),
		PaidAt:        timex.Format(p.PaidAt),
		Remark:        p.Remark,
	}
}

func toLogDTO(l model.OrderLog) LogDTO {
	return LogDTO{
		Action:    l.Action,
		Field:     l.Field,
		FromValue: l.FromValue,
		ToValue:   l.ToValue,
		Operator:  l.Operator,
		Remark:    l.Remark,
		CreatedAt: timex.Format(l.CreatedAt),
	}
}

// ToOrderDTO 构造全局统一的订单对象格式。
func ToOrderDTO(o *model.Order) OrderDTO {
	items := make([]ItemDTO, 0, len(o.Items))
	for _, it := range o.Items {
		items = append(items, toItemDTO(it))
	}
	payments := make([]PaymentDTO, 0, len(o.Payments))
	for _, p := range o.Payments {
		payments = append(payments, toPaymentDTO(p))
	}
	logs := make([]LogDTO, 0, len(o.Logs))
	for _, l := range o.Logs {
		logs = append(logs, toLogDTO(l))
	}

	return OrderDTO{
		ID:           o.ID,
		OrderNo:      o.OrderNo,
		ReceiverName: o.ReceiverName,
		Phone:        o.Phone,
		Address:      o.Address,
		WechatNick:   o.WechatNick,
		WechatRemark: o.WechatRemark,
		Items:        items,

		GoodsAmount:       o.GoodsAmount,
		GoodsAmountYuan:   model.FormatYuan(o.GoodsAmount),
		FreightFee:        o.FreightFee,
		FreightFeeYuan:    model.FormatYuan(o.FreightFee),
		Discount:          o.Discount,
		DiscountYuan:      model.FormatYuan(o.Discount),
		PayableAmount:     o.PayableAmount,
		PayableAmountYuan: model.FormatYuan(o.PayableAmount),
		PaidAmount:        o.PaidAmount,
		PaidAmountYuan:    model.FormatYuan(o.PaidAmount),
		UnpaidAmount:      o.UnpaidAmount(),
		UnpaidAmountYuan:  model.FormatYuan(o.UnpaidAmount()),

		ShipStatus:     string(o.ShipStatus),
		ShipStatusText: o.ShipStatus.Text(),
		PayStatus:      string(o.PayStatus),
		PayStatusText:  o.PayStatus.Text(),

		ShipCompany: o.ShipCompany,
		TrackingNo:  o.TrackingNo,

		ExpectShipDate: nilIfEmpty(o.ExpectShipDate),
		ShipTime:       timex.FormatPtr(o.ShipTime),
		ReceiveTime:    timex.FormatPtr(o.ReceiveTime),
		FirstPayTime:   timex.FormatPtr(o.FirstPayTime),
		SettledTime:    timex.FormatPtr(o.SettledTime),

		Remark:     o.Remark,
		Source:     string(o.Source),
		SourceText: o.Source.Text(),
		CreatedAt:  timex.Format(o.CreatedAt),
		UpdatedAt:  timex.Format(o.UpdatedAt),

		Payments: payments,
		Logs:     logs,
	}
}

func ToOrderSummaryDTO(o *model.Order) OrderSummaryDTO {
	return OrderSummaryDTO{
		ID:           o.ID,
		OrderNo:      o.OrderNo,
		ReceiverName: o.ReceiverName,
		Phone:        o.Phone,
		Address:      o.Address,
		WechatNick:   o.WechatNick,
		WechatRemark: o.WechatRemark,
		ItemsSummary: o.ItemsSummary(),

		GoodsAmount:       o.GoodsAmount,
		GoodsAmountYuan:   model.FormatYuan(o.GoodsAmount),
		FreightFee:        o.FreightFee,
		FreightFeeYuan:    model.FormatYuan(o.FreightFee),
		Discount:          o.Discount,
		DiscountYuan:      model.FormatYuan(o.Discount),
		PayableAmount:     o.PayableAmount,
		PayableAmountYuan: model.FormatYuan(o.PayableAmount),
		PaidAmount:        o.PaidAmount,
		PaidAmountYuan:    model.FormatYuan(o.PaidAmount),
		UnpaidAmount:      o.UnpaidAmount(),
		UnpaidAmountYuan:  model.FormatYuan(o.UnpaidAmount()),

		ShipStatus:     string(o.ShipStatus),
		ShipStatusText: o.ShipStatus.Text(),
		PayStatus:      string(o.PayStatus),
		PayStatusText:  o.PayStatus.Text(),

		ShipCompany: o.ShipCompany,
		TrackingNo:  o.TrackingNo,

		ExpectShipDate: nilIfEmpty(o.ExpectShipDate),
		ShipTime:       timex.FormatPtr(o.ShipTime),
		ReceiveTime:    timex.FormatPtr(o.ReceiveTime),
		FirstPayTime:   timex.FormatPtr(o.FirstPayTime),
		SettledTime:    timex.FormatPtr(o.SettledTime),

		Remark:     o.Remark,
		Source:     string(o.Source),
		SourceText: o.Source.Text(),
		CreatedAt:  timex.Format(o.CreatedAt),
		UpdatedAt:  timex.Format(o.UpdatedAt),
	}
}

func ToOrderSummaryList(list []*model.Order) []OrderSummaryDTO {
	out := make([]OrderSummaryDTO, 0, len(list))
	for _, o := range list {
		out = append(out, ToOrderSummaryDTO(o))
	}
	return out
}

func ToPublicOrderDTO(o *model.Order) PublicOrderDTO {
	return PublicOrderDTO{
		OrderNo:        o.OrderNo,
		ReceiverName:   MaskName(o.ReceiverName),
		Phone:          MaskPhone(o.Phone),
		Address:        MaskAddress(o.Address),
		ItemsSummary:   o.ItemsSummary(),
		ShipStatus:     string(o.ShipStatus),
		ShipStatusText: o.ShipStatus.Text(),
		ShipCompany:    o.ShipCompany,
		TrackingNo:     o.TrackingNo,
		ExpectShipDate: nilIfEmpty(o.ExpectShipDate),
		ShipTime:       timex.FormatPtr(o.ShipTime),
	}
}

func ToPublicSpecDTO(s model.Spec) PublicSpecDTO {
	return PublicSpecDTO{
		ID:            s.ID,
		Gender:        string(s.Gender),
		GenderText:    s.Gender.Text(),
		SpecGram:      s.SpecGram,
		SpecLabel:     s.SpecLabel,
		Unit:          string(s.Unit),
		UnitText:      s.Unit.Text(),
		UnitPrice:     s.UnitPrice,
		UnitPriceYuan: model.FormatYuan(s.UnitPrice),
		PackSize:      s.PackSize,
	}
}

// ToRegistrationDTO 构造买家登记回执。买家页显示的应收是服务端算的，不是页面自己加的。
//
// idem 为 true 表示这条链接之前已经被提交过，回执里是**别人可能填的**那笔单：
// 链接会被转发，拿到转发链接的人提交一次就能看到这个回执，所以这种情况下姓名要打码。
// 首次提交回显的是他自己刚填的内容，不需要打码。
func ToRegistrationDTO(o *model.Order, idem bool) RegistrationDTO {
	name := o.ReceiverName
	if idem {
		name = MaskName(name)
	}
	return RegistrationDTO{
		OrderNo:           o.OrderNo,
		ReceiverName:      name,
		ItemsSummary:      o.ItemsSummary(),
		GoodsAmount:       o.GoodsAmount,
		GoodsAmountYuan:   model.FormatYuan(o.GoodsAmount),
		PayableAmount:     o.PayableAmount,
		PayableAmountYuan: model.FormatYuan(o.PayableAmount),
		ExpectShipDate:    nilIfEmpty(o.ExpectShipDate),
		CreatedAt:         timex.Format(o.CreatedAt),
		Idempotent:        idem,
	}
}

func ToSpecDTO(s model.Spec) SpecDTO {
	return SpecDTO{
		ID:            s.ID,
		Gender:        string(s.Gender),
		GenderText:    s.Gender.Text(),
		SpecGram:      s.SpecGram,
		SpecLabel:     s.SpecLabel,
		Unit:          string(s.Unit),
		UnitText:      s.Unit.Text(),
		UnitPrice:     s.UnitPrice,
		UnitPriceYuan: model.FormatYuan(s.UnitPrice),
		PackSize:      s.PackSize,
		Enabled:       s.Enabled,
		SortNo:        s.SortNo,
		UpdatedAt:     timex.Format(s.UpdatedAt),
	}
}

func ToAddressDTO(a model.Address) AddressDTO {
	return AddressDTO{
		Phone:        a.Phone,
		ReceiverName: a.ReceiverName,
		Address:      a.AddressText,
		WechatNick:   a.WechatNick,
		WechatRemark: a.WechatRemark,
		OrderCount:   a.OrderCount,
		LastOrderAt:  timex.Format(a.LastOrderAt),
	}
}

// ---------- 脱敏 ----------

// MaskName 只保留姓，其余打码："张三" → "张*"。
func MaskName(name string) string {
	rs := []rune(name)
	if len(rs) <= 1 {
		return name
	}
	return string(rs[0]) + strings.Repeat("*", len(rs)-1)
}

// MaskPhone 手机号中间 4 位打码："13800138000" → "138****8000"。
func MaskPhone(phone string) string {
	if len(phone) != 11 {
		return "****"
	}
	return phone[:3] + "****" + phone[7:]
}

// MaskAddress 只保留到区县级，详址不外泄。
// 优先切到第一个「区/县/旗」，没有就切到第一个「市」，再没有就截前 10 个字。
func MaskAddress(addr string) string {
	rs := []rune(addr)
	cut := -1
	for i, r := range rs {
		if r == '区' || r == '县' || r == '旗' {
			cut = i + 1
			break
		}
	}
	if cut < 0 {
		for i, r := range rs {
			if r == '市' {
				cut = i + 1
				break
			}
		}
	}
	if cut < 0 {
		cut = min(len(rs), 10)
	}
	if cut > 20 {
		cut = 20
	}
	return string(rs[:cut])
}
