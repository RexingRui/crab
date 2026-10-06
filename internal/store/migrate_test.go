package store

import (
	"context"
	"path/filepath"
	"testing"

	"crab-order/internal/model"
)

// oldOrdersDDL 是加 source 列之前的 orders 表。
// 线上已经跑着这样一张表，schema.sql 里的 CREATE TABLE IF NOT EXISTS 对它不生效，
// 增量列必须由 Migrate 自己补上——不补的话所有查询都会因为缺列直接炸。
const oldOrdersDDL = `CREATE TABLE orders (
	id                INTEGER PRIMARY KEY AUTOINCREMENT,
	order_no          TEXT    NOT NULL UNIQUE,
	request_id        TEXT,
	receiver_name     TEXT    NOT NULL,
	phone             TEXT    NOT NULL,
	address           TEXT    NOT NULL,
	wechat_nick       TEXT    NOT NULL DEFAULT '',
	wechat_remark     TEXT    NOT NULL DEFAULT '',
	goods_amount      INTEGER NOT NULL DEFAULT 0,
	freight_fee       INTEGER NOT NULL DEFAULT 0,
	discount          INTEGER NOT NULL DEFAULT 0,
	payable_amount    INTEGER NOT NULL DEFAULT 0,
	paid_amount       INTEGER NOT NULL DEFAULT 0,
	ship_status       TEXT    NOT NULL DEFAULT 'pending',
	pay_status        TEXT    NOT NULL DEFAULT 'unpaid',
	ship_company      TEXT    NOT NULL DEFAULT '',
	tracking_no       TEXT    NOT NULL DEFAULT '',
	expect_ship_date  TEXT,
	ship_time         INTEGER,
	receive_time      INTEGER,
	first_pay_time    INTEGER,
	settled_time      INTEGER,
	remark            TEXT    NOT NULL DEFAULT '',
	created_at        INTEGER NOT NULL,
	updated_at        INTEGER NOT NULL,
	deleted_at        INTEGER
)`

// TestMigrateAddsSourceToExistingDB 老库升级：补列、老数据按 manual 读出来。
func TestMigrateAddsSourceToExistingDB(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "old.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	ctx := context.Background()

	// 先造一张老表，并塞一笔老订单进去。
	if _, err := st.DB().ExecContext(ctx, oldOrdersDDL); err != nil {
		t.Fatalf("create old table: %v", err)
	}
	if _, err := st.DB().ExecContext(ctx,
		`INSERT INTO orders(order_no, receiver_name, phone, address, created_at, updated_at)
		 VALUES('20260101-001','张三','13800138000','苏州市工业园区xx路 1 号',1,1)`); err != nil {
		t.Fatalf("insert old order: %v", err)
	}

	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// 可重复执行：再跑一次不该报 duplicate column。
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate 第二次: %v", err)
	}

	o, err := st.GetOrderByNo(ctx, "20260101-001")
	if err != nil {
		t.Fatalf("查老订单失败: %v", err)
	}
	if o.Source != model.SourceManual {
		t.Fatalf("老数据的来源该是 manual，实际 %q", o.Source)
	}
}

// TestMigrateBackfillsCrabCount 老明细补 crab_count：按只=数量，按盒回价目表乘每盒只数，按斤=0。
func TestMigrateBackfillsCrabCount(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "old.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	ctx := context.Background()

	for _, q := range []string{
		oldOrdersDDL,
		`CREATE TABLE order_items (
			id INTEGER PRIMARY KEY AUTOINCREMENT, order_id INTEGER NOT NULL,
			gender TEXT NOT NULL, spec_gram INTEGER NOT NULL, spec_label TEXT NOT NULL,
			unit TEXT NOT NULL DEFAULT 'piece', quantity INTEGER NOT NULL,
			unit_price INTEGER NOT NULL, amount INTEGER NOT NULL, sort_no INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE specs (
			id INTEGER PRIMARY KEY AUTOINCREMENT, gender TEXT NOT NULL, spec_gram INTEGER NOT NULL,
			spec_label TEXT NOT NULL, unit TEXT NOT NULL DEFAULT 'piece', unit_price INTEGER NOT NULL,
			pack_size INTEGER NOT NULL DEFAULT 0, enabled INTEGER NOT NULL DEFAULT 1,
			sort_no INTEGER NOT NULL DEFAULT 0, updated_at INTEGER NOT NULL)`,
		`INSERT INTO specs(gender, spec_gram, spec_label, unit, unit_price, pack_size, updated_at)
		 VALUES('mixed', 1200, '8只装', 'box', 18900, 8, 1)`,
		`INSERT INTO orders(order_no, receiver_name, phone, address, created_at, updated_at)
		 VALUES('20260101-001','张三','13800138000','苏州市工业园区xx路 1 号',1,1)`,
		`INSERT INTO order_items(order_id, gender, spec_gram, spec_label, unit, quantity, unit_price, amount, sort_no) VALUES
		 (1,'mixed',1200,'8只装（公8母8）','box',2,18900,37800,0),
		 (1,'mixed',1200,'8只装 散只（公2母3）','piece',5,2400,12000,1),
		 (1,'male',500,'断脚蟹','jin',3,6000,18000,2),
		 (1,'mixed',9999,'早就删掉的套餐','box',1,10000,10000,3)`,
	} {
		if _, err := st.DB().ExecContext(ctx, q); err != nil {
			t.Fatalf("准备老库失败: %v\n%s", err, q)
		}
	}

	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	items, err := st.ListItemsByOrder(ctx, 1)
	if err != nil {
		t.Fatalf("list items: %v", err)
	}
	want := []int{16, 5, 0, 0}
	for i, it := range items {
		if it.CrabCount != want[i] {
			t.Errorf("%s: crab_count=%d，期望 %d", it.SpecLabel, it.CrabCount, want[i])
		}
	}
}

