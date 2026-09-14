package service

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"unicode/utf8"

	"crab-order/internal/errs"
	"crab-order/internal/model"
	"crab-order/internal/store"
	"crab-order/internal/timex"
)

var phoneRe = regexp.MustCompile(`^1[3-9]\d{9}$`)

// errDuplicateRequest 是建单时 request_id 撞唯一索引的内部信号，不会返回给客户端。
var errDuplicateRequest = errors.New("duplicate request_id")

// OrderService 承载订单的全部业务逻辑：金额计算、状态流转、幂等、收款重算。
type OrderService struct {
	st  store.Store
	now func() int64
}

func NewOrderService(st store.Store) *OrderService {
	return &OrderService{st: st, now: timex.Now}
}

// SetClock 替换时钟，仅供测试使用。
func (s *OrderService) SetClock(f func() int64) { s.now = f }

// ========== 纯计算函数（可单测，不碰数据库） ==========

// CalcItemAmount 明细金额 = 数量 × 单价，全整数运算。
func CalcItemAmount(quantity int, unitPrice int64) int64 {
	return int64(quantity) * unitPrice
}

// CalcGoodsAmount 货款 = Σ 明细金额。
func CalcGoodsAmount(items []model.OrderItem) int64 {
	var sum int64
	for _, it := range items {
		sum += it.Amount
	}
	return sum
}

// CalcPayableAmount 应收 = 货款 + 运费 - 优惠。
func CalcPayableAmount(goods, freight, discount int64) int64 {
	return goods + freight - discount
}

// ========== 入参 ==========

type ItemInput struct {
	Gender    model.Gender
	SpecGram  int
	SpecLabel string
	Unit      model.Unit
	Quantity  int
	UnitPrice int64
}

type CreateOrderInput struct {
	RequestID      string
	ReceiverName   string
	Phone          string
	Address        string
	WechatNick     string
	WechatRemark   string
	Items          []ItemInput
	FreightFee     int64
	Discount       int64
	ExpectShipDate string
	Remark         string
	Operator       string
}

type UpdateOrderInput struct {
	ID             int64
	ReceiverName   string
	Phone          string
	Address        string
	WechatNick     string
	WechatRemark   string
	Items          []ItemInput
	FreightFee     int64
	Discount       int64
	ExpectShipDate string
	Remark         string
	// ExpectedUpdatedAt 为乐观锁版本，0 表示不校验。
	ExpectedUpdatedAt int64
	Operator          string
}

// ========== 校验 ==========

func validateReceiver(name, phone, address string) error {
	if n := utf8.RuneCountInString(name); n < 1 || n > 32 {
		return errs.InvalidParam("receiver_name 必填，长度 1-32 字符")
	}
	if !phoneRe.MatchString(phone) {
		return errs.InvalidParam("phone 格式不正确，应为 11 位手机号")
	}
	if n := utf8.RuneCountInString(address); n < 5 || n > 200 {
		return errs.InvalidParam("address 必填，长度 5-200 字符")
	}
	return nil
}

// buildItems 校验明细并计算每条的金额。
func buildItems(in []ItemInput) ([]model.OrderItem, error) {
	if len(in) == 0 {
		return nil, errs.InvalidParam("items 至少需要 1 条明细")
	}
	out := make([]model.OrderItem, 0, len(in))
	for i, it := range in {
		if !it.Gender.Valid() {
			return nil, errs.InvalidParam("items[%d].gender 非法，应为 male/female/mixed", i)
		}
		unit := it.Unit
		if unit == "" {
			unit = model.UnitPiece
		}
		if !unit.Valid() {
			return nil, errs.InvalidParam("items[%d].unit 非法，应为 piece/box/jin", i)
		}
		if it.SpecGram <= 0 {
			return nil, errs.InvalidParam("items[%d].spec_gram 必须大于 0", i)
		}
		if it.SpecLabel == "" {
			return nil, errs.InvalidParam("items[%d].spec_label 必填", i)
		}
		if it.Quantity < 1 {
			return nil, errs.InvalidParam("items[%d].quantity 必须大于等于 1", i)
		}
		if it.UnitPrice < 0 {
			return nil, errs.InvalidParam("items[%d].unit_price 不能为负", i)
		}
		out = append(out, model.OrderItem{
			Gender:    it.Gender,
			SpecGram:  it.SpecGram,
			SpecLabel: it.SpecLabel,
			Unit:      unit,
			Quantity:  it.Quantity,
			UnitPrice: it.UnitPrice,
			Amount:    CalcItemAmount(it.Quantity, it.UnitPrice),
			SortNo:    i,
		})
	}
	return out, nil
}

