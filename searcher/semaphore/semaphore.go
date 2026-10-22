// Package semaphore 提供一个带容量的并发闸门，用于限制同时飞向外部接口的请求数。
//
// 抓取入库时每篇论文都要调用 LLM（文献介绍）与机器翻译（翻译），串行执行会把时间
// 全部花在等待网络回应上。加速的办法是让多篇论文同时处理，但又不能无限并发——
// 外部接口有 QPS / 并发 / 额度限制，打满只会换来限流与重试。
//
// 用法（Acquire 返回的错误必须交给调用方处理，否则会丢掉 ctx 取消的原因）：
//
//	if err := gate.Acquire(ctx); err != nil {
//		return err
//	}
//	defer gate.Release()
//	// ... 发起一次接口调用
package semaphore

import "context"

// Semaphore 是容量固定的并发闸门。nil 表示不限流，Acquire 永远立即成功、Release 为空操作，
// 从而让调用方不必为"没有配置上限"写分支。
type Semaphore struct {
	slots chan struct{}
}

// New 创建一个最多允许 n 个并发持有者的闸门。
// n <= 0 表示不限流，返回 nil（在 nil 上调用 Acquire / Release 仍然安全）。
func New(n int) *Semaphore {
	if n <= 0 {
		return nil
	}
	return &Semaphore{slots: make(chan struct{}, n)}
}

// Acquire 占用一个名额；名额已满时阻塞，直到有人释放或 ctx 被取消。
// ctx 取消时返回 ctx.Err()，调用方应当停止后续的接口调用。
func (s *Semaphore) Acquire(ctx context.Context) error {
	if s == nil {
		return nil
	}
	select {
	case s.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Release 归还一个名额，必须与成功的 Acquire 一一对应（约定用 defer）。
// nil 接收者上调用是空操作。
func (s *Semaphore) Release() {
	if s == nil {
		return
	}
	<-s.slots
}

// Cap 返回闸门容量；nil（不限流）返回 0。
func (s *Semaphore) Cap() int {
	if s == nil {
		return 0
	}
	return cap(s.slots)
}
