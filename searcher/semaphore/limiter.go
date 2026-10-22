package semaphore

import (
	"context"
	"sync"
	"time"
)

// Limiter 是按"速率"放行的限流器（令牌桶）。
//
// 与 Semaphore 的区别，也是它存在的理由：
//
//   - Semaphore 限制的是"同时有多少个请求在飞"，但**管不住发出的节奏**。比如并发 4、
//     每次请求只要 0.3s，就会在 300ms 内连发 4 个请求——对按秒限流（RPS）的接口来说，
//     这种瞬时突发恰恰最容易触发保护（阿里云会返回 Throttling 相关错误码）。
//   - Limiter 限制的是"每隔多久才能发一个请求"，请求间隔均匀，突发不会超过 burst。
//
// 两者可以叠加使用：先按速率排队（Limiter），再抢并发名额（Semaphore）。
//
// 零值不可用，请用 NewLimiter 构造；nil 表示不限速，Wait 立即返回。
type Limiter struct {
	mu       sync.Mutex
	rate     float64       // 每秒放行的请求数，必须 > 0
	capacity float64       // 桶容量（= 允许的瞬时突发数）
	interval time.Duration // 相邻两个槽位的间隔 = 1 / rate（稳态下两个请求的最小间隔）

	credit     float64   // 空闲额度（令牌数），上限为 capacity（= burst）
	nextStart  time.Time // 下一个槽位的开始时刻（预约时间轴的末端）
	lastRefill time.Time // 上次结算空闲额度的时刻

	// now / sleep 便于测试替换时钟；生产环境分别是 time.Now / 响应 ctx 的休眠。
	now   func() time.Time
	sleep func(context.Context, time.Duration) error
}

// NewLimiter 创建限流器：
//
//	rate    每秒允许的请求数，<= 0 表示不限速（返回 nil）
//	burst   桶容量，即允许的瞬时突发数，<= 0 时按 1 处理（不突发，请求均匀间隔）
//
// 例如 NewLimiter(1, 1) 表示"每秒 1 个请求、不突发"，也就是任意两次请求至少间隔 1s；
// NewLimiter(2, 3) 表示"每秒 2 个、最多 3 个一起发出"：前 3 个立即放行，
// 之后每 500ms 一个（稳态速率与 burst 无关）。
func NewLimiter(rate float64, burst int) *Limiter {
	if rate <= 0 {
		return nil
	}
	if burst <= 0 {
		burst = 1
	}
	capacity := float64(burst)

	return &Limiter{
		rate:     rate,
		capacity: capacity,
		interval: time.Duration(float64(time.Second) / rate),
		credit:   capacity, // 起步满桶：第一波允许 burst 个请求立即发出
		now:      time.Now,
		sleep:    sleepCtx,
	}
}

// Interval 返回两次请求之间的最小间隔（rate<=0 时为 0）。
func (l *Limiter) Interval() time.Duration {
	if l == nil {
		return 0
	}
	return l.interval
}

// Rate 返回每秒允许的请求数。
func (l *Limiter) Rate() float64 {
	if l == nil {
		return 0
	}
	return l.rate
}

// Burst 返回桶容量。
func (l *Limiter) Burst() int {
	if l == nil {
		return 0
	}
	return int(l.capacity)
}

// Wait 取一个令牌；没有令牌时按速率等到下一个可用时刻。
// 等待期间 ctx 取消会立即返回 ctx.Err()——Ctrl-C 时不该还赖在限速队列里。
//
// 规则只有两条，其余都是推论：
//
//  1. credit（空闲额度）只按"流逝的时间"补充，上限为 burst。
//     因此 burst 只体现在启动时和长时间空闲之后；连续请求之间时间没有流逝，
//     自然不会有新额度，也就不会出现持续突发。
//  2. 有额度 → 立即放行；没有额度 → 预约下一个槽位并睡到那时。
//
// slot（本次最早可开始的时刻）= max(nextStart, now)：
// 有额度时 nextStart 落后于 now（空闲期），取 now；没额度时它已被前面的请求
// 推到将来，取它——因此并发的后来者一定排在前面那个之后，不会挤在同一时刻。
// 每次放行都把 nextStart 推到 slot + interval，保证后续请求的间隔不被压缩。
func (l *Limiter) Wait(ctx context.Context) error {
	if l == nil {
		return nil
	}

	now := l.now()

	l.mu.Lock()
	// 1) 结算空闲额度，但只在"时间轴没被预约到将来"时进行：
	//    时间轴在将来意味着还有排队中的请求，那些等待时间已经分配给它们了，
	//    不能再折算成新额度（否则连续请求会出现比 1/rate 更短的间隔，
	//    实测会把 3 个突发一路拖到 1.5s）。
	//
	//    注意 lastRefill 只在真正结算时才前移：若不管有没有结算都重置它，
	//    排队排到将来的那段时间会被永久"吞掉"，批次跑完后要等很久才能恢复到
	//    正常速率（空闲额度再也补不回来）。
	if !l.nextStart.After(now) {
		if idle := now.Sub(l.lastRefill); idle > 0 {
			l.credit = min(l.capacity, l.credit+idle.Seconds()*l.rate)
		}
		l.lastRefill = now
	}

	if l.credit >= 1 {
		// 2a) 有额度：立即放行。
		// 时间轴按"剩余额度"用掉这么多间隔：连续突发期间时间轴保持不动，
		// 突发结束后第一个排队请求就能马上开始（否则突发会把时间轴推前
		// burst 个间隔，8 个请求会拖到 3.5s 而不是 2.5s）。
		// 等到额度耗尽后再恢复正常推进。
		l.credit--
		if l.credit < 1 {
			// 额度用尽：此刻是这一串突发的最后一发，时间轴从这一发开始计时。
			l.nextStart = now.Add(l.interval)
		}
		l.mu.Unlock()
		return nil
	}

	// 2b) 没有额度：预约下一个槽位，等到那时再放行。
	slot := l.nextStart
	if slot.Before(now) {
		slot = now
	}
	l.nextStart = slot.Add(l.interval)
	l.mu.Unlock()

	return l.sleep(ctx, slot.Sub(now))
}

// sleepCtx 等待 d，期间响应 ctx 取消。
// d <= 0 时也要检查一次 ctx，保证"已取消"能被调用方看到。
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
