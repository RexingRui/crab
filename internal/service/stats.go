package service

import (
	"context"

	"crab-order/internal/errs"
	"crab-order/internal/model"
	"crab-order/internal/store"
	"crab-order/internal/timex"
)

// StatsService 负责看板与发货计划。
type StatsService struct {
	st  store.Store
	now func() int64
}

func NewStatsService(st store.Store) *StatsService {
	return &StatsService{st: st, now: timex.Now}
}

func (s *StatsService) SetClock(f func() int64) { s.now = f }

type TodayStat struct {
	NewOrders     int    `json:"new_orders"`
	ShippedOrders int    `json:"shipped_orders"`
	Revenue       int64  `json:"revenue"`
	RevenueYuan   string `json:"revenue_yuan"`
}

type PendingStat struct {
	ToShipCount             int    `json:"to_ship_count"`
	ToShipTodayCount        int    `json:"to_ship_today_count"`
	ShippedNotReceivedCount int    `json:"shipped_not_received_count"`
	UnpaidOrderCount        int    `json:"unpaid_order_count"`
	UnpaidAmount            int64  `json:"unpaid_amount"`
	UnpaidAmountYuan        string `json:"unpaid_amount_yuan"`
}

type RangeStat struct {
	Start            string           `json:"start"`
	End              string           `json:"end"`
	OrderCount       int              `json:"order_count"`
	PayableTotal     int64            `json:"payable_total"`
	PayableTotalYuan string           `json:"payable_total_yuan"`
	PaidTotal        int64            `json:"paid_total"`
	PaidTotalYuan    string           `json:"paid_total_yuan"`
	CrabCount        int              `json:"crab_count"`
	BySpec           []model.SpecStat `json:"by_spec"`
}

type Dashboard struct {
	Today   TodayStat   `json:"today"`
	Pending PendingStat `json:"pending"`
	Range   RangeStat   `json:"range"`
}

// Dashboard 汇总看板数据。start/end 为空时默认取当天。
//
// 口径说明：
//   - today.revenue：当天实际收到的钱（按 payments.paid_at 计，含退款负数）
//   - range.*：按订单创建时间落在区间内统计
func (s *StatsService) Dashboard(ctx context.Context, start, end string) (*Dashboard, error) {
	today := timex.DateStr(s.now())
	if start == "" {
		start = today
	}
	if end == "" {
		end = today
	}
	if !timex.ValidDate(start) {
		return nil, errs.InvalidParam("start 格式应为 YYYY-MM-DD")
	}
	if !timex.ValidDate(end) {
		return nil, errs.InvalidParam("end 格式应为 YYYY-MM-DD")
	}
	if start > end {
		return nil, errs.InvalidParam("start 不能晚于 end")
	}

	dayStart, dayEnd, err := timex.DayRange(today)
	if err != nil {
		return nil, errs.InvalidParam("%v", err)
	}
	rangeStart, rangeEnd, err := timex.Range(start, end)
	if err != nil {
		return nil, errs.InvalidParam("%v", err)
	}

	var d Dashboard

	if d.Today.NewOrders, err = s.st.CountOrdersCreatedBetween(ctx, dayStart, dayEnd); err != nil {
		return nil, errs.Internal(err)
	}
	if d.Today.ShippedOrders, err = s.st.CountOrdersShippedBetween(ctx, dayStart, dayEnd); err != nil {
		return nil, errs.Internal(err)
	}
	if d.Today.Revenue, err = s.st.SumPaymentsBetween(ctx, dayStart, dayEnd); err != nil {
		return nil, errs.Internal(err)
	}
	d.Today.RevenueYuan = model.FormatYuan(d.Today.Revenue)

	if d.Pending.ToShipCount, err = s.st.CountOrdersByShipStatus(ctx, model.ShipPending); err != nil {
		return nil, errs.Internal(err)
	}
	if d.Pending.ToShipTodayCount, err = s.st.CountPendingByExpectDate(ctx, today); err != nil {
		return nil, errs.Internal(err)
	}
	if d.Pending.ShippedNotReceivedCount, err = s.st.CountOrdersByShipStatus(ctx, model.ShipShipped); err != nil {
		return nil, errs.Internal(err)
	}
	cnt, amount, err := s.st.UnpaidSummary(ctx)
	if err != nil {
		return nil, errs.Internal(err)
	}
	d.Pending.UnpaidOrderCount = cnt
	d.Pending.UnpaidAmount = amount
	d.Pending.UnpaidAmountYuan = model.FormatYuan(amount)

	rs, err := s.st.RangeSummary(ctx, rangeStart, rangeEnd)
	if err != nil {
		return nil, errs.Internal(err)
	}
	bySpec, err := s.st.SpecStatsBetween(ctx, rangeStart, rangeEnd)
	if err != nil {
		return nil, errs.Internal(err)
	}
	d.Range = RangeStat{
		Start:            start,
		End:              end,
		OrderCount:       rs.OrderCount,
		PayableTotal:     rs.PayableTotal,
		PayableTotalYuan: model.FormatYuan(rs.PayableTotal),
		PaidTotal:        rs.PaidTotal,
		PaidTotalYuan:    model.FormatYuan(rs.PaidTotal),
		CrabCount:        rs.CrabCount,
		BySpec:           bySpec,
	}
	return &d, nil
}

// ShipPlan 返回某天约定发货、且仍待发货的订单——卖家每天早上最常看的那一屏。
// date 为空默认取明天。
func (s *StatsService) ShipPlan(ctx context.Context, date string) (string, []*model.Order, error) {
	if date == "" {
		var err error
		date, err = timex.AddDays(timex.DateStr(s.now()), 1)
		if err != nil {
			return "", nil, errs.Internal(err)
		}
	}
	if !timex.ValidDate(date) {
		return "", nil, errs.InvalidParam("date 格式应为 YYYY-MM-DD")
	}

	list, _, err := s.st.ListOrders(ctx, model.OrderFilter{
		ShipStatus:     []model.ShipStatus{model.ShipPending},
		ExpectShipDate: date,
		Sort:           model.SortCreatedAsc,
	})
	if err != nil {
		return "", nil, errs.Internal(err)
	}
	if len(list) > 0 {
		ids := make([]int64, len(list))
		for i, o := range list {
			ids[i] = o.ID
		}
		m, err := s.st.ListItemsByOrders(ctx, ids)
		if err != nil {
			return "", nil, errs.Internal(err)
		}
		for _, o := range list {
			o.Items = m[o.ID]
		}
	}
	return date, list, nil
}
