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

// TestMigrateBoxToPiece 按盒计价的老库转成按只计价：
// 明细的数量换成只数、单价换成单只价（厘），金额一分不动；规格表改名留底，换上按只的价目表。
// 还顺带覆盖了更老的库：明细上连 crab_count 都没有，得先按价目表补出只数再转。
func TestMigrateBoxToPiece(t *testing.T) {
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
		`CREATE UNIQUE INDEX uk_specs ON specs(gender, spec_gram, unit)`,
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
	// 可重复执行：转过一次之后规格表没有 unit 列了，不会再转第二次。
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate 第二次: %v", err)
	}

	items, err := st.ListItemsByOrder(ctx, 1)
	if err != nil {
		t.Fatalf("list items: %v", err)
	}
	want := []struct {
		qty    int
		milli  int64
		amount int64
	}{
		{16, 23625, 37800}, // 2 盒 × 8 只，189 / 8 = 23.625
		{5, 24000, 12000},  // 散只本来就按只
		{3, 60000, 18000},  // 按斤折不出只数：数量照旧，单价换成厘
		{1, 100000, 10000}, // 查不到每盒几只的盒：同上
	}
	if len(items) != len(want) {
		t.Fatalf("明细条数 %d，期望 %d", len(items), len(want))
	}
	for i, it := range items {
		w := want[i]
		if it.Quantity != w.qty || it.UnitPriceMilli != w.milli || it.Amount != w.amount {
			t.Errorf("%s: 只数 %d 单价 %d 金额 %d，期望 %d / %d / %d",
				it.SpecLabel, it.Quantity, it.UnitPriceMilli, it.Amount, w.qty, w.milli, w.amount)
		}
		if it.Grade != model.GradeNormal {
			t.Errorf("%s: 老明细品相该是 normal，实际 %q", it.SpecLabel, it.Grade)
		}
	}
	// 能折出只数的，按新公式重算金额也正好等于原金额
	if got := model.LineAmount(items[0].Quantity, items[0].UnitPriceMilli); got != items[0].Amount {
		t.Errorf("按新公式重算 %d，原金额 %d", got, items[0].Amount)
	}

	var n int
	if err := st.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM specs_legacy`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("老规格表应改名为 specs_legacy 留底: n=%d err=%v", n, err)
	}

	// 新规格表是空的，种子数据写入按只计价的价目表
	seeded, err := st.SeedSpecs(ctx, 100)
	if err != nil || seeded != len(seedSpecs) {
		t.Fatalf("seed: n=%d err=%v", seeded, err)
	}
	dup := model.Spec{Gender: model.GenderFemale, SpecGram: 150, Grade: model.GradeNormal,
		SpecLabel: "3两", UnitPriceMilli: 1, UpdatedAt: 100}
	if err := st.InsertSpec(ctx, &dup); !IsUniqueViolation(err) {
		t.Fatalf("同 性别 + 克重 + 品相 应撞唯一索引，实际 %v", err)
	}
	broken := dup
	broken.Grade = model.GradeBroken
	if err := st.InsertSpec(ctx, &broken); err != nil {
		t.Fatalf("同克重的残蟹是另一档，应能加: %v", err)
	}
}

// 种子价目表：每一档 8 只正好是去年的整盒价。
func TestSeedSpecsPackPrices(t *testing.T) {
	want := map[int64]bool{18900: true, 26900: true, 35900: true, 43900: true}
	for _, sp := range seedSpecs {
		if got := model.LineAmount(8, sp.UnitPriceMilli); !want[got] {
			t.Errorf("%s%s 8 只 = %d 分，不是整盒价", sp.Gender.Text(), sp.SpecLabel, got)
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
