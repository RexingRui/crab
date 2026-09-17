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
