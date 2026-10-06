package model

import "strconv"

// Order 订单主体。金额字段单位一律为「分」，时间字段一律为 Unix 秒。
// GoodsAmount / PayableAmount / PaidAmount / PayStatus 均为派生值，
// 只能由 service 层的计算与重算函数写入，不接受客户端赋值。
type Order struct {
	ID        int64
	OrderNo   string
	RequestID string // 空串表示未使用幂等键

	ReceiverName string
	Phone        string
	Address      string
	WechatNick   string
	WechatRemark string

	GoodsAmount int64
	// FreightFee 买家承担的运费，计入应收。由卖家在填运费时确认（系统按规则给建议值）。
	FreightFee    int64
	Discount      int64
	PayableAmount int64
	PaidAmount    int64

	ShipStatus ShipStatus
	PayStatus  PayStatus

	ShipCompany string
	TrackingNo  string

	// 运费，只给卖家看。FreightCost 为 nil 表示还没填（运费待定）。
	FreightList      *int64       // 快递原价
	FreightCost      *int64       // 用券后的实付
	FreightBasis     FreightBasis // 买家补多少按哪个金额算，填运费时才有
	FreightRuleVer   string       // 建单时的补贴规则版本，之后不变
	FreightSettledAt *int64       // 和快递结清的时间，nil 表示还没结

	ExpectShipDate string // YYYY-MM-DD，空串表示未约定
	ShipTime       *int64
	ReceiveTime    *int64
	FirstPayTime   *int64
	SettledTime    *int64

	Remark string
	// Source 订单来源，空串按 manual 处理（老数据没有这一列）。
	Source    Source
	CreatedAt int64
	UpdatedAt int64
	DeletedAt *int64

	// 关联数据，按需加载
	Items []OrderItem
	Logs  []OrderLog
}

// UnpaidAmount 未收金额，可为负数（超付）。
func (o *Order) UnpaidAmount() int64 { return o.PayableAmount - o.PaidAmount }

// FreightPending 运费还没填：没取消的单在填运费之前，即使货款付清了也不算真正结清。
func (o *Order) FreightPending() bool {
	return o.FreightCost == nil && o.ShipStatus != ShipCancelled
}

// FreightSellerPart 卖家实际承担的运费 = 实付 - 买家承担。运费没填时为 0。
func (o *Order) FreightSellerPart() int64 {
	if o.FreightCost == nil {
		return 0
	}
	return *o.FreightCost - o.FreightFee
}

// CrabCount 整单一共多少只（按斤的明细不计）。
func (o *Order) CrabCount() int {
	n := 0
	for _, it := range o.Items {
		n += it.CrabCount
	}
	return n
}

// ItemsSummary 明细摘要，形如 "公4.5两×5, 母3.5两×5"，用于列表与导出。
//
// 套餐（mixed）不加「公母」前缀：一盒里公母都有，前缀说不出任何东西，
// 而它的 spec_label 本来就写着盒里装的是什么。
func (o *Order) ItemsSummary() string {
	s := ""
	for i, it := range o.Items {
		if i > 0 {
			s += ", "
		}
		if it.Gender != GenderMixed {
			s += it.Gender.Text()
		}
		s += it.SpecLabel + "×" + strconv.Itoa(it.Quantity)
	}
	return s
}

// OrderItem 订单明细。SpecLabel 与 UnitPrice 是下单时的快照，
// 不与 specs 表做外键关联——今年 4.5 两卖 88 元，明年卖 95 元，历史订单金额不能跟着变。
type OrderItem struct {
	ID        int64
	OrderID   int64
	Gender    Gender
	SpecGram  int
	SpecLabel string
	Unit      Unit
	Quantity  int
	UnitPrice int64
	Amount    int64 // = Quantity * UnitPrice
	// CrabCount 这一行折合多少只：按只就是数量，按盒是盒数 × 每盒只数，按斤为 0。
	// 存快照是因为价目表里的每盒只数日后可能改，历史订单的只数不能跟着变。
	CrabCount int
	SortNo    int
}

// OrderLog 操作流水。收款与退款也记在这里：Amount 是这一笔的金额（退款为负），
// 订单上的 paid_amount 就是这些金额累加出来的。
type OrderLog struct {
	ID        int64
	OrderID   int64
	Action    string
	Field     string
	FromValue string
	ToValue   string
	Operator  string
	Remark    string
	Amount    int64
	PayMethod PayMethod
	CreatedAt int64
}

// IsPayment 这条流水是不是一笔收款或退款。
func (l OrderLog) IsPayment() bool {
	return (l.Action == ActionPay || l.Action == ActionRefund) && l.Amount != 0
}

// Spec 价目表的一档。按只卖就是一只的价，按套餐卖就是一盒的价。
// 仅在录单与买家登记时用于填充默认值，订单明细存的是快照，不回头关联这张表。
type Spec struct {
	ID        int64
	Gender    Gender
	SpecGram  int
	SpecLabel string
	Unit      Unit
	UnitPrice int64
	// PackSize 一盒几只。0 表示这一档不是套餐（按只/按斤卖）。
	// 套餐的公母比例由买家在登记页自己定，总数固定为 PackSize，价格不随比例变。
	PackSize  int
	Enabled   bool
	SortNo    int
	UpdatedAt int64
}

// IsPack 这一档是不是按盒卖的套餐。
func (s Spec) IsPack() bool { return s.PackSize > 0 }

// Address 地址簿条目，从历史订单聚合而来，不单独建客户表。
type Address struct {
	Phone        string
	ReceiverName string
	AddressText  string
	WechatNick   string
	WechatRemark string
	OrderCount   int
	LastOrderAt  int64
}

// 排序方式
const (
	SortCreatedDesc = "created_desc"
	SortCreatedAsc  = "created_asc"
	SortExpectAsc   = "expect_asc"
)

// 订单列表的运费筛选
const (
	FreightFilterPending   = "pending"   // 还没填运费（不含已取消）
	FreightFilterUnsettled = "unsettled" // 填了运费、还没和快递结
	FreightFilterSettled   = "settled"   // 已经和快递结了
)

// OrderFilter 订单列表/导出的筛选条件，多条件 AND。
type OrderFilter struct {
	ShipStatus          []ShipStatus
	PayStatus           []PayStatus
	Source              Source
	Freight             string // 见 FreightFilter*
	Keyword             string
	ExpectShipDate      string
	ExpectShipDateStart string
	ExpectShipDateEnd   string
	CreatedStart        *int64
	CreatedEnd          *int64
	Sort                string

	Page     int
	PageSize int
}

// StatsRange 区间统计中的按规格聚合项。
type SpecStat struct {
	Gender    Gender `json:"gender"`
	SpecLabel string `json:"spec_label"`
	Unit      Unit   `json:"unit"`
	Quantity  int    `json:"quantity"`
	CrabCount int    `json:"crab_count"`
	Amount    int64  `json:"amount"`
}
