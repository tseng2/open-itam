//go:build windows

package watchdog

import (
	"context"
	"os/exec"
	"time"
)

const serviceName = "ITAgentService"

func Status() (string, error) {
	out, err := exec.Command("sc", "query", serviceName).Output()
	return string(out), err
}

func EnsureRunning(ctx context.Context) error {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			_ = exec.Command("sc", "start", serviceName).Run()
		}
	}
}

// 在 core-agent 里调用，启动独立守护进程
func SpawnGuardian() error {
	self, _ := exec.LookPath("core-agent.exe")
	cmd := exec.Command(self, "-guardian")
	return cmd.Start()
}
