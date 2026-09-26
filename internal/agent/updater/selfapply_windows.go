//go:build windows

package updater

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// applyServiceName 安装布局（install_windows.ps1）固定的服务名；
// 若安装脚本变更需同步调整
const applyServiceName = "ITAgentService"

// processSynchronize 进程同步访问权（Windows SDK 标准值），用于等待旧进程退出
const processSynchronize = 0x00100000

// spawnApply 以脱离父进程的方式启动新包的自应用流程：
// 新包进程在 Agent 退出后继续完成替换并拉起服务
func spawnApply(newExe, installDir string, parentPID int) error {
	cmd := exec.Command(newExe,
		"--apply-update",
		"--apply-parent-pid", strconv.Itoa(parentPID),
		"--apply-install-dir", installDir)
	cmd.SysProcAttr = &windows.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS,
	}
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", newExe, err)
	}
	return nil
}

// RunSelfApply 新包自替换入口：等旧 Agent 退出 → 停服务 →
// 备份旧版 → 用自己覆盖 → 起服务（失败回滚），全过程写 update.log。
// 死路修复（2026-09-26，.160 离线案）：本函数是服务停止后系统里唯一
// 能把它拉起的组件（watchdog 已被杀、SCM failure recovery 不覆盖
// 正常 stop），因此任何失败 return 前都必须尽力把服务重启回去——
// backup/replace 失败绝不允许留一个「已停且无人救」的服务
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
	backup := target + ".bak"

	waitProcessExit(parentPID, 60*time.Second)
	logf("parent %d exited, applying update", parentPID)

	// 停看门狗：避免其拉起旧服务与替换动作竞争（若部署未启用则空转）
	_ = exec.Command("taskkill", "/F", "/IM", "agent-watchdog.exe").Run()

	m, err := mgr.Connect()
	if err != nil {
		logf("scm connect: %v", err)
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(applyServiceName)
	if err != nil {
		logf("open service: %v", err)
		return err
	}
	defer s.Close()

	// 之后所有失败路径服务已停：必须先救活服务再返回（回滚旧包兜底）
	logf("service stop requested, stopped=%v", StopService(s))

	if err := copyFile(target, backup); err != nil {
		logf("backup old exe: %v; restart service anyway", err)
		_ = s.Start()
		return err
	}
	if err := copyFile(newExe, target); err != nil {
		logf("replace exe: %v; rollback and restart", err)
		_ = copyFile(backup, target)
		_ = s.Start()
		return err
	}
	if err := s.Start(); err != nil {
		logf("start new failed: %v; rollback", err)
		_ = copyFile(backup, target)
		_ = s.Start()
		return err
	}
	if !waitServiceRunning(s, 30*time.Second) {
		logf("service not running after update; rollback")
		_ = copyFile(backup, target)
		_ = s.Start()
		return fmt.Errorf("service not running after update")
	}
	logf("update applied, service running")
	return nil
}

// StopService 停止 SCM 服务：已停直接返回，否则发停止指令并轮询至 30 秒超时
func StopService(s *mgr.Service) bool {
	if st, err := s.Query(); err == nil && st.State == svc.Stopped {
		return true
	}
	st, err := s.Control(svc.Stop)
	if err != nil {
		return false
	}
	if st.State == svc.Stopped {
		return true
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		if cur, err := s.Query(); err == nil && cur.State == svc.Stopped {
			return true
		}
	}
	return false
}

func waitServiceRunning(s *mgr.Service, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if st, err := s.Query(); err == nil && st.State == svc.Running {
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

func waitProcessExit(pid int, timeout time.Duration) {
	if pid <= 0 {
		return
	}
	h, err := windows.OpenProcess(processSynchronize, false, uint32(pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		event, err := windows.WaitForSingleObject(h, 1000)
		if err == nil && event == windows.WAIT_OBJECT_0 {
			return
		}
	}
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
