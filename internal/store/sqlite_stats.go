package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"

	"crab-order/internal/errs"
	"crab-order/internal/model"
)

// ratioSuffix 明细快照末尾的公母比例，如「（公4母4）」。
var ratioSuffix = regexp.MustCompile(`（公\d+母\d+）$`)

// ---------- 规格价目表 ----------

const specColumns = `id, gender, spec_gram, grade, spec_label, unit_price_milli, enabled, sort_no, updated_at`

func scanSpec(sc rowScanner) (*model.Spec, error) {
	var s model.Spec
	var enabled int
	if err := sc.Scan(&s.ID, &s.Gender, &s.SpecGram, &s.Grade, &s.SpecLabel,
		&s.UnitPriceMilli, &enabled, &s.SortNo, &s.UpdatedAt); err != nil {
		return nil, err
	}
	s.Enabled = enabled != 0
	return &s, nil
}

func (q *queries) ListSpecs(ctx context.Context, onlyEnabled bool) ([]model.Spec, error) {
	sqlStr := `SELECT ` + specColumns + ` FROM specs`
	if onlyEnabled {
		sqlStr += ` WHERE enabled = 1`
	}
	sqlStr += ` ORDER BY sort_no, gender, spec_gram`

	rows, err := q.db.QueryContext(ctx, sqlStr)
	if err != nil {
		return nil, fmt.Errorf("list specs: %w", err)
	}
	defer rows.Close()

	out := make([]model.Spec, 0, 8)
	for rows.Next() {
		s, err := scanSpec(rows)
		if err != nil {
			return nil, fmt.Errorf("scan spec: %w", err)
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

func (q *queries) GetSpecByID(ctx context.Context, id int64) (*model.Spec, error) {
	row := q.db.QueryRowContext(ctx, `SELECT `+specColumns+` FROM specs WHERE id=?`, id)
	s, err := scanSpec(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errs.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get spec: %w", err)
	}
	return s, nil
}

func (q *queries) InsertSpec(ctx context.Context, s *model.Spec) error {
	res, err := q.db.ExecContext(ctx,
		`INSERT INTO specs(gender, spec_gram, grade, spec_label, unit_price_milli, enabled, sort_no, updated_at)
		 VALUES(?,?,?,?,?,?,?,?)`,
		string(s.Gender), s.SpecGram, string(s.Grade), s.SpecLabel, s.UnitPriceMilli,
		boolToInt(s.Enabled), s.SortNo, s.UpdatedAt)
	if err != nil {
		if IsUniqueViolation(err) {
			return fmt.Errorf("%w: %v", ErrDuplicate, err)
		}
		return fmt.Errorf("insert spec: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("insert spec last id: %w", err)
	}
	s.ID = id
	return nil
}

func (q *queries) UpdateSpec(ctx context.Context, s *model.Spec) error {
	res, err := q.db.ExecContext(ctx,
		`UPDATE specs SET gender=?, spec_gram=?, grade=?, spec_label=?, unit_price_milli=?,
		 enabled=?, sort_no=?, updated_at=? WHERE id=?`,
		string(s.Gender), s.SpecGram, string(s.Grade), s.SpecLabel, s.UnitPriceMilli,
		boolToInt(s.Enabled), s.SortNo, s.UpdatedAt, s.ID)
	if err != nil {
		if IsUniqueViolation(err) {
			return fmt.Errorf("%w: %v", ErrDuplicate, err)
		}
		return fmt.Errorf("update spec: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errs.ErrNotFound
	}
	return nil
}

// DisableSpec 停用规格（置 enabled = 0），不做物理删除，历史订单不受影响。
func (q *queries) DisableSpec(ctx context.Context, id, now int64) error {
	res, err := q.db.ExecContext(ctx,
		`UPDATE specs SET enabled=0, updated_at=? WHERE id=?`, now, id)
	if err != nil {
		return fmt.Errorf("disable spec: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errs.ErrNotFound
	}
	return nil
}

func (q *queries) CountSpecs(ctx context.Context) (int, error) {
	var n int
	if err := q.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM specs`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count specs: %w", err)
	}
	return n, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ---------- 地址簿 ----------

// ListAddresses 从历史订单聚合出地址簿，不单独建客户表，保证永远和实际下单信息一致。
func (q *queries) ListAddresses(ctx context.Context, keyword string, limit int) ([]model.Address, error) {
	kw := ""
	if keyword != "" {
		kw = likePattern(keyword)
	}
	rows, err := q.db.QueryContext(ctx,
		`SELECT phone, receiver_name, address, wechat_nick, wechat_remark,
		        COUNT(*) AS order_count, MAX(created_at) AS last_order_at
		 FROM orders
		 WHERE deleted_at IS NULL
		   AND (? = '' OR receiver_name LIKE ? ESCAPE '\' OR phone LIKE ? ESCAPE '\'
		        OR wechat_nick LIKE ? ESCAPE '\' OR wechat_remark LIKE ? ESCAPE '\')
		 GROUP BY phone, receiver_name, address
		 ORDER BY last_order_at DESC
		 LIMIT ?`,
		keyword, kw, kw, kw, kw, limit)
	if err != nil {
		return nil, fmt.Errorf("list addresses: %w", err)
	}
	defer rows.Close()

	out := make([]model.Address, 0, 16)
	for rows.Next() {
		var a model.Address
		if err := rows.Scan(&a.Phone, &a.ReceiverName, &a.AddressText, &a.WechatNick,
			&a.WechatRemark, &a.OrderCount, &a.LastOrderAt); err != nil {
			return nil, fmt.Errorf("scan address: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ---------- 统计 ----------

func (q *queries) countOrders(ctx context.Context, where string, args ...any) (int, error) {
	var n int
	if err := q.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM orders WHERE deleted_at IS NULL AND `+where, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("count orders: %w", err)
	}
	return n, nil
}

func (q *queries) CountOrdersCreatedBetween(ctx context.Context, start, end int64) (int, error) {
	return q.countOrders(ctx, `created_at >= ? AND created_at <= ?`, start, end)
}

func (q *queries) CountOrdersShippedBetween(ctx context.Context, start, end int64) (int, error) {
	return q.countOrders(ctx, `ship_time IS NOT NULL AND ship_time >= ? AND ship_time <= ?`, start, end)
}

// SumPaymentsBetween 区间内实际收到的钱（按收款时间口径，含退款的负数）。
func (q *queries) SumPaymentsBetween(ctx context.Context, start, end int64) (int64, error) {
	var sum int64
	err := q.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(l.amount),0) FROM order_logs l
		 JOIN orders o ON o.id = l.order_id
		 WHERE l.action IN ('pay','refund') AND o.deleted_at IS NULL
		   AND l.created_at >= ? AND l.created_at <= ?`, start, end).Scan(&sum)
	if err != nil {
		return 0, fmt.Errorf("sum payments between: %w", err)
	}
	return sum, nil
}

func (q *queries) CountOrdersByShipStatus(ctx context.Context, status model.ShipStatus) (int, error) {
	return q.countOrders(ctx, `ship_status = ?`, string(status))
}

func (q *queries) CountPendingByExpectDate(ctx context.Context, date string) (int, error) {
	return q.countOrders(ctx, `ship_status = 'pending' AND expect_ship_date = ?`, date)
}

// UnpaidSummary 统计仍欠款的订单数与欠款总额，已取消的订单不计入。
func (q *queries) UnpaidSummary(ctx context.Context) (int, int64, error) {
	var (
		count  int
		amount sql.NullInt64
	)
	err := q.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(payable_amount - paid_amount), 0) FROM orders
		 WHERE deleted_at IS NULL AND ship_status != 'cancelled'
		   AND pay_status IN ('unpaid','partial')`).Scan(&count, &amount)
	if err != nil {
		return 0, 0, fmt.Errorf("unpaid summary: %w", err)
	}
	return count, amount.Int64, nil
}

// RangeSummary 按创建时间口径统计区间内的订单数、应收、实收与蟹只数。
func (q *queries) RangeSummary(ctx context.Context, start, end int64) (RangeSummary, error) {
	var r RangeSummary
	err := q.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(payable_amount),0), COALESCE(SUM(paid_amount),0)
		 FROM orders WHERE deleted_at IS NULL AND created_at >= ? AND created_at <= ?`,
		start, end).Scan(&r.OrderCount, &r.PayableTotal, &r.PaidTotal)
	if err != nil {
		return r, fmt.Errorf("range summary: %w", err)
	}

	err = q.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(i.quantity),0) FROM order_items i
		 JOIN orders o ON o.id = i.order_id
		 WHERE o.deleted_at IS NULL AND o.created_at >= ? AND o.created_at <= ?`,
		start, end).Scan(&r.CrabCount)
	if err != nil {
		return r, fmt.Errorf("range crab count: %w", err)
	}
	return r, nil
}

// SpecStatsBetween 按规格聚合区间内的销量（只数）与金额。
//
// 按「性别 + 克重 + 品相」分组，不按 spec_label：档名可能改过，按名字分组同一档会被
// 拆成好几行。名称优先用价目表里的现名，价目表里没有了就取快照里的一个，
// 再去掉改版前混装老明细里的公母比例后缀。
func (q *queries) SpecStatsBetween(ctx context.Context, start, end int64) ([]model.SpecStat, error) {
	rows, err := q.db.QueryContext(ctx,
		`SELECT i.gender, i.spec_gram, i.grade,
		        COALESCE((SELECT s.spec_label FROM specs s
		                  WHERE s.gender = i.gender AND s.spec_gram = i.spec_gram AND s.grade = i.grade
		                  ORDER BY s.enabled DESC, s.id DESC LIMIT 1), MIN(i.spec_label)),
		        SUM(i.quantity), SUM(i.amount)
		 FROM order_items i JOIN orders o ON o.id = i.order_id
		 WHERE o.deleted_at IS NULL AND o.created_at >= ? AND o.created_at <= ?
		 GROUP BY i.gender, i.spec_gram, i.grade
		 ORDER BY SUM(i.amount) DESC`, start, end)
	if err != nil {
		return nil, fmt.Errorf("spec stats: %w", err)
	}
	defer rows.Close()

	out := make([]model.SpecStat, 0, 8)
	for rows.Next() {
		var s model.SpecStat
		if err := rows.Scan(&s.Gender, &s.SpecGram, &s.Grade, &s.SpecLabel, &s.Quantity, &s.Amount); err != nil {
			return nil, fmt.Errorf("scan spec stat: %w", err)
		}
		s.SpecLabel = ratioSuffix.ReplaceAllString(s.SpecLabel, "")
		out = append(out, s)
	}
	return out, rows.Err()
}

func (q *queries) FreightSummary(ctx context.Context, start, end int64) (FreightSummary, error) {
	var f FreightSummary
	err := q.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(freight_list),0), COALESCE(SUM(freight_cost),0), COALESCE(SUM(freight_fee),0)
		 FROM orders WHERE deleted_at IS NULL AND freight_cost IS NOT NULL
		   AND created_at >= ? AND created_at <= ?`, start, end).
		Scan(&f.RangeCount, &f.ListTotal, &f.CostTotal, &f.BuyerTotal)
	if err != nil {
		return f, fmt.Errorf("freight range: %w", err)
	}
	err = q.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(freight_cost),0), COALESCE(SUM(freight_list),0)
		 FROM orders WHERE deleted_at IS NULL AND freight_cost IS NOT NULL AND freight_settled_at IS NULL`).
		Scan(&f.UnsettledCount, &f.UnsettledCost, &f.UnsettledList)
	if err != nil {
		return f, fmt.Errorf("freight unsettled: %w", err)
	}
	f.ShippedPendingCount, err = q.countOrders(ctx,
		`freight_cost IS NULL AND ship_status IN ('shipped','received')`)
	if err != nil {
		return f, fmt.Errorf("freight pending: %w", err)
	}
	return f, nil
}
