package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"crab-order/internal/errs"
	"crab-order/internal/model"

	sqlite3 "modernc.org/sqlite"
)

// ErrVersionConflict 乐观锁冲突（数据已被他人修改）。
var ErrVersionConflict = errors.New("version conflict")

// ErrDuplicate 唯一约束冲突，用于幂等键重复的识别。
var ErrDuplicate = errors.New("duplicate key")

// dbtx 同时被 *sql.DB 与 *sql.Tx 满足。
type dbtx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// queries 是 Queries 接口的 SQL 实现，底层既可以是连接池也可以是事务。
type queries struct {
	db dbtx
}

// SQLiteStore 是 Store 的 SQLite 实现。
type SQLiteStore struct {
	queries
	db *sql.DB
}

var _ Store = (*SQLiteStore)(nil)

// Open 打开（并按需创建）SQLite 数据库。
//
// 连接串里带上必须的 PRAGMA，保证每条新连接都生效；_txlock=immediate 让
// database/sql 的 BeginTx 直接发出 BEGIN IMMEDIATE，避免写事务升级锁时报
// "database is locked"。
func Open(path string) (*SQLiteStore, error) {
	dsn := "file:" + path + "?" + strings.Join([]string{
		"_pragma=journal_mode(WAL)",
		"_pragma=busy_timeout(5000)",
		"_pragma=foreign_keys(ON)",
		"_pragma=synchronous(NORMAL)",
		"_txlock=immediate",
	}, "&")

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// SQLite 是单写者模型，本项目并发极低，统一限制为 1 条连接最省事且不会出错。
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	// DSN 里已经设置过，这里再显式执行一次并校验，避免驱动参数拼写错误被静默忽略。
	for _, p := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA foreign_keys = ON",
		"PRAGMA synchronous = NORMAL",
	} {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("exec %q: %w", p, err)
		}
	}

	s := &SQLiteStore{db: db}
	s.queries.db = db
	return s, nil
}

// DB 暴露底层连接池，仅供 migrate 与测试使用。
func (s *SQLiteStore) DB() *sql.DB { return s.db }

func (s *SQLiteStore) Close() error { return s.db.Close() }

