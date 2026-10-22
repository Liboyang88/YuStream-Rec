package semaphore

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"
)

// fakeClock 让限流器的计时可控：sleep 只推进虚拟时间，不真的等待，
// 因此限速的"节奏"可以确定性地断言，而不是靠 sleep 猜时间。
type fakeClock struct {
	mu    sync.Mutex
	now   time.Time
	slept []time.Duration
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2024, 6, 20, 0, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	// Wait 计算出的 need 是相对它自己读到的 now，因此这里直接推进到该时刻。
	c.now = c.now.Add(d)
	c.slept = append(c.slept, d)
	return nil
}

func (c *fakeClock) sleeps() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.slept...)
}

// advance 只推进虚拟时间，不计入 sleeps（模拟"空闲了一阵"）。
func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// withClock 把限流器的时钟换成 fakeClock。
func withClock(l *Limiter) (*Limiter, *fakeClock) {
	clock := newFakeClock()
	l.now = clock.Now
	l.sleep = clock.Sleep
	return l, clock
}

// TestNilLimiterIsNoop 不限速（nil）时 Wait 立即返回，构造参数非法时也是 nil。
func TestNilLimiterIsNoop(t *testing.T) {
	var l *Limiter
	if err := l.Wait(context.Background()); err != nil {
		t.Errorf("nil 限流器 Wait 失败: %v", err)
	}
	if l.Interval() != 0 || l.Rate() != 0 || l.Burst() != 0 {
		t.Errorf("nil 限流器应返回零值: %v/%v/%v", l.Interval(), l.Rate(), l.Burst())
	}
	for _, rate := range []float64{0, -1} {
		if got := NewLimiter(rate, 1); got != nil {
			t.Errorf("NewLimiter(%v, 1) = %v, want nil（不限速）", rate, got)
		}
	}
}

// TestLimiterInterval 槽位间隔只由速率决定（interval = 1/rate），与 burst 无关：
// burst 只影响"允许一起发出几个"，不该拖慢稳态速率。
func TestLimiterInterval(t *testing.T) {
	cases := []struct {
		rate, burst float64
		want        time.Duration
	}{
		{1, 1, time.Second},
		{2, 1, 500 * time.Millisecond},
		{1, 2, time.Second},
		{2, 3, 500 * time.Millisecond},
		{10, 1, 100 * time.Millisecond},
	}
	for _, tc := range cases {
		l := NewLimiter(tc.rate, int(tc.burst))
		if got := l.Interval(); got != tc.want {
			t.Errorf("NewLimiter(%v, %v).Interval() = %v, want %v", tc.rate, tc.burst, got, tc.want)
		}
	}
}

// TestLimiterBurstDefaultsToOne burst <= 0 时按 1 处理（不突发）。
func TestLimiterBurstDefaultsToOne(t *testing.T) {
	for _, burst := range []int{0, -1, -5} {
		l := NewLimiter(1, burst)
		if got := l.Burst(); got != 1 {
			t.Errorf("NewLimiter(1, %d).Burst() = %d, want 1", burst, got)
		}
	}
}

// TestLimiterFirstRequestIsImmediate 桶初始为满：第一批 burst 个请求可以立即发出，
// 不会被无谓地延迟（默认配置 burst=1，所以只有第一个是即时的）。
func TestLimiterFirstRequestIsImmediate(t *testing.T) {
	l, clock := withClock(NewLimiter(1, 1))

	if err := l.Wait(context.Background()); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if slept := clock.sleeps(); len(slept) != 0 {
		t.Errorf("第一个请求不应等待，实际等待 %v", slept)
	}
}

// TestLimiterPacesRequests 核心用例：每秒 1 个请求时，连续的请求被均匀摊开——
// 第 2、3、4 个分别等到 1s、2s、3s，而不是一起挤在开头（那正是并发闸门挡不住的突发）。
func TestLimiterPacesRequests(t *testing.T) {
	l, clock := withClock(NewLimiter(1, 1))
	ctx := context.Background()

	start := clock.Now()
	for i := 0; i < 4; i++ {
		if err := l.Wait(ctx); err != nil {
			t.Fatalf("第 %d 次 Wait: %v", i+1, err)
		}
	}

	want := []time.Duration{time.Second, time.Second, time.Second} // 后三次各等 1s
	got := clock.sleeps()
	if len(got) != len(want) {
		t.Fatalf("等待次数 = %d (%v), want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 次等待 = %v, want %v", i+1, got[i], want[i])
		}
	}
	if elapsed := clock.Now().Sub(start); elapsed != 3*time.Second {
		t.Errorf("4 个请求共耗时 %v, want 3s（= 3 个间隔）", elapsed)
	}
}

