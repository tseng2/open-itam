package licensealert

import (
	"context"
	"testing"
	"time"

	"itagent/internal/server/store"
)

// Run 韧性用例独立成文件：默认间隔收敛 + DB 故障轮次不崩溃
//（与 engine_test.go 的正常节奏用例互补）

func TestRunDefaultIntervalAndScanFailureResilience(t *testing.T) {
	rec := &notifyRecorder{}
	engine, _ := setupEngine(t, rec.handle)
	seedLicenseFixture(t)

	// interval<=0 收敛默认（1h）：启动扫描即完成两笔投递，取消即退
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		engine.Run(ctx, 0)
		close(done)
	}()
	deadline := time.After(5 * time.Second)
	tick := time.After(0)
	for rec.count() < 2 {
		select {
		case <-deadline:
			cancel()
			t.Fatalf("startup scan must deliver, delivered=%d", rec.count())
		case <-tick:
			tick = time.After(20 * time.Millisecond)
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run must exit promptly after ctx cancel")
	}

	// DB 不可用后扫描轮次失败：引擎记日志不崩溃，ctx 取消仍能退出
	if sqlDB, err := store.DB.DB(); err == nil {
		if err := sqlDB.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}
	ctx2, cancel2 := context.WithCancel(context.Background())
	done2 := make(chan struct{})
	go func() {
		engine.Run(ctx2, 10*time.Millisecond)
		close(done2)
	}()
	time.Sleep(120 * time.Millisecond) // 覆盖若干失败轮次（启动扫描 + ticker 轮）
	cancel2()
	select {
	case <-done2:
	case <-time.After(2 * time.Second):
		t.Fatal("Run must survive scan failures and exit on cancel")
	}
}
