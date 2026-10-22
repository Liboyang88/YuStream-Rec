package semaphore

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestNilSemaphoreIsNoop 不限流（nil）时 Acquire / Release / Cap 都必须安全可用。
func TestNilSemaphoreIsNoop(t *testing.T) {
	var gate *Semaphore

	for i := 0; i < 100; i++ {
		if err := gate.Acquire(context.Background()); err != nil {
			t.Fatalf("nil 闸门 Acquire 失败: %v", err)
		}
		gate.Release()
	}
	if got := gate.Cap(); got != 0 {
		t.Errorf("Cap() = %d, want 0（不限流）", got)
	}
}

// TestNewNonPositiveIsUnlimited n <= 0 视为不限流。
func TestNewNonPositiveIsUnlimited(t *testing.T) {
	for _, n := range []int{0, -1, -8} {
		if gate := New(n); gate != nil {
			t.Errorf("New(%d) = %v, want nil（不限流）", n, gate)
		}
	}
}

// TestAcquireBlocksAtCapacity 名额用满后发起者阻塞，释放后立即放行。
func TestAcquireBlocksAtCapacity(t *testing.T) {
	gate := New(2)
	ctx := context.Background()

	if err := gate.Acquire(ctx); err != nil {
		t.Fatalf("第 1 次 Acquire: %v", err)
	}
	if err := gate.Acquire(ctx); err != nil {
		t.Fatalf("第 2 次 Acquire: %v", err)
	}
	if got := gate.Cap(); got != 2 {
		t.Errorf("Cap() = %d, want 2", got)
	}

	// 第 3 次必须阻塞
	acquired := make(chan struct{})
	go func() {
		_ = gate.Acquire(ctx)
		close(acquired)
	}()

	select {
	case <-acquired:
		t.Fatal("名额已满时 Acquire 不应立即返回")
	case <-time.After(50 * time.Millisecond):
	}

	gate.Release()
	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("Release 之后阻塞的 Acquire 应及时返回")
	}
}

// TestAcquireRespectsContextCancel 等待名额期间 ctx 取消要立刻返回 ctx.Err()，
// 否则 Ctrl-C 之后程序会卡在等名额上而不是退出。
func TestAcquireRespectsContextCancel(t *testing.T) {
	gate := New(1)
	if err := gate.Acquire(context.Background()); err != nil {
		t.Fatalf("占满名额: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- gate.Acquire(ctx) }()

	cancel()
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Acquire 返回 %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("ctx 取消后 Acquire 未返回")
	}
}

// TestAcquireWithDeadline ctx 超时同样返回错误。
func TestAcquireWithDeadline(t *testing.T) {
	gate := New(1)
	if err := gate.Acquire(context.Background()); err != nil {
		t.Fatalf("占满名额: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := gate.Acquire(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Acquire 返回 %v, want context.DeadlineExceeded", err)
	}
}

// TestConcurrencyNeverExceedsCapacity 并发压测：同时在闸门内的 goroutine 数不超过容量。
func TestConcurrencyNeverExceedsCapacity(t *testing.T) {
	const (
		capacity = 4
		workers  = 64
	)
	gate := New(capacity)

	var (
		inFlight atomic.Int64
		peak     atomic.Int64
		wg       sync.WaitGroup
	)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := gate.Acquire(context.Background()); err != nil {
				t.Errorf("Acquire: %v", err)
				return
			}
			defer gate.Release()

			cur := inFlight.Add(1)
			for {
				old := peak.Load()
				if cur <= old || peak.CompareAndSwap(old, cur) {
					break
				}
			}
			time.Sleep(time.Millisecond)
			inFlight.Add(-1)
		}()
	}
	wg.Wait()

	if peak.Load() > capacity {
		t.Errorf("并发峰值 = %d, 超过容量 %d", peak.Load(), capacity)
	}
	if peak.Load() < 2 {
		t.Errorf("并发峰值 = %d, 闸门可能把并发压成了串行", peak.Load())
	}
}
