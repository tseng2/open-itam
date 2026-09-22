//go:build embed

// Package main 实现 ITAgent 单文件安装器：全部组件经 go:embed 内嵌，
// 一条命令完成安装。不走 PowerShell——绿盾等终端安全软件会拦截解释器脚本，
// 自研二进制在管理员控制台运行不受影响；单文件形态适合 AD 域 GPO 启动脚本
// 与绿盾批量推送。构建方式：先组装 assets/（见 build_prod.ps1），再
// go build -tags embed ./cmd/itagent-setup
package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"
)

//go:embed assets
var assets embed.FS

const serviceName = "ITAgentService"

func main() {
	server := flag.String("server", "", "server base url, e.g. http://10.1.1.96:8443")
	token := flag.String("token", "", "install token issued by server")
	password := flag.String("password", "Admin@12345", "agent quit/uninstall password")
	installDir := flag.String("dir", `C:\ProgramData\ITAgent`, "install directory")
	flag.Parse()

	if !windows.GetCurrentProcessToken().IsElevated() {
		fmt.Println("ERROR: run as Administrator")
		os.Exit(1)
	}
	if *server == "" || *token == "" {
		flag.Usage()
		os.Exit(1)
	}
	if err := install(*server, *token, *password, *installDir); err != nil {
		fmt.Printf("install failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Install OK, service started")
}

func install(server, token, password, dir string) error {
	fmt.Println("=== IT Agent Install ===")
	killAgentProcesses()

	for _, sub := range []string{"bin", "configs", "tools"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return err
		}
	}
	if err := extractPayloads(dir); err != nil {
		return err
	}
	if err := writeQuitPassword(dir, password); err != nil {
		return err
	}
	if err := writeConfig(dir, server, token); err != nil {
		return err
	}
	if err := recreateService(dir); err != nil {
		return err
	}
	// 非致命：Server Core 无桌面会话时允许失败，不影响核心采集
	registerTrayTask(dir)
	return startService()
}

// killAgentProcesses 停掉旧 Agent 全部进程：文件被占用会导致组件覆盖失败
func killAgentProcesses() {
	for _, name := range []string{"core-agent.exe", "tray.exe", "agent-watchdog.exe"} {
		_ = exec.Command("taskkill", "/F", "/IM", name).Run()
	}
}

// extractPayloads 把内嵌组件解包到安装目录：主组件进 bin，
// verify/smartctl 工具进 tools，配置模板进 configs
func extractPayloads(dir string) error {
	return fs.WalkDir(assets, "assets", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := assets.ReadFile(path)
		if err != nil {
			return err
		}
		name := filepath.Base(path)
		sub := "bin"
		switch name {
		case "verify.exe", "smartctl.exe":
			sub = "tools"
		case "agent.json":
			sub = "configs"
		}
		return os.WriteFile(filepath.Join(dir, sub, name), data, 0o755)
	})
}

// writeQuitPassword 生成退出/卸载密码哈希（Argon2id，绝不存明文）；
// 复用内嵌 verify 工具，保证与 Agent 校验格式完全一致
func writeQuitPassword(dir, password string) error {
	out, err := exec.Command(filepath.Join(dir, "tools", "verify.exe"), "hash", password).Output()
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "configs", "agent.password"),
		bytes.TrimSpace(out), 0o644)
}

// writeConfig 生成 agent.json：新装/重装必须是无身份状态，device_id 由
// agent 按本机硬件现场生成，绝不能从模板继承（两机共用同一指纹的坑真实发生过）
func writeConfig(dir, server, token string) error {
	tpl, err := assets.ReadFile("assets/agent.json")
	if err != nil {
		return err
	}
	var cfg map[string]any
	if err := json.Unmarshal(tpl, &cfg); err != nil {
		return err
	}
	cfg["install_token"] = token
	cfg["server_primary"] = server
	cfg["spool_dir"] = filepath.Join(dir, "data", "spool")
	cfg["device_id"] = ""
	cfg["device_token"] = ""
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "configs", "agent.json"), raw, 0o644)
}

// recreateService 重建服务并配置失败自动重启
func recreateService(dir string) error {
	binPath := fmt.Sprintf(`"%s\bin\core-agent.exe" -config "%s\configs\agent.json"`, dir, dir)
	_ = exec.Command("sc.exe", "stop", serviceName).Run()
	time.Sleep(2 * time.Second)
	_ = exec.Command("sc.exe", "delete", serviceName).Run()
	time.Sleep(2 * time.Second)
	if out, err := exec.Command("sc.exe", "create", serviceName,
		"binPath=", binPath, "start=", "auto").CombinedOutput(); err != nil {
		return fmt.Errorf("create service: %s", out)
	}
	if out, err := exec.Command("sc.exe", "failure", serviceName,
		"reset=", "60", "actions=", "restart/5000/restart/10000/restart/30000").CombinedOutput(); err != nil {
		return fmt.Errorf("config failure recovery: %s", out)
	}
	return nil
}

// registerTrayTask 注册登录自启的托盘任务
func registerTrayTask(dir string) {
	_ = exec.Command("schtasks", "/Create", "/TN", "ITAgentTray",
		"/TR", filepath.Join(dir, "bin", "tray.exe"),
		"/SC", "ONLOGON", "/RL", "LIMITED", "/RU", os.Getenv("USERNAME"), "/F").Run()
}

func startService() error {
	if out, err := exec.Command("sc.exe", "start", serviceName).CombinedOutput(); err != nil {
		return fmt.Errorf("start service: %s", out)
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		out, err := exec.Command("sc.exe", "query", serviceName).Output()
		if err == nil && bytes.Contains(out, []byte("RUNNING")) {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("service not running after 30s")
}
