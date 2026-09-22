//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc/mgr"

	"itagent/internal/agent/config"
	"itagent/internal/agent/protection"
	"itagent/internal/agent/reporter"
	"itagent/internal/agent/updater"
	"itagent/internal/shared/password"
	"itagent/internal/shared/protocol"
)

// 与 install_windows.ps1 的安装布局固定绑定，若安装布局变更需同步调整
const (
	uninstallServiceName = "ITAgentService"
	uninstallTrayTask    = "ITAgentTray"
	regPathSoftware      = `SOFTWARE\ITAgent`
)

// RunUninstall 卸载主入口（Go 化，替代 ps1 卸载脚本，绿盾环境不再被解释器拦截）：
// 验证门禁 → 停看门狗 → 停删服务 → 清注册表与文件 → 自删，全过程写 data/uninstall.log
func RunUninstall(codeFlag string) error {
	installDir := updateExeInstallDir()
	logf := uninstallLogger(filepath.Join(installDir, "data", "uninstall.log"))

	policy, hasPolicy := protection.Load()
	if err := verifyUninstallGate(policy, hasPolicy, codeFlag, logf); err != nil {
		logf("uninstall rejected: %v", err)
		return err
	}
	logf("uninstall gate passed")

	// 停看门狗：避免其与卸载动作竞争拉起服务；未运行属正常路径，记录不中断
	if out, err := exec.Command("taskkill", "/F", "/IM", "agent-watchdog.exe").CombinedOutput(); err != nil {
		logf("stop watchdog: %v (%s)", err, strings.TrimSpace(string(out)))
	}

	stopAndDeleteService(logf)

	// 删 tray 登录计划任务（Go 化，不再依赖 PowerShell 卸载脚本）
	if out, err := exec.Command("schtasks", "/Delete", "/TN", uninstallTrayTask, "/F").CombinedOutput(); err != nil {
		logf("delete tray task: %v (%s)", err, strings.TrimSpace(string(out)))
	}

	// 清理身份与防护的注册表持久化
	if err := clearIdentityRegistry(); err != nil {
		logf("clear registry: %v", err)
	}

	logf("uninstall complete, removing install dir")
	logf("self delete spawned")
	selfDelete(installDir)
	return nil
}

// uninstallLogger 追加式现场日志：卸载过程无可见 stderr 时落盘供排查
func uninstallLogger(path string) func(string, ...any) {
	return func(format string, args ...any) {
		entry := time.Now().UTC().Format(time.RFC3339) + " " + fmt.Sprintf(format, args...) + "\n"
		if f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			_, _ = f.WriteString(entry)
			f.Close()
		}
		log.Printf(format, args...)
	}
}

// verifyUninstallGate 卸载门禁（QAX 同款逻辑）：模块关闭 → 免验证；
// 开启时密码（本地）或验证码（在线）二选一；都缺失或校验失败即拒绝（fail-closed）
func verifyUninstallGate(policy protection.Policy, hasPolicy bool, code string, logf func(string, ...any)) error {
	if !hasPolicy || !policy.Uninstall.Enabled {
		return nil
	}
	if code != "" {
		return verifyUninstallCodeOnline(code, logf)
	}
	if pass := promptConsole("输入卸载密码: "); pass != "" {
		if policy.Uninstall.PasswordHash == "" {
			return fmt.Errorf("卸载防护已启用但服务端未设置密码，仅支持在线验证码")
		}
		if password.Verify(pass, policy.Uninstall.PasswordHash) {
			return nil
		}
		logf("local password mismatch, fallback to online code")
	}
	codeInput := promptConsole("输入在线验证码（管理员提供，直接回车放弃）: ")
	if codeInput == "" {
		return fmt.Errorf("未提供任何有效凭据，拒绝卸载")
	}
	return verifyUninstallCodeOnline(codeInput, logf)
}

// verifyUninstallCodeOnline 在线校验一次性验证码：走 device token 通道，
// 服务端执行单次使用、10 分钟过期与设备绑定校验，通过即标记已用
func verifyUninstallCodeOnline(code string, logf func(string, ...any)) error {
	cfg, err := config.Load(filepath.Join(updateExeInstallDir(), "configs", "agent.json"))
	if err != nil || cfg.DeviceToken == "" {
		return fmt.Errorf("本地凭据不可用，无法在线校验验证码（离线终端请使用本地密码）")
	}
	u := reporter.NewUploader(cfg.ServerPrimary, cfg.ServerBackup, cfg.DeviceID, cfg.DeviceToken, nil)
	body, err := json.Marshal(protocol.UninstallCodeVerifyRequest{Code: code})
	if err != nil {
		return err
	}
	path := "/api/v1/agent/uninstall-code/verify?device_id=" + cfg.DeviceID
	if _, err := u.PostJSON(path, body); err != nil {
		logf("online code verify failed: %v", err)
		return fmt.Errorf("验证码校验失败: %w", err)
	}
	logf("online code verified")
	return nil
}

// promptConsole 交互会话的控制台单行输入（管理员终端场景，明文回显可接受）
func promptConsole(prompt string) string {
	fmt.Print(prompt)
	var input string
	fmt.Scanln(&input)
	return strings.TrimSpace(input)
}

// stopAndDeleteService 停止并删除 ITAgentService；服务不存在或删除失败记录后继续
// 清理文件（卸载不因单步失败中断，残余服务可后续手动删除）
func stopAndDeleteService(logf func(string, ...any)) {
	m, err := mgr.Connect()
	if err != nil {
		logf("connect scm: %v", err)
		return
	}
	defer m.Disconnect()
	s, err := m.OpenService(uninstallServiceName)
	if err != nil {
		logf("open service: %v", err)
		return
	}
	defer s.Close()
	if !updater.StopService(s) {
		logf("service stop timeout, continue cleanup")
	}
	if err := s.Delete(); err != nil {
		logf("delete service: %v", err)
		return
	}
	logf("service deleted")
}

// clearIdentityRegistry 删除 SOFTWARE\ITAgent 键（DeviceID + Protection 持久化，
// 键下仅值无子键），HKLM 失败降级 HKCU；键不存在视为已清理
func clearIdentityRegistry() error {
	for _, root := range []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER} {
		k, err := registry.OpenKey(root, regPathSoftware, registry.SET_VALUE)
		if err != nil {
			continue
		}
		k.Close()
		if err := registry.DeleteKey(root, regPathSoftware); err != nil {
			continue
		}
		return nil
	}
	return nil
}

// selfDelete 运行中的二进制无法删除自身：spawn 脱离的 cmd 延迟清理安装目录，
// 等待本进程退出后执行 rd（交互会话下 cmd 不受终端安全软件拦截）
func selfDelete(installDir string) {
	cmd := exec.Command("cmd.exe", "/c", "ping -n 3 127.0.0.1 > nul & rd /s /q \""+installDir+"\"")
	cmd.SysProcAttr = &windows.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS,
	}
	if err := cmd.Start(); err != nil {
		log.Printf("spawn self delete: %v", err)
	}
	os.Exit(0)
}
