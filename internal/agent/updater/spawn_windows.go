//go:build windows

package updater

import (
	"os/exec"

	"golang.org/x/sys/windows"
)

// spawnDetached 以脱离父进程的方式启动替换脚本：Agent 进程退出后脚本仍继续运行
func spawnDetached(scriptPath string) error {
	cmd := exec.Command("powershell",
		"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-WindowStyle", "Hidden", "-File", scriptPath)
	cmd.SysProcAttr = &windows.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS,
	}
	// 脚本不继承父进程的标准句柄，避免句柄占用导致进程组无法解散
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Start()
}
