package watchdog

import (
	"context"
	"testing"
)

func TestEnsureRunningContextCancel(t *testing.T) {
	// 这里验证它在 context 取消时退出；真实的 sc start 需要管理员
	// 我们在 1ms 内取消，函数必须立刻退出
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := EnsureRunning(ctx)
	if err != nil {
		t.Fatalf("expected nil after cancel, got %v", err)
	}
}
