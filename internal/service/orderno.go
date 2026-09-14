package service

import (
	"context"
	"fmt"

	"crab-order/internal/store"
	"crab-order/internal/timex"
)

// FormatOrderNo 拼装订单号，格式 YYYYMMDD-NNN。
// 超过 999 时 %03d 会自动扩位为 1000、1001……，不会截断也不会重号。
func FormatOrderNo(dateKey string, seq int) string {
	return fmt.Sprintf("%s-%03d", dateKey, seq)
}

// NextOrderNo 生成当天的下一个订单号。
// 必须在创建订单的同一个事务内调用：日序列的自增与订单插入同生共死，避免并发重号。
func NextOrderNo(ctx context.Context, q store.Queries, ts int64) (string, error) {
	dateKey := timex.DateKey(ts)
	seq, err := q.NextOrderSeq(ctx, dateKey)
	if err != nil {
		return "", err
	}
	return FormatOrderNo(dateKey, seq), nil
}