// WithTx 在 BEGIN IMMEDIATE 事务中执行 fn。
func (s *SQLiteStore) WithTx(ctx context.Context, fn func(q Queries) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()

	if err := fn(&queries{db: tx}); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

// IsUniqueViolation 判断错误是否为唯一约束冲突。
func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrDuplicate) {
		return true
	}
	var se *sqlite3.Error
	if errors.As(err, &se) {
		// SQLITE_CONSTRAINT = 19，扩展码为 19 | (n<<8)
		if se.Code()&0xff == 19 {
			return true
		}
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// ---------- 通用小工具 ----------

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullInt(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

func toStr(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}

func toPtr(ni sql.NullInt64) *int64 {
	if ni.Valid {
		v := ni.Int64
		return &v
	}
	return nil
}

// likePattern 构造模糊匹配串，转义 LIKE 的通配符，SQL 侧需配合 ESCAPE '\'。
func likePattern(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(s) + "%"
}

// ---------- 订单 ----------

const orderColumns = `id, order_no, request_id, receiver_name, phone, address,
	wechat_nick, wechat_remark, goods_amount, freight_fee, discount, payable_amount,
	paid_amount, ship_status, pay_status, ship_company, tracking_no, expect_ship_date,
	ship_time, receive_time, first_pay_time, settled_time, remark, source,
	created_at, updated_at, deleted_at`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanOrder(sc rowScanner) (*model.Order, error) {
	var (
		o        model.Order
		reqID    sql.NullString
		expect   sql.NullString
		shipT    sql.NullInt64
		recvT    sql.NullInt64
		firstPay sql.NullInt64
		settled  sql.NullInt64
		deleted  sql.NullInt64
	)
	err := sc.Scan(
		&o.ID, &o.OrderNo, &reqID, &o.ReceiverName, &o.Phone, &o.Address,
		&o.WechatNick, &o.WechatRemark, &o.GoodsAmount, &o.FreightFee, &o.Discount,
		&o.PayableAmount, &o.PaidAmount, &o.ShipStatus, &o.PayStatus,
		&o.ShipCompany, &o.TrackingNo, &expect,
		&shipT, &recvT, &firstPay, &settled, &o.Remark, &o.Source,
		&o.CreatedAt, &o.UpdatedAt, &deleted,
	)
	if err != nil {
		return nil, err
	}
	o.RequestID = toStr(reqID)
	if o.Source == "" {
		o.Source = model.SourceManual
	}
	o.ExpectShipDate = toStr(expect)
	o.ShipTime = toPtr(shipT)
	o.ReceiveTime = toPtr(recvT)
	o.FirstPayTime = toPtr(firstPay)
	o.SettledTime = toPtr(settled)
	o.DeletedAt = toPtr(deleted)
	return &o, nil
}

// sourceOrDefault 兜住零值：订单来源不写死在调用方，漏传就是卖家自己录的。
func sourceOrDefault(s model.Source) model.Source {
	if s == "" {
		return model.SourceManual
	}
	return s
}

func (q *queries) InsertOrder(ctx context.Context, o *model.Order) error {
	const sqlStr = `INSERT INTO orders(
		order_no, request_id, receiver_name, phone, address, wechat_nick, wechat_remark,
		goods_amount, freight_fee, discount, payable_amount, paid_amount,
		ship_status, pay_status, ship_company, tracking_no, expect_ship_date,
		ship_time, receive_time, first_pay_time, settled_time, remark, source,
		created_at, updated_at, deleted_at)
	VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`

	res, err := q.db.ExecContext(ctx, sqlStr,
		o.OrderNo, nullString(o.RequestID), o.ReceiverName, o.Phone, o.Address,
		o.WechatNick, o.WechatRemark,
		o.GoodsAmount, o.FreightFee, o.Discount, o.PayableAmount, o.PaidAmount,
		string(o.ShipStatus), string(o.PayStatus), o.ShipCompany, o.TrackingNo,
		nullString(o.ExpectShipDate),
		nullInt(o.ShipTime), nullInt(o.ReceiveTime), nullInt(o.FirstPayTime), nullInt(o.SettledTime),
		o.Remark, string(sourceOrDefault(o.Source)), o.CreatedAt, o.UpdatedAt, nullInt(o.DeletedAt),
	)
	if err != nil {
		if IsUniqueViolation(err) {
			return fmt.Errorf("%w: %v", ErrDuplicate, err)
		}
		return fmt.Errorf("insert order: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("insert order last id: %w", err)
	}
	o.ID = id
	return nil
}

func (q *queries) UpdateOrder(ctx context.Context, o *model.Order, expectUpdatedAt int64) error {
	sqlStr := `UPDATE orders SET
		receiver_name=?, phone=?, address=?, wechat_nick=?, wechat_remark=?,
		goods_amount=?, freight_fee=?, discount=?, payable_amount=?, paid_amount=?,
		ship_status=?, pay_status=?, ship_company=?, tracking_no=?, expect_ship_date=?,
		ship_time=?, receive_time=?, first_pay_time=?, settled_time=?, remark=?,
		updated_at=?, deleted_at=?
	WHERE id=?`
	args := []any{
		o.ReceiverName, o.Phone, o.Address, o.WechatNick, o.WechatRemark,
		o.GoodsAmount, o.FreightFee, o.Discount, o.PayableAmount, o.PaidAmount,
		string(o.ShipStatus), string(o.PayStatus), o.ShipCompany, o.TrackingNo,
		nullString(o.ExpectShipDate),
		nullInt(o.ShipTime), nullInt(o.ReceiveTime), nullInt(o.FirstPayTime), nullInt(o.SettledTime),
		o.Remark, o.UpdatedAt, nullInt(o.DeletedAt), o.ID,
	}
	if expectUpdatedAt > 0 {
		sqlStr += ` AND updated_at=?`
		args = append(args, expectUpdatedAt)
	}

	res, err := q.db.ExecContext(ctx, sqlStr, args...)
	if err != nil {
		return fmt.Errorf("update order: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update order rows: %w", err)
	}
	if n == 0 {
		if expectUpdatedAt > 0 {
			return ErrVersionConflict
		}
		return errs.ErrNotFound
	}
	return nil
}

func (q *queries) getOrderBy(ctx context.Context, where string, arg any) (*model.Order, error) {
	row := q.db.QueryRowContext(ctx,
		`SELECT `+orderColumns+` FROM orders WHERE `+where+` AND deleted_at IS NULL`, arg)
	o, err := scanOrder(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errs.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get order: %w", err)
	}
	return o, nil
}

func (q *queries) GetOrderByID(ctx context.Context, id int64) (*model.Order, error) {
	return q.getOrderBy(ctx, "id=?", id)
}

func (q *queries) GetOrderByNo(ctx context.Context, orderNo string) (*model.Order, error) {
	return q.getOrderBy(ctx, "order_no=?", orderNo)
}

func (q *queries) GetOrderByRequestID(ctx context.Context, requestID string) (*model.Order, error) {
	if requestID == "" {
		return nil, errs.ErrNotFound
	}
	return q.getOrderBy(ctx, "request_id=?", requestID)
}

func (q *queries) CountOrdersByPhoneSince(ctx context.Context, phone string, since int64) (int, error) {
	var n int
	err := q.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM orders WHERE phone=? AND created_at>=? AND deleted_at IS NULL`,
		phone, since).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count orders by phone: %w", err)
	}
	return n, nil
}

// buildOrderFilter 把筛选条件翻译成 WHERE 片段与参数。
func buildOrderFilter(f model.OrderFilter) (string, []any) {
	where := []string{"deleted_at IS NULL"}
	var args []any

	if len(f.ShipStatus) > 0 {
		ph := make([]string, len(f.ShipStatus))
		for i, s := range f.ShipStatus {
			ph[i] = "?"
			args = append(args, string(s))
		}
		where = append(where, "ship_status IN ("+strings.Join(ph, ",")+")")
	}
	if len(f.PayStatus) > 0 {
		ph := make([]string, len(f.PayStatus))
		for i, s := range f.PayStatus {
			ph[i] = "?"
			args = append(args, string(s))
		}
		where = append(where, "pay_status IN ("+strings.Join(ph, ",")+")")
	}
	if f.Source != "" {
		where = append(where, "source = ?")
		args = append(args, string(f.Source))
	}
	if f.Keyword != "" {
		p := likePattern(f.Keyword)
		where = append(where, `(order_no LIKE ? ESCAPE '\' OR receiver_name LIKE ? ESCAPE '\'
			OR phone LIKE ? ESCAPE '\' OR wechat_nick LIKE ? ESCAPE '\'
			OR wechat_remark LIKE ? ESCAPE '\' OR tracking_no LIKE ? ESCAPE '\')`)
		args = append(args, p, p, p, p, p, p)
	}
	if f.ExpectShipDate != "" {
		where = append(where, "expect_ship_date = ?")
		args = append(args, f.ExpectShipDate)
	}
	if f.ExpectShipDateStart != "" {
		where = append(where, "expect_ship_date >= ?")
		args = append(args, f.ExpectShipDateStart)
	}
	if f.ExpectShipDateEnd != "" {
		where = append(where, "expect_ship_date <= ?")
		args = append(args, f.ExpectShipDateEnd)
	}
	if f.CreatedStart != nil {
		where = append(where, "created_at >= ?")
		args = append(args, *f.CreatedStart)
	}
	if f.CreatedEnd != nil {
		where = append(where, "created_at <= ?")
		args = append(args, *f.CreatedEnd)
	}
	return strings.Join(where, " AND "), args
}

func orderBySQL(sort string) string {
	switch sort {
	case model.SortCreatedAsc:
		return "created_at ASC, id ASC"
	case model.SortExpectAsc:
		// SQLite 中 NULL 在 ASC 时排最前，这里把未约定发货日的订单挪到最后。
		return "(expect_ship_date IS NULL) ASC, expect_ship_date ASC, created_at ASC"
	default:
		return "created_at DESC, id DESC"
	}
}

func (q *queries) ListOrders(ctx context.Context, f model.OrderFilter) ([]*model.Order, int, error) {
	where, args := buildOrderFilter(f)

	var total int
	if err := q.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM orders WHERE `+where, args...).
		Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count orders: %w", err)
	}

	sqlStr := `SELECT ` + orderColumns + ` FROM orders WHERE ` + where +
		` ORDER BY ` + orderBySQL(f.Sort)
	if f.PageSize > 0 {
		page := f.Page
		if page < 1 {
			page = 1
		}
		sqlStr += ` LIMIT ? OFFSET ?`
		args = append(args, f.PageSize, (page-1)*f.PageSize)
	}

	rows, err := q.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list orders: %w", err)
	}
	defer rows.Close()

	list := make([]*model.Order, 0, 16)
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan order: %w", err)
		}
		list = append(list, o)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list orders rows: %w", err)
	}
	return list, total, nil
}

func (q *queries) SoftDeleteOrder(ctx context.Context, id, now int64) error {
	res, err := q.db.ExecContext(ctx,
		`UPDATE orders SET deleted_at=?, updated_at=? WHERE id=? AND deleted_at IS NULL`,
		now, now, id)
	if err != nil {
		return fmt.Errorf("soft delete order: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errs.ErrNotFound
	}
	return nil
}

// ---------- 订单明细 ----------

func (q *queries) InsertItems(ctx context.Context, orderID int64, items []model.OrderItem) error {
	const sqlStr = `INSERT INTO order_items(order_id, gender, spec_gram, spec_label, unit,
		quantity, unit_price, amount, sort_no) VALUES(?,?,?,?,?,?,?,?,?)`
	for i := range items {
		it := &items[i]
		res, err := q.db.ExecContext(ctx, sqlStr, orderID, string(it.Gender), it.SpecGram,
			it.SpecLabel, string(it.Unit), it.Quantity, it.UnitPrice, it.Amount, it.SortNo)
		if err != nil {
			return fmt.Errorf("insert item: %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("insert item last id: %w", err)
		}
		it.ID = id
		it.OrderID = orderID
	}
	return nil
}

func (q *queries) DeleteItems(ctx context.Context, orderID int64) error {
	if _, err := q.db.ExecContext(ctx, `DELETE FROM order_items WHERE order_id=?`, orderID); err != nil {
		return fmt.Errorf("delete items: %w", err)
	}
	return nil
}

func scanItems(rows *sql.Rows) ([]model.OrderItem, error) {
	var out []model.OrderItem
	for rows.Next() {
		var it model.OrderItem
		if err := rows.Scan(&it.ID, &it.OrderID, &it.Gender, &it.SpecGram, &it.SpecLabel,
			&it.Unit, &it.Quantity, &it.UnitPrice, &it.Amount, &it.SortNo); err != nil {
			return nil, fmt.Errorf("scan item: %w", err)
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

const itemColumns = `id, order_id, gender, spec_gram, spec_label, unit, quantity, unit_price, amount, sort_no`

func (q *queries) ListItemsByOrder(ctx context.Context, orderID int64) ([]model.OrderItem, error) {
	rows, err := q.db.QueryContext(ctx,
		`SELECT `+itemColumns+` FROM order_items WHERE order_id=? ORDER BY sort_no, id`, orderID)
	if err != nil {
		return nil, fmt.Errorf("list items: %w", err)
	}
	defer rows.Close()
	return scanItems(rows)
}

func (q *queries) ListItemsByOrders(ctx context.Context, orderIDs []int64) (map[int64][]model.OrderItem, error) {
	out := make(map[int64][]model.OrderItem, len(orderIDs))
	if len(orderIDs) == 0 {
		return out, nil
	}
	ph := make([]string, len(orderIDs))
	args := make([]any, len(orderIDs))
	for i, id := range orderIDs {
		ph[i] = "?"
		args[i] = id
	}
	rows, err := q.db.QueryContext(ctx,
		`SELECT `+itemColumns+` FROM order_items WHERE order_id IN (`+strings.Join(ph, ",")+
			`) ORDER BY order_id, sort_no, id`, args...)
	if err != nil {
		return nil, fmt.Errorf("list items by orders: %w", err)
	}
	defer rows.Close()

	items, err := scanItems(rows)
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		out[it.OrderID] = append(out[it.OrderID], it)
	}
	return out, nil
}

// ---------- 收款流水 ----------

const paymentColumns = `id, order_id, amount, pay_method, paid_at, remark, created_at, deleted_at`

func scanPayment(sc rowScanner) (*model.Payment, error) {
	var p model.Payment
	var deleted sql.NullInt64
	if err := sc.Scan(&p.ID, &p.OrderID, &p.Amount, &p.PayMethod, &p.PaidAt,
		&p.Remark, &p.CreatedAt, &deleted); err != nil {
		return nil, err
	}
	p.DeletedAt = toPtr(deleted)
	return &p, nil
}

func (q *queries) InsertPayment(ctx context.Context, p *model.Payment) error {
	res, err := q.db.ExecContext(ctx,
		`INSERT INTO payments(order_id, amount, pay_method, paid_at, remark, created_at, deleted_at)
		 VALUES(?,?,?,?,?,?,?)`,
		p.OrderID, p.Amount, string(p.PayMethod), p.PaidAt, p.Remark, p.CreatedAt, nullInt(p.DeletedAt))
	if err != nil {
		return fmt.Errorf("insert payment: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("insert payment last id: %w", err)
	}
	p.ID = id
	return nil
}

func (q *queries) GetPaymentByID(ctx context.Context, id int64) (*model.Payment, error) {
	row := q.db.QueryRowContext(ctx,
		`SELECT `+paymentColumns+` FROM payments WHERE id=? AND deleted_at IS NULL`, id)
	p, err := scanPayment(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errs.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get payment: %w", err)
	}
	return p, nil
}

func (q *queries) ListPaymentsByOrder(ctx context.Context, orderID int64) ([]model.Payment, error) {
	rows, err := q.db.QueryContext(ctx,
		`SELECT `+paymentColumns+` FROM payments WHERE order_id=? AND deleted_at IS NULL
		 ORDER BY paid_at, id`, orderID)
	if err != nil {
		return nil, fmt.Errorf("list payments: %w", err)
	}
	defer rows.Close()

	var out []model.Payment
	for rows.Next() {
		p, err := scanPayment(rows)
		if err != nil {
			return nil, fmt.Errorf("scan payment: %w", err)
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// SumPayments 汇总某订单未删除的收款流水金额，是 orders.paid_amount 的唯一来源。
func (q *queries) SumPayments(ctx context.Context, orderID int64) (int64, error) {
	var sum sql.NullInt64
	err := q.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(amount),0) FROM payments WHERE order_id=? AND deleted_at IS NULL`,
		orderID).Scan(&sum)
	if err != nil {
		return 0, fmt.Errorf("sum payments: %w", err)
	}
	return sum.Int64, nil
}

func (q *queries) SoftDeletePayment(ctx context.Context, id, now int64) error {
	res, err := q.db.ExecContext(ctx,
		`UPDATE payments SET deleted_at=? WHERE id=? AND deleted_at IS NULL`, now, id)
	if err != nil {
		return fmt.Errorf("soft delete payment: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errs.ErrNotFound
	}
	return nil
}

// ---------- 操作流水 ----------

func (q *queries) InsertLog(ctx context.Context, l *model.OrderLog) error {
	res, err := q.db.ExecContext(ctx,
		`INSERT INTO order_logs(order_id, action, field, from_value, to_value, operator, remark, created_at)
		 VALUES(?,?,?,?,?,?,?,?)`,
		l.OrderID, l.Action, l.Field, l.FromValue, l.ToValue, l.Operator, l.Remark, l.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert log: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("insert log last id: %w", err)
	}
	l.ID = id
	return nil
}

func (q *queries) ListLogsByOrder(ctx context.Context, orderID int64) ([]model.OrderLog, error) {
	rows, err := q.db.QueryContext(ctx,
		`SELECT id, order_id, action, field, from_value, to_value, operator, remark, created_at
		 FROM order_logs WHERE order_id=? ORDER BY created_at, id`, orderID)
	if err != nil {
		return nil, fmt.Errorf("list logs: %w", err)
	}
	defer rows.Close()

	var out []model.OrderLog
	for rows.Next() {
		var l model.OrderLog
		if err := rows.Scan(&l.ID, &l.OrderID, &l.Action, &l.Field, &l.FromValue,
			&l.ToValue, &l.Operator, &l.Remark, &l.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan log: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ---------- 订单号日序列 ----------

// NextOrderSeq 取当天的下一个流水号。必须在建单的同一个事务内调用，避免并发重号。
func (q *queries) NextOrderSeq(ctx context.Context, dateKey string) (int, error) {
	var seq int
	err := q.db.QueryRowContext(ctx,
		`INSERT INTO order_seq(date_key, seq) VALUES(?, 1)
		 ON CONFLICT(date_key) DO UPDATE SET seq = seq + 1
		 RETURNING seq`, dateKey).Scan(&seq)
	if err != nil {
		return 0, fmt.Errorf("next order seq: %w", err)
	}
	return seq, nil
}
