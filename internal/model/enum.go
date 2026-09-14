package model

// ========== 发货状态 ==========

type ShipStatus string

const (
	ShipPending   ShipStatus = "pending"
	ShipShipped   ShipStatus = "shipped"
	ShipReceived  ShipStatus = "received"
	ShipCancelled ShipStatus = "cancelled"
)

func (s ShipStatus) Valid() bool {
	switch s {
	case ShipPending, ShipShipped, ShipReceived, ShipCancelled:
		return true
	}
	return false
}

func (s ShipStatus) Text() string {
	switch s {
	case ShipPending:
		return "待发货"
	case ShipShipped:
		return "已发货"
	case ShipReceived:
		return "已收货"
	case ShipCancelled:
		return "已取消"
	}
	return string(s)
}

// shipForward 为允许的正向流转矩阵。
var shipForward = map[ShipStatus]map[ShipStatus]bool{
	ShipPending: {ShipShipped: true, ShipCancelled: true},
	ShipShipped: {ShipReceived: true, ShipCancelled: true},
}

// shipRevert 为允许的回退（误操作撤销）矩阵。
var shipRevert = map[ShipStatus]map[ShipStatus]bool{
	ShipShipped:   {ShipPending: true},
	ShipReceived:  {ShipShipped: true},
	ShipCancelled: {ShipPending: true},
}

// CanTransitShip 判断正向流转是否合法。
func CanTransitShip(from, to ShipStatus) bool {
	return shipForward[from][to]
}

// CanRevertShip 判断回退是否合法。
func CanRevertShip(from, to ShipStatus) bool {
	return shipRevert[from][to]
}

// ========== 收款状态 ==========

type PayStatus string

const (
	PayUnpaid  PayStatus = "unpaid"
	PayPartial PayStatus = "partial"
	PayPaid    PayStatus = "paid"
)

func (s PayStatus) Valid() bool {
	switch s {
	case PayUnpaid, PayPartial, PayPaid:
		return true
	}
	return false
}

func (s PayStatus) Text() string {
	switch s {
	case PayUnpaid:
		return "未付款"
	case PayPartial:
		return "部分付款"
	case PayPaid:
		return "已付清"
	}
	return string(s)
}

// CalcPayStatus 由金额推导收款状态。pay_status 只能由此函数产出，不接受客户端赋值。
func CalcPayStatus(paidAmount, payableAmount int64) PayStatus {
	switch {
	case paidAmount <= 0:
		return PayUnpaid
	case paidAmount < payableAmount:
		return PayPartial
	default:
		return PayPaid
	}
}

// ========== 蟹的性别 ==========

type Gender string

const (
	GenderMale   Gender = "male"
	GenderFemale Gender = "female"
	GenderMixed  Gender = "mixed"
)

func (g Gender) Valid() bool {
	switch g {
	case GenderMale, GenderFemale, GenderMixed:
		return true
	}
	return false
}

func (g Gender) Text() string {
	switch g {
	case GenderMale:
		return "公"
	case GenderFemale:
		return "母"
	case GenderMixed:
		return "公母"
	}
	return string(g)
}

// ========== 计量单位 ==========

type Unit string

const (
	UnitPiece Unit = "piece"
	UnitBox   Unit = "box"
	UnitJin   Unit = "jin"
)

func (u Unit) Valid() bool {
	switch u {
	case UnitPiece, UnitBox, UnitJin:
		return true
	}
	return false
}

func (u Unit) Text() string {
	switch u {
	case UnitPiece:
		return "只"
	case UnitBox:
		return "盒"
	case UnitJin:
		return "斤"
	}
	return string(u)
}

// ========== 收款方式 ==========

type PayMethod string

const (
	PayMethodWechat   PayMethod = "wechat"
	PayMethodAlipay   PayMethod = "alipay"
	PayMethodCash     PayMethod = "cash"
	PayMethodTransfer PayMethod = "transfer"
	PayMethodOther    PayMethod = "other"
)

func (m PayMethod) Valid() bool {
	switch m {
	case PayMethodWechat, PayMethodAlipay, PayMethodCash, PayMethodTransfer, PayMethodOther:
		return true
	}
	return false
}

func (m PayMethod) Text() string {
	switch m {
	case PayMethodWechat:
		return "微信"
	case PayMethodAlipay:
		return "支付宝"
	case PayMethodCash:
		return "现金"
	case PayMethodTransfer:
		return "银行转账"
	case PayMethodOther:
		return "其他"
	}
	return string(m)
}

// ========== 操作流水动作 ==========

const (
	ActionCreate  = "create"
	ActionUpdate  = "update"
	ActionShip    = "ship"
	ActionReceive = "receive"
	ActionPay     = "pay"
	ActionRefund  = "refund"
	ActionCancel  = "cancel"
	ActionDelete  = "delete"
	ActionRevert  = "revert"
)
