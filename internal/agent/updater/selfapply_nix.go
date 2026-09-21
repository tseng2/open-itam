//go:build !windows

package updater

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

// spawnApply 脱离会话组启动新包的自应用流程
func spawnApply(newExe, installDir string, parentPID int) error {
	cmd := exec.Command(newExe,
		"--apply-update",
		"--apply-parent-pid", strconv.Itoa(parentPID),
		"--apply-install-dir", installDir)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Start()
}

// RunSelfApply 新包自替换入口：等旧 Agent 退出 → 备份 → 覆盖 → 重新拉起
func RunSelfApply(installDir string, parentPID int) error {
	updateDir := filepath.Join(installDir, "data", "update")
	logf := func(format string, args ...any) {
		f, err := os.OpenFile(filepath.Join(updateDir, "update.log"),
			os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return
		}
		defer f.Close()
		fmt.Fprintf(f, time.Now().UTC().Format(time.RFC3339)+" "+format+"\n", args...)
	}
	newExe := filepath.Join(updateDir, "core-agent.new.exe")
	target := filepath.Join(installDir, "bin", "core-agent.exe")

	if parentPID > 0 {
		deadline := time.Now().Add(60 * time.Second)
		for time.Now().Before(deadline) {
			if err := syscall.Kill(parentPID, 0); err != nil {
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
	}
	logf("parent %d exited, applying update", parentPID)

	if err := copyFile(target, target+".bak"); err != nil {
		logf("backup old exe: %v", err)
		return err
	}
	if err := copyFile(newExe, target); err != nil {
		logf("replace exe: %v", err)
		return err
	}
	// 重新拉起（console 模式代理，无独立服务管理器）
	if err := exec.Command(target).Start(); err != nil {
		logf("restart failed: %v", err)
		return err
	}
	logf("update applied")
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
