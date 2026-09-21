//go:build !windows

package updater

import (
	"os/exec"
	"syscall"
)

// spawnDetached 脱离会话组启动替换脚本，Agent 退出后脚本继续运行
func spawnDetached(scriptPath string) error {
	cmd := exec.Command("/bin/sh", scriptPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Start()
}
