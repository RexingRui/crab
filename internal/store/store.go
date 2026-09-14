// Package store 负责所有数据访问。
//
// 分层规则：handler → service → store，严格单向。store 只做数据存取，不含业务判断。
package store

import (
	"context"

	"crab-order/internal/model"
)

// Queries 是全部数据访问方法的集合。
// *sql.DB 与 *sql.Tx 都能实现它，因此 service 层的代码在事务内外完全一致。
type Queries interface {
	// ---------- 订单 ----------
	InsertOrder(ctx context.Context, o *model.Order) error
	// UpdateOrder 写回订单的全部可变列。expectUpdatedAt > 0 时启用乐观锁，
	// 版本不一致返回 ErrVersionConflict。
	UpdateOrder(ctx context.Context, o *model.Order, expectUpdatedAt int64) error
	GetOrderByID(ctx context.Context, id int64) (*model.Order, error)
	GetOrderByNo(ctx context.Context, orderNo string) (*model.Order, error)
	GetOrderByRequestID(ctx context.Context, requestID string) (*model.Order, error)
	// ListOrders 返回一页订单与符合条件的总数。f.PageSize <= 0 表示不分页（导出用）。
	ListOrders(ctx context.Context, f model.OrderFilter) ([]*model.Order, int, error)
	SoftDeleteOrder(ctx context.Context, id, now int64) error

	// ---------- 明细 ----------
	InsertItems(ctx context.Context, orderID int64, items []model.OrderItem) error
	DeleteItems(ctx context.Context, orderID int64) error
	ListItemsByOrder(ctx context.Context, orderID int64) ([]model.OrderItem, error)
	ListItemsByOrders(ctx context.Context, orderIDs []int64) (map[int64][]model.OrderItem, error)

	// ---------- 收款 ----------
	InsertPayment(ctx context.Context, p *model.Payment) error
	GetPaymentByID(ctx context.Context, id int64) (*model.Payment, error)
	ListPaymentsByOrder(ctx context.Context, orderID int64) ([]model.Payment, error)
	SumPayments(ctx context.Context, orderID int64) (int64, error)
	SoftDeletePayment(ctx context.Context, id, now int64) error

	// ---------- 操作流水 ----------
	InsertLog(ctx context.Context, l *model.OrderLog) error
	ListLogsByOrder(ctx context.Context, orderID int64) ([]model.OrderLog, error)

	// ---------- 订单号序列 ----------
	NextOrderSeq(ctx context.Context, dateKey string) (int, error)

	// ---------- 规格价目表 ----------
	ListSpecs(ctx context.Context, onlyEnabled bool) ([]model.Spec, error)
	GetSpecByID(ctx context.Context, id int64) (*model.Spec, error)
	InsertSpec(ctx context.Context, s *model.Spec) error
	UpdateSpec(ctx context.Context, s *model.Spec) error
	DisableSpec(ctx context.Context, id, now int64) error
	CountSpecs(ctx context.Context) (int, error)

	// ---------- 地址簿 ----------
	ListAddresses(ctx context.Context, keyword string, limit int) ([]model.Address, error)

	// ---------- 统计 ----------
	CountOrdersCreatedBetween(ctx context.Context, start, end int64) (int, error)
	CountOrdersShippedBetween(ctx context.Context, start, end int64) (int, error)
	SumPaymentsBetween(ctx context.Context, start, end int64) (int64, error)
	CountOrdersByShipStatus(ctx context.Context, status model.ShipStatus) (int, error)
	CountPendingByExpectDate(ctx context.Context, date string) (int, error)
	UnpaidSummary(ctx context.Context) (count int, amount int64, err error)
	RangeSummary(ctx context.Context, start, end int64) (RangeSummary, error)
	SpecStatsBetween(ctx context.Context, start, end int64) ([]model.SpecStat, error)
}

// RangeSummary 区间统计结果。
type RangeSummary struct {
	OrderCount   int
	PayableTotal int64
	PaidTotal    int64
	CrabCount    int
}

// Store 在 Queries 之上增加事务与生命周期管理。
// 定义成接口是为了日后换 MySQL 时上层无需改动。
type Store interface {
	Queries

	// WithTx 在一个 BEGIN IMMEDIATE 事务内执行 fn，fn 返回错误则回滚，否则提交。
	// 所有涉及多表写入的操作（建单、改单、记收款、状态流转）都必须走这里。
	WithTx(ctx context.Context, fn func(q Queries) error) error

	Close() error
}
