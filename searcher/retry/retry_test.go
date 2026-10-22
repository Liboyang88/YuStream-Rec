package retry

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"os"
	"testing"
	"time"
)

// noBackoff 让测试不等退避时间。
func noBackoff(maxRetries int) Config {
	return Config{MaxRetries: maxRetries, Backoff: 0}
}

// quiet 关闭日志输出，避免用例里的重试日志刷屏。
func quiet(t *testing.T) {
	t.Helper()
	old := log.Writer()
	log.SetOutput(io.Discard)
	t.Cleanup(func() { log.SetOutput(old) })
}

func TestDoSucceedsFirstTry(t *testing.T) {
	quiet(t)
	calls := 0
	err := Do(context.Background(), noBackoff(3), "测试", func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if calls != 1 {
		t.Errorf("调用 %d 次, want 1（首次成功不应重试）", calls)
	}
}

func TestDoRetriesThenSucceeds(t *testing.T) {
	quiet(t)
	const failures = 2
	calls := 0
	err := Do(context.Background(), noBackoff(3), "测试", func() error {
		calls++
		if calls <= failures {
			return errors.New("临时故障")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if calls != failures+1 {
		t.Errorf("调用 %d 次, want %d", calls, failures+1)
	}
}

// TestDoGivesUpAfterMaxRetries 失败时最多尝试 1+MaxRetries 次，并返回最后一次的错误。
func TestDoGivesUpAfterMaxRetries(t *testing.T) {
	quiet(t)
	calls := 0
	wantErr := errors.New("一直失败")
	err := Do(context.Background(), noBackoff(3), "测试", func() error {
		calls++
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	if calls != 4 {
		t.Errorf("调用 %d 次, want 4（首次 + 重试 3 次）", calls)
	}
}

// TestDoZeroRetries MaxRetries <= 0 时只调用一次。
func TestDoZeroRetries(t *testing.T) {
	quiet(t)
	for _, maxRetries := range []int{0, -1} {
		calls := 0
		err := Do(context.Background(), noBackoff(maxRetries), "测试", func() error {
			calls++
			return errors.New("失败")
		})
		if err == nil {
			t.Fatalf("MaxRetries=%d 时期望报错", maxRetries)
		}
		if calls != 1 {
			t.Errorf("MaxRetries=%d 时调用 %d 次, want 1", maxRetries, calls)
		}
	}
}

// TestDoStopsOnContextCancel ctx 取消后不再重试。
func TestDoStopsOnContextCancel(t *testing.T) {
	quiet(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	calls := 0
	err := Do(ctx, Config{MaxRetries: 3, Backoff: time.Hour}, "测试", func() error {
		calls++
		return errors.New("失败")
	})
	if err == nil {
		t.Fatal("ctx 已取消时期望返回错误")
	}
	if calls != 1 {
		t.Errorf("调用 %d 次, want 1（取消后不应继续重试）", calls)
	}
}

// TestDoLogsRetries 每次重试都应有日志，便于排查外部接口抖动。
func TestDoLogsRetries(t *testing.T) {
	var buf bytes.Buffer
	oldWriter := log.Writer()
	oldStdout := os.Stdout
	log.SetOutput(&buf)
	os.Stdout = oldStdout
	t.Cleanup(func() { log.SetOutput(oldWriter) })

	_ = Do(context.Background(), noBackoff(2), "生成文献介绍", func() error {
		return errors.New("boom")
	})

	out := buf.String()
	if !bytes.Contains([]byte(out), []byte("生成文献介绍")) {
		t.Errorf("重试日志缺少操作名:\n%s", out)
	}
	if !bytes.Contains([]byte(out), []byte("1/3")) || !bytes.Contains([]byte(out), []byte("2/3")) {
		t.Errorf("重试日志缺少次数信息:\n%s", out)
	}
}

// TestPermanentNotRetried 被标记为 Permanent 的错误不重试，立刻返回。
func TestPermanentNotRetried(t *testing.T) {
	quiet(t)
	calls := 0
	wantErr := errors.New("InvalidApiKey")
	err := Do(context.Background(), Config{MaxRetries: 3, Backoff: time.Hour}, "翻译", func() error {
		calls++
		return Permanent(wantErr)
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v（Unwrap 应保留原始错误）", err, wantErr)
	}
	if calls != 1 {
		t.Errorf("调用 %d 次, want 1（不可重试的错误不应重试）", calls)
	}
	if !IsPermanent(err) {
		t.Error("IsPermanent 应识别被包装的错误")
	}
	if IsPermanent(wantErr) || IsPermanent(nil) {
		t.Error("普通错误与 nil 不应被判定为不可重试")
	}
}

// TestPermanentWrapperKeepsMessage Permanent 包装后错误信息不变。
func TestPermanentWrapperKeepsMessage(t *testing.T) {
	err := Permanent(errors.New("boom"))
	if err.Error() != "boom" {
		t.Errorf("Error() = %q, want boom", err.Error())
	}
	if Permanent(nil) != nil {
		t.Error("Permanent(nil) 应为 nil")
	}
}
