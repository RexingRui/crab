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

	GoodsAmount   int64
	FreightFee    int64
	Discount      int64
	PayableAmount int64
	PaidAmount    int64

	ShipStatus ShipStatus
	PayStatus  PayStatus

	ShipCompany string
	TrackingNo  string

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
	Items    []OrderItem
	Payments []Payment
	Logs     []OrderLog
}

// UnpaidAmount 未收金额，可为负数（超付）。
func (o *Order) UnpaidAmount() int64 { return o.PayableAmount - o.PaidAmount }

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
	SortNo    int
}

// Payment 收款流水。Amount 为负数表示退款。
type Payment struct {
	ID        int64
	OrderID   int64
	Amount    int64
	PayMethod PayMethod
	PaidAt    int64
	Remark    string
	CreatedAt int64
	DeletedAt *int64
}

// OrderLog 操作流水。
type OrderLog struct {
	ID        int64
	OrderID   int64
	Action    string
	Field     string
	FromValue string
	ToValue   string
	Operator  string
	Remark    string
	CreatedAt int64
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

// OrderFilter 订单列表/导出的筛选条件，多条件 AND。
type OrderFilter struct {
	ShipStatus          []ShipStatus
	PayStatus           []PayStatus
	Source              Source
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
	Quantity  int    `json:"quantity"`
	Amount    int64  `json:"amount"`
}