// validateMoney 校验运费、优惠，并返回货款与应收。
func validateMoney(items []model.OrderItem, freight, discount int64) (goods, payable int64, err error) {
	if freight < 0 {
		return 0, 0, errs.InvalidParam("freight_fee 不能为负")
	}
	if discount < 0 {
		return 0, 0, errs.InvalidParam("discount 不能为负")
	}
	goods = CalcGoodsAmount(items)
	if discount > goods+freight {
		return 0, 0, errs.InvalidParam("discount 不得大于货款与运费之和")
	}
	return goods, CalcPayableAmount(goods, freight, discount), nil
}

func validateExpectDate(d string) error {
	if d != "" && !timex.ValidDate(d) {
		return errs.InvalidParam("expect_ship_date 格式应为 YYYY-MM-DD")
	}
	return nil
}

// ========== 建单 ==========

// CreateOrder 创建订单。返回的第二个值表示是否命中幂等（已存在同 request_id 的订单）。
func (s *OrderService) CreateOrder(ctx context.Context, in CreateOrderInput) (*model.Order, bool, error) {
	if err := validateReceiver(in.ReceiverName, in.Phone, in.Address); err != nil {
		return nil, false, err
	}
	items, err := buildItems(in.Items)
	if err != nil {
		return nil, false, err
	}
	goods, payable, err := validateMoney(items, in.FreightFee, in.Discount)
	if err != nil {
		return nil, false, err
	}
	if err := validateExpectDate(in.ExpectShipDate); err != nil {
		return nil, false, err
	}

	// 幂等：同一 request_id 直接返回已有订单，不报错，让小程序的网络重试无副作用。
	if in.RequestID != "" {
		if existing, err := s.st.GetOrderByRequestID(ctx, in.RequestID); err == nil {
			if err := s.loadDetail(ctx, s.st, existing); err != nil {
				return nil, false, err
			}
			return existing, true, nil
		} else if !errors.Is(err, errs.ErrNotFound) {
			return nil, false, errs.Internal(err)
		}
	}

	now := s.now()
	o := &model.Order{
		RequestID:      in.RequestID,
		ReceiverName:   in.ReceiverName,
		Phone:          in.Phone,
		Address:        in.Address,
		WechatNick:     in.WechatNick,
		WechatRemark:   in.WechatRemark,
		GoodsAmount:    goods,
		FreightFee:     in.FreightFee,
		Discount:       in.Discount,
		PayableAmount:  payable,
		PaidAmount:     0,
		ShipStatus:     model.ShipPending,
		PayStatus:      model.PayUnpaid,
		ExpectShipDate: in.ExpectShipDate,
		Remark:         in.Remark,
		CreatedAt:      now,
		UpdatedAt:      now,
		Items:          items,
	}

	err = s.st.WithTx(ctx, func(q store.Queries) error {
		orderNo, err := NextOrderNo(ctx, q, now)
		if err != nil {
			return errs.Internal(err)
		}
		o.OrderNo = orderNo

		if err := q.InsertOrder(ctx, o); err != nil {
			if store.IsUniqueViolation(err) && in.RequestID != "" {
				// 让事务回滚（日序列也一并退回），提交后回查已有订单。
				return errDuplicateRequest
			}
			return errs.Internal(err)
		}
		if err := q.InsertItems(ctx, o.ID, o.Items); err != nil {
			return errs.Internal(err)
		}
		return q.InsertLog(ctx, &model.OrderLog{
			OrderID:   o.ID,
			Action:    model.ActionCreate,
			Operator:  in.Operator,
			CreatedAt: now,
		})
	})
	// 并发下同一 request_id 同时进来，落败的那一方回查并返回赢家的订单。
	if errors.Is(err, errDuplicateRequest) {
		existing, err := s.st.GetOrderByRequestID(ctx, in.RequestID)
		if err != nil {
			return nil, false, errs.Internal(err)
		}
		if err := s.loadDetail(ctx, s.st, existing); err != nil {
			return nil, false, err
		}
		return existing, true, nil
	}
	if err != nil {
		return nil, false, err
	}

	if err := s.loadDetail(ctx, s.st, o); err != nil {
		return nil, false, err
	}
	return o, false, nil
}