// TestMigrateMovesPaymentsIntoLogs 老库的收款流水抄进操作流水，删掉的不抄，原表改名留底。
func TestMigrateMovesPaymentsIntoLogs(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "old.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	ctx := context.Background()

	for _, q := range []string{
		oldOrdersDDL,
		`CREATE TABLE payments (
			id INTEGER PRIMARY KEY AUTOINCREMENT, order_id INTEGER NOT NULL, amount INTEGER NOT NULL,
			pay_method TEXT NOT NULL DEFAULT 'wechat', paid_at INTEGER NOT NULL,
			remark TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL, deleted_at INTEGER)`,
		`CREATE TABLE order_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT, order_id INTEGER NOT NULL, action TEXT NOT NULL,
			field TEXT NOT NULL DEFAULT '', from_value TEXT NOT NULL DEFAULT '', to_value TEXT NOT NULL DEFAULT '',
			operator TEXT NOT NULL DEFAULT '', remark TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL)`,
		`INSERT INTO orders(order_no, receiver_name, phone, address, paid_amount, created_at, updated_at)
		 VALUES('20260101-001','张三','13800138000','苏州市工业园区xx路 1 号',30000,1,1)`,
		`INSERT INTO payments(order_id, amount, pay_method, paid_at, remark, created_at, deleted_at) VALUES
		 (1, 40000, 'wechat', 100, '定金', 100, NULL),
		 (1, 99900, 'cash',   150, '记错了', 150, 160),
		 (1, -10000, 'wechat', 200, '死了一只', 200, NULL)`,
	} {
		if _, err := st.DB().ExecContext(ctx, q); err != nil {
			t.Fatalf("准备老库失败: %v\n%s", err, q)
		}
	}

	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate 第二次: %v", err)
	}

	logs, err := st.ListLogsByOrder(ctx, 1)
	if err != nil {
		t.Fatalf("list logs: %v", err)
	}
	var got []model.OrderLog
	for _, l := range logs {
		if l.IsPayment() {
			got = append(got, l)
		}
	}
	if len(got) != 2 {
		t.Fatalf("应抄过来 2 笔（删掉的不抄、跑两次不重复），实际 %+v", got)
	}
	if got[0].Amount != 40000 || got[0].Remark != "定金" || got[0].CreatedAt != 100 {
		t.Errorf("定金抄错了: %+v", got[0])
	}
	if got[1].Amount != -10000 || got[1].Action != model.ActionRefund || got[1].PayMethod != model.PayMethodWechat {
		t.Errorf("退款抄错了: %+v", got[1])
	}

	var n int
	if err := st.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='payments_legacy'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("原表应改名为 payments_legacy 留底: n=%d err=%v", n, err)
	}
}

// TestMigrateFreshDBHasSource 新库直接由 schema.sql 建出来，也得有这一列。
func TestMigrateFreshDBHasSource(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	ctx := context.Background()

	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ok, err := st.hasColumn(ctx, "orders", "source")
	if err != nil {
		t.Fatalf("hasColumn: %v", err)
	}
	if !ok {
		t.Fatal("新建的库里没有 orders.source")
	}
}

// TestCountOrdersByPhoneSince 手机号查重只看窗口内、未删除的单。
func TestCountOrdersByPhoneSince(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "phone.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	ctx := context.Background()
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	const phone = "13800138000"
	mk := func(no string, createdAt int64) *model.Order {
		return &model.Order{
			OrderNo: no, ReceiverName: "张三", Phone: phone,
			Address: "苏州市工业园区xx路 1 号", Source: model.SourceWeb,
			CreatedAt: createdAt, UpdatedAt: createdAt,
		}
	}
	old := mk("20260101-001", 1000)
	recent := mk("20260101-002", 5000)
	for _, o := range []*model.Order{old, recent} {
		if err := st.InsertOrder(ctx, o); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	n, err := st.CountOrdersByPhoneSince(ctx, phone, 4000)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("窗口内应只数到 1 笔，实际 %d", n)
	}

	// 软删掉之后就不该再拦人。
	if err := st.SoftDeleteOrder(ctx, recent.ID, 6000); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	if n, err = st.CountOrdersByPhoneSince(ctx, phone, 4000); err != nil || n != 0 {
		t.Fatalf("软删后该数到 0，实际 %d err=%v", n, err)
	}
}
