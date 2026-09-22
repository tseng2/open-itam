//go:build embed

// Package main 实现 ITAgent 单文件安装器：全部组件经 go:embed 内嵌，
// 一条命令完成安装。不走 PowerShell——绿盾等终端安全软件会拦截解释器脚本，
// 自研二进制在管理员控制台运行不受影响；单文件形态适合 AD 域 GPO 启动脚本
// 与绿盾批量推送。构建方式：先组装 assets/（见 build_prod.ps1），再
// go build -tags embed ./cmd/itagent-setup
package main

import (
	"bufio"
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

// defaultServer/defaultToken 构建时经 -ldflags "-X main.defaultServer=... -X
// main.defaultToken=..." 烧入本环境默认值：双击即可安装、批量推送无需传参，
// flags 仍可覆盖以适配其他环境
var (
	defaultServer = ""
	defaultToken  = ""
)

func main() {
	server := flag.String("server", defaultServer, "server base url (default baked at build time)")
	token := flag.String("token", defaultToken, "install token (default baked at build time)")
	installDir := flag.String("dir", `C:\ProgramData\ITAgent`, "install directory")
	flag.Parse()

	if !windows.GetCurrentProcessToken().IsElevated() {
		fail("ERROR: run as Administrator")
	}
	if *server == "" || *token == "" {
		fmt.Println("ERROR: -server and -token are required (or bake defaults at build time)")
		flag.Usage()
		fail("")
	}
	err := install(*server, *token, *installDir)
	if err != nil {
		fail("install failed: " + err.Error())
	}
	fmt.Println("Install OK, service started")
	pauseIfInteractive()
}

// fail 打印错误后按需暂停退出：双击场景控制台会立即关闭，错误来不及看
func fail(msg string) {
	if msg != "" {
		fmt.Println(msg)
	}
	pauseIfInteractive()
	os.Exit(1)
}

// pauseIfInteractive 仅在有真实控制台时暂停：GPO/绿盾批量推送无控制台
// 输入立即退出，双击安装能看到结果
func pauseIfInteractive() {
	if fi, err := os.Stdin.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
		fmt.Println()
		fmt.Println("按回车键退出...")
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	}
}

func install(server, token, dir string) error {
	fmt.Println("=== IT Agent Install ===")
	killAgentProcesses()

	// 防退出/防卸载密码不再烧入安装包：归服务端 Web UI 集中配置，
	// Agent 经 agent/config 下发后按注册表持久化
	for _, sub := range []string{"bin", "configs"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return err
		}
	}
	if err := extractPayloads(dir); err != nil {
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