// ========== 查询 ==========

// loadDetail 装载订单的明细、收款流水与操作流水。
func (s *OrderService) loadDetail(ctx context.Context, q store.Queries, o *model.Order) error {
	items, err := q.ListItemsByOrder(ctx, o.ID)
	if err != nil {
		return errs.Internal(err)
	}
	o.Items = items

	payments, err := q.ListPaymentsByOrder(ctx, o.ID)
	if err != nil {
		return errs.Internal(err)
	}
	o.Payments = payments

	logs, err := q.ListLogsByOrder(ctx, o.ID)
	if err != nil {
		return errs.Internal(err)
	}
	o.Logs = logs
	return nil
}

func (s *OrderService) GetOrder(ctx context.Context, id int64) (*model.Order, error) {
	o, err := s.st.GetOrderByID(ctx, id)
	if err != nil {
		return nil, mapNotFound(err, "订单")
	}
	if err := s.loadDetail(ctx, s.st, o); err != nil {
		return nil, err
	}
	return o, nil
}

func (s *OrderService) GetOrderByNo(ctx context.Context, orderNo string) (*model.Order, error) {
	o, err := s.st.GetOrderByNo(ctx, orderNo)
	if err != nil {
		return nil, mapNotFound(err, "订单")
	}
	if err := s.loadDetail(ctx, s.st, o); err != nil {
		return nil, err
	}
	return o, nil
}

