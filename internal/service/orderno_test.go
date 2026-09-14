package service

import (
	"context"
	"sync"
	"testing"

	"crab-order/internal/store"
)

func TestFormatOrderNo(t *testing.T) {
	cases := []struct {
		seq  int
		want string
	}{
		{1, "20260914-001"},
		{7, "20260914-007"},
		{999, "20260914-999"},
		{1000, "20260914-1000"}, // 超过 999 自动扩位，不截断也不重号
	}
	for _, c := range cases {
		if got := FormatOrderNo("20260914", c.seq); got != c.want {
			t.Errorf("FormatOrderNo(20260914, %d) = %q, want %q", c.seq, got, c.want)
		}
	}
}

// TestNextOrderNoConcurrent 并发生成 1000 个订单号，必须两两不重复。
func TestNextOrderNoConcurrent(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	const n = 1000
	const workers = 16

	var (
		mu   sync.Mutex
		seen = make(map[string]struct{}, n)
		wg   sync.WaitGroup
	)
	jobs := make(chan struct{}, n)
	for i := 0; i < n; i++ {
		jobs <- struct{}{}
	}
	close(jobs)

	errCh := make(chan error, workers)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range jobs {
				var no string
				err := st.WithTx(ctx, func(q store.Queries) error {
					var err error
					no, err = NextOrderNo(ctx, q, 1_757_000_000)
					return err
				})
				if err != nil {
					errCh <- err
					return
				}
				mu.Lock()
				if _, dup := seen[no]; dup {
					mu.Unlock()
					errCh <- errDuplicateOrderNo(no)
					return
				}
				seen[no] = struct{}{}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("并发生成订单号失败: %v", err)
	}
	if len(seen) != n {
		t.Fatalf("生成了 %d 个不重复订单号，期望 %d 个", len(seen), n)
	}
}

type dupOrderNoErr string

func (e dupOrderNoErr) Error() string { return "订单号重复: " + string(e) }

func errDuplicateOrderNo(no string) error { return dupOrderNoErr(no) }