// TestLimiterRefillsOverTime 长时间不请求后桶被填满，下一个请求立即放行。
func TestLimiterRefillsOverTime(t *testing.T) {
	l, clock := withClock(NewLimiter(2, 1))
	ctx := context.Background()

	if err := l.Wait(ctx); err != nil { // 用掉初始令牌
		t.Fatalf("Wait: %v", err)
	}
	// 推进虚拟时间：模拟这段时间没有任何请求
	clock.advance(5 * time.Second)

	if err := l.Wait(ctx); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if slept := clock.sleeps(); len(slept) != 0 {
		t.Errorf("令牌已补充，不应再等待，实际等过 %v", slept)
	}
}

// TestLimiterCapsAccumulatedTokens 令牌不会超过桶容量：空闲再久也只能突发 burst 个。
func TestLimiterCapsAccumulatedTokens(t *testing.T) {
	l, clock := withClock(NewLimiter(1, 2)) // burst=2
	ctx := context.Background()

	clock.advance(time.Hour) // 空闲很久

	// 前两个立即放行（桶容量 2）
	for i := 0; i < 2; i++ {
		if err := l.Wait(ctx); err != nil {
			t.Fatalf("第 %d 次 Wait: %v", i+1, err)
		}
	}
	// 第三个必须等 1 个间隔（1s），不能因为"攒了一小时"就再多放行几个
	if err := l.Wait(ctx); err != nil {
		t.Fatalf("第 3 次 Wait: %v", err)
	}
	if got := clock.sleeps(); got[len(got)-1] != time.Second {
		t.Errorf("第 3 个请求等待 %v, want 1s（额度上限为桶容量，突发放行不超过 burst 个）", got[len(got)-1])
	}
}

// TestLimiterRespectsContextCancel 排队等待期间 ctx 取消要立刻返回。
func TestLimiterRespectsContextCancel(t *testing.T) {
	l := NewLimiter(1, 1) // 用真实时钟：先耗尽令牌，再取消
	ctx := context.Background()

	if err := l.Wait(ctx); err != nil {
		t.Fatalf("首个 Wait: %v", err)
	}

	cancelCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- l.Wait(cancelCtx) }()
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Wait 返回 %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ctx 取消后 Wait 未返回（会卡住 Ctrl-C 退出）")
	}
}

// TestLimiterConcurrentRequestsArePaced 并发调用时不会突发：这是限速器相对并发闸门
// 的核心价值，因此用真实时钟直接断言"相邻两次放行的间隔"。
//
// 断言方式刻意不依赖虚拟时钟的精确值：并发场景下"谁排第几"取决于 goroutine 调度，
// 但**相邻放行之间至少隔一个间隔**是任何调度顺序都必须满足的不变式。
func TestLimiterConcurrentRequestsArePaced(t *testing.T) {
	const (
		interval = 12 * time.Millisecond
		workers  = 10
	)

	l := NewLimiter(float64(time.Second)/float64(interval), 1) // 每秒 1000/12 个
	if got := l.Interval(); got != interval {
		t.Fatalf("Interval() = %v, want %v", got, interval)
	}

	released := make([]time.Time, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if err := l.Wait(context.Background()); err != nil {
				t.Errorf("Wait: %v", err)
				return
			}
			released[idx] = time.Now() // 记录放行时刻（≈ 发起请求的时刻）
		}(i)
	}
	wg.Wait()

	sort.Slice(released, func(i, j int) bool { return released[i].Before(released[j]) })

	// 不变式：相邻两次放行至少相隔一个 interval。
	//
	// 余量取 40% 而不是 25%：这里的时间戳是 Wait 返回**之后**由调度器真正唤醒
	// goroutine 时打的，等于"放行时刻 + 唤醒延迟"。限速器本身的节奏没问题，
	// 但 -race 下唤醒延迟能吃掉大半个 interval，实测偶发出现过 5.6ms/12ms
	// （整包串行跑 20 次才复现 1 次）。
	// 这个放宽不会放过真正的突发：没有限速器时 10 个 goroutine 会在几毫秒内
	// 全部放行，最大间隔远小于 4.8ms。
	minGap := time.Duration(float64(interval) * 0.4)
	for i := 1; i < len(released); i++ {
		if gap := released[i].Sub(released[i-1]); gap < minGap {
			t.Errorf("第 %d 与第 %d 次放行只隔了 %v（< %v），说明发生了突发",
				i, i+1, gap, minGap)
		}
	}

	// 总跨度应接近 (workers-1) * interval = 108ms；过小同样是突发。
	total := released[len(released)-1].Sub(released[0])
	if want := time.Duration(workers-1) * interval; total < want/2 {
		t.Errorf("10 个请求的总跨度 = %v, want 至少 %v（%v）", total, want/2, want)
	}
}