// ListOrders 返回一页订单。只装载明细（用于 items_summary），不装载收款与操作流水。
func (s *OrderService) ListOrders(ctx context.Context, f model.OrderFilter) ([]*model.Order, int, error) {
	list, total, err := s.st.ListOrders(ctx, f)
	if err != nil {
		return nil, 0, errs.Internal(err)
	}
	if err := s.attachItems(ctx, list); err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func (s *OrderService) attachItems(ctx context.Context, list []*model.Order) error {
	if len(list) == 0 {
		return nil
	}
	ids := make([]int64, len(list))
	for i, o := range list {
		ids[i] = o.ID
	}
	m, err := s.st.ListItemsByOrders(ctx, ids)
	if err != nil {
		return errs.Internal(err)
	}
	for _, o := range list {
		o.Items = m[o.ID]
	}
	return nil
}

// ========== 改单 ==========

func (s *OrderService) UpdateOrder(ctx context.Context, in UpdateOrderInput) (*model.Order, error) {
	if err := validateReceiver(in.ReceiverName, in.Phone, in.Address); err != nil {
		return nil, err
	}
	items, err := buildItems(in.Items)
	if err != nil {
		return nil, err
	}
	goods, payable, err := validateMoney(items, in.FreightFee, in.Discount)
	if err != nil {
		return nil, err
	}
	if err := validateExpectDate(in.ExpectShipDate); err != nil {
		return nil, err
	}

	now := s.now()
	var updated *model.Order

	err = s.st.WithTx(ctx, func(q store.Queries) error {
		o, err := q.GetOrderByID(ctx, in.ID)
		if err != nil {
			return mapNotFound(err, "订单")
		}
		old := *o
		oldItems, err := q.ListItemsByOrder(ctx, o.ID)
		if err != nil {
			return errs.Internal(err)
		}
		old.Items = oldItems

		o.ReceiverName = in.ReceiverName
		o.Phone = in.Phone
		o.Address = in.Address
		o.WechatNick = in.WechatNick
		o.WechatRemark = in.WechatRemark
		o.FreightFee = in.FreightFee
		o.Discount = in.Discount
		o.GoodsAmount = goods
		o.PayableAmount = payable
		o.ExpectShipDate = in.ExpectShipDate
		o.Remark = in.Remark
		o.Items = items

		// 明细整体替换
		if err := q.DeleteItems(ctx, o.ID); err != nil {
			return errs.Internal(err)
		}
		if err := q.InsertItems(ctx, o.ID, o.Items); err != nil {
			return errs.Internal(err)
		}

		// 应收变了，实收没变，收款状态可能从 paid 退回 partial，必须重算。
		if err := recalcPayment(ctx, q, o, now, now); err != nil {
			return err
		}

		if err := q.UpdateOrder(ctx, o, in.ExpectedUpdatedAt); err != nil {
			if errors.Is(err, store.ErrVersionConflict) {
				return errs.New(errs.CodeVersionConflict, "订单已被修改，请刷新后重试")
			}
			return errs.Internal(err)
		}

		for _, d := range diffOrder(&old, o) {
			d.OrderID = o.ID
			d.Action = model.ActionUpdate
			d.Operator = in.Operator
			d.CreatedAt = now
			if err := q.InsertLog(ctx, d); err != nil {
				return errs.Internal(err)
			}
		}

		updated = o
		return nil
	})
	if err != nil {
		return nil, err
	}

	if err := s.loadDetail(ctx, s.st, updated); err != nil {
		return nil, err
	}
	return updated, nil
}

// diffOrder 逐字段比对，产出待写入 order_logs 的变更记录。
func diffOrder(old, cur *model.Order) []*model.OrderLog {
	var logs []*model.OrderLog
	add := func(field, from, to string) {
		if from != to {
			logs = append(logs, &model.OrderLog{Field: field, FromValue: from, ToValue: to})
		}
	}
	add("receiver_name", old.ReceiverName, cur.ReceiverName)
	add("phone", old.Phone, cur.Phone)
	add("address", old.Address, cur.Address)
	add("wechat_nick", old.WechatNick, cur.WechatNick)
	add("wechat_remark", old.WechatRemark, cur.WechatRemark)
	add("expect_ship_date", old.ExpectShipDate, cur.ExpectShipDate)
	add("remark", old.Remark, cur.Remark)
	add("items", old.ItemsSummary(), cur.ItemsSummary())
	add("freight_fee", strconv.FormatInt(old.FreightFee, 10), strconv.FormatInt(cur.FreightFee, 10))
	add("discount", strconv.FormatInt(old.Discount, 10), strconv.FormatInt(cur.Discount, 10))
	add("goods_amount", strconv.FormatInt(old.GoodsAmount, 10), strconv.FormatInt(cur.GoodsAmount, 10))
	add("payable_amount", strconv.FormatInt(old.PayableAmount, 10), strconv.FormatInt(cur.PayableAmount, 10))
	add("pay_status", string(old.PayStatus), string(cur.PayStatus))
	return logs
}

// ========== 状态流转 ==========

type ShipInput struct {
	ID          int64
	ShipCompany string
	TrackingNo  string
	ShipTime    *int64
	Operator    string
}

func (s *OrderService) Ship(ctx context.Context, in ShipInput) (*model.Order, error) {
	if in.ShipCompany == "" {
		return nil, errs.InvalidParam("ship_company 必填")
	}
	if in.TrackingNo == "" {
		return nil, errs.InvalidParam("tracking_no 必填")
	}
	now := s.now()
	shipTime := now
	if in.ShipTime != nil {
		shipTime = *in.ShipTime
	}

	return s.transit(ctx, in.ID, model.ShipShipped, model.ActionShip, in.Operator, "", now,
		func(o *model.Order) error {
			o.ShipCompany = in.ShipCompany
			o.TrackingNo = in.TrackingNo
			o.ShipTime = &shipTime
			return nil
		})
}

type ReceiveInput struct {
	ID          int64
	ReceiveTime *int64
	Operator    string
}

func (s *OrderService) Receive(ctx context.Context, in ReceiveInput) (*model.Order, error) {
	now := s.now()
	recvTime := now
	if in.ReceiveTime != nil {
		recvTime = *in.ReceiveTime
	}

	return s.transit(ctx, in.ID, model.ShipReceived, model.ActionReceive, in.Operator, "", now,
		func(o *model.Order) error {
			if o.ShipTime == nil {
				return errs.StateConflict("订单缺少发货时间，无法确认收货")
			}
			o.ReceiveTime = &recvTime
			return nil
		})
}

type CancelInput struct {
	ID       int64
	Reason   string
	Operator string
}

func (s *OrderService) Cancel(ctx context.Context, in CancelInput) (*model.Order, error) {
	now := s.now()
	// 取消保留已有时间戳，不清空。
	return s.transit(ctx, in.ID, model.ShipCancelled, model.ActionCancel, in.Operator, in.Reason, now, nil)
}

// transit 执行一次正向状态流转：校验 → 副作用 → 落库 → 写日志。
func (s *OrderService) transit(
	ctx context.Context,
	id int64,
	to model.ShipStatus,
	action, operator, remark string,
	now int64,
	apply func(o *model.Order) error,
) (*model.Order, error) {
	var result *model.Order
	err := s.st.WithTx(ctx, func(q store.Queries) error {
		o, err := q.GetOrderByID(ctx, id)
		if err != nil {
			return mapNotFound(err, "订单")
		}
		from := o.ShipStatus
		if !model.CanTransitShip(from, to) {
			return errs.StateConflict("发货状态不允许从 %s 变为 %s", from, to)
		}
		if apply != nil {
			if err := apply(o); err != nil {
				return err
			}
		}
		o.ShipStatus = to
		o.UpdatedAt = now

		if err := q.UpdateOrder(ctx, o, 0); err != nil {
			return errs.Internal(err)
		}
		if err := q.InsertLog(ctx, &model.OrderLog{
			OrderID:   o.ID,
			Action:    action,
			Field:     "ship_status",
			FromValue: string(from),
			ToValue:   string(to),
			Operator:  operator,
			Remark:    remark,
			CreatedAt: now,
		}); err != nil {
			return errs.Internal(err)
		}
		result = o
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := s.loadDetail(ctx, s.st, result); err != nil {
		return nil, err
	}
	return result, nil
}

type RevertShipInput struct {
	ID       int64
	To       model.ShipStatus
	Reason   string
	Operator string
}

// RevertShip 回退发货状态，用于误操作撤销。
func (s *OrderService) RevertShip(ctx context.Context, in RevertShipInput) (*model.Order, error) {
	if !in.To.Valid() {
		return nil, errs.InvalidParam("to 非法，应为 pending/shipped/received/cancelled")
	}
	if in.Reason == "" {
		return nil, errs.InvalidParam("reason 必填")
	}
	now := s.now()

	var result *model.Order
	err := s.st.WithTx(ctx, func(q store.Queries) error {
		o, err := q.GetOrderByID(ctx, in.ID)
		if err != nil {
			return mapNotFound(err, "订单")
		}
		from := o.ShipStatus
		if !model.CanRevertShip(from, in.To) {
			return errs.StateConflict("发货状态不允许从 %s 回退为 %s", from, in.To)
		}

		switch in.To {
		case model.ShipPending:
			o.ShipTime = nil
			o.ReceiveTime = nil
			o.ShipCompany = ""
			o.TrackingNo = ""
		case model.ShipShipped:
			o.ReceiveTime = nil
		}
		o.ShipStatus = in.To
		o.UpdatedAt = now

		if err := q.UpdateOrder(ctx, o, 0); err != nil {
			return errs.Internal(err)
		}
		if err := q.InsertLog(ctx, &model.OrderLog{
			OrderID:   o.ID,
			Action:    model.ActionRevert,
			Field:     "ship_status",
			FromValue: string(from),
			ToValue:   string(in.To),
			Operator:  in.Operator,
			Remark:    in.Reason,
			CreatedAt: now,
		}); err != nil {
			return errs.Internal(err)
		}
		result = o
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := s.loadDetail(ctx, s.st, result); err != nil {
		return nil, err
	}
	return result, nil
}

// DeleteOrder 软删除。列表与统计一律过滤 deleted_at IS NULL。
func (s *OrderService) DeleteOrder(ctx context.Context, id int64, operator string) error {
	now := s.now()
	return s.st.WithTx(ctx, func(q store.Queries) error {
		if _, err := q.GetOrderByID(ctx, id); err != nil {
			return mapNotFound(err, "订单")
		}
		if err := q.SoftDeleteOrder(ctx, id, now); err != nil {
			return errs.Internal(err)
		}
		return q.InsertLog(ctx, &model.OrderLog{
			OrderID:   id,
			Action:    model.ActionDelete,
			Operator:  operator,
			CreatedAt: now,
		})
	})
}

// ========== 收款 ==========

type AddPaymentInput struct {
	OrderID   int64
	Amount    int64
	PayMethod model.PayMethod
	PaidAt    *int64
	Remark    string
	Operator  string
}

// AddPayment 记一笔收款（金额为负表示退款），并在同一事务内重算订单的实收与收款状态。
func (s *OrderService) AddPayment(ctx context.Context, in AddPaymentInput) (*model.Order, error) {
	if in.Amount == 0 {
		return nil, errs.InvalidParam("amount 不能为 0")
	}
	method := in.PayMethod
	if method == "" {
		method = model.PayMethodWechat
	}
	if !method.Valid() {
		return nil, errs.InvalidParam("pay_method 非法，应为 wechat/alipay/cash/transfer/other")
	}

	now := s.now()
	paidAt := now
	if in.PaidAt != nil {
		paidAt = *in.PaidAt
	}

	var result *model.Order
	err := s.st.WithTx(ctx, func(q store.Queries) error {
		o, err := q.GetOrderByID(ctx, in.OrderID)
		if err != nil {
			return mapNotFound(err, "订单")
		}
		p := &model.Payment{
			OrderID:   o.ID,
			Amount:    in.Amount,
			PayMethod: method,
			PaidAt:    paidAt,
			Remark:    in.Remark,
			CreatedAt: now,
		}
		if err := q.InsertPayment(ctx, p); err != nil {
			return errs.Internal(err)
		}

		fromStatus := o.PayStatus
		if err := recalcPayment(ctx, q, o, paidAt, now); err != nil {
			return err
		}
		if err := q.UpdateOrder(ctx, o, 0); err != nil {
			return errs.Internal(err)
		}

		action := model.ActionPay
		if in.Amount < 0 {
			action = model.ActionRefund
		}
		if err := q.InsertLog(ctx, &model.OrderLog{
			OrderID:   o.ID,
			Action:    action,
			Field:     "pay_status",
			FromValue: string(fromStatus),
			ToValue:   string(o.PayStatus),
			Operator:  in.Operator,
			Remark:    in.Remark,
			CreatedAt: now,
		}); err != nil {
			return errs.Internal(err)
		}
		result = o
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := s.loadDetail(ctx, s.st, result); err != nil {
		return nil, err
	}
	return result, nil
}

// DeletePayment 删除记错的收款流水（软删除），同样走「重算实收 → 重算状态」的链路。
func (s *OrderService) DeletePayment(ctx context.Context, paymentID int64, operator string) (*model.Order, error) {
	now := s.now()

	var result *model.Order
	err := s.st.WithTx(ctx, func(q store.Queries) error {
		p, err := q.GetPaymentByID(ctx, paymentID)
		if err != nil {
			return mapNotFound(err, "收款记录")
		}
		o, err := q.GetOrderByID(ctx, p.OrderID)
		if err != nil {
			return mapNotFound(err, "订单")
		}
		if err := q.SoftDeletePayment(ctx, paymentID, now); err != nil {
			return errs.Internal(err)
		}

		fromStatus := o.PayStatus
		if err := recalcPayment(ctx, q, o, now, now); err != nil {
			return err
		}
		if err := q.UpdateOrder(ctx, o, 0); err != nil {
			return errs.Internal(err)
		}
		if err := q.InsertLog(ctx, &model.OrderLog{
			OrderID:   o.ID,
			Action:    model.ActionDelete,
			Field:     "pay_status",
			FromValue: string(fromStatus),
			ToValue:   string(o.PayStatus),
			Operator:  operator,
			Remark: "删除收款记录 #" + strconv.FormatInt(paymentID, 10) +
				"，金额 " + model.FormatYuan(p.Amount) + " 元",
			CreatedAt: now,
		}); err != nil {
			return errs.Internal(err)
		}
		result = o
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := s.loadDetail(ctx, s.st, result); err != nil {
		return nil, err
	}
	return result, nil
}

// recalcPayment 重算订单的实收金额与收款状态。
//
// paid_amount 与 pay_status 都是派生值，只能由这里写入：
// 实收 = 该订单未删除收款流水之和；状态由实收与应收的大小关系推导。
// eventTime 用于首次收款 / 付清的时间戳（取触发这次重算的收款时间）。
func recalcPayment(ctx context.Context, q store.Queries, o *model.Order, eventTime, now int64) error {
	paid, err := q.SumPayments(ctx, o.ID)
	if err != nil {
		return errs.Internal(err)
	}
	o.PaidAmount = paid
	o.PayStatus = model.CalcPayStatus(paid, o.PayableAmount)

	switch o.PayStatus {
	case model.PayUnpaid:
		// 退款退回未付款，首次收款时间随之失效。
		o.FirstPayTime = nil
	default:
		if o.FirstPayTime == nil {
			t := eventTime
			o.FirstPayTime = &t
		}
	}

	if o.PayStatus == model.PayPaid {
		if o.SettledTime == nil {
			t := eventTime
			o.SettledTime = &t
		}
	} else {
		o.SettledTime = nil
	}

	o.UpdatedAt = now
	return nil
}

// ========== 买家免登录查单 ==========

// PublicQuery 按「单号 + 手机号后 4 位」双因子查询。
// 任一不匹配都统一返回「不存在」，不区分原因，避免被枚举。
func (s *OrderService) PublicQuery(ctx context.Context, orderNo, phoneTail string) (*model.Order, error) {
	if orderNo == "" || len(phoneTail) != 4 {
		return nil, errs.NotFound("订单")
	}
	o, err := s.st.GetOrderByNo(ctx, orderNo)
	if err != nil {
		return nil, errs.NotFound("订单")
	}
	if len(o.Phone) < 4 || o.Phone[len(o.Phone)-4:] != phoneTail {
		return nil, errs.NotFound("订单")
	}
	items, err := s.st.ListItemsByOrder(ctx, o.ID)
	if err != nil {
		return nil, errs.Internal(err)
	}
	o.Items = items
	return o, nil
}

// mapNotFound 把 store 的哨兵错误翻译成带中文提示的业务错误。
func mapNotFound(err error, what string) error {
	if errors.Is(err, errs.ErrNotFound) {
		return errs.NotFound(what)
	}
	var e *errs.Error
	if errors.As(err, &e) {
		return e
	}
	return errs.Internal(err)
}
