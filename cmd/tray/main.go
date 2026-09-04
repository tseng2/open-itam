//go:build windows

package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/getlantern/systray"

	trayipc "itagent/internal/agent/ipc"
	"itagent/internal/agent/password"
)

const (
	title   = "IT Agent"
	version = "0.1.0"
)

var passwordHash atomic.Value

func main() {
	cfgPath := filepath.Join(filepath.Dir(os.Args[0]), "configs", "agent.password")
	if hash, err := os.ReadFile(cfgPath); err == nil {
		passwordHash.Store(strings.TrimSpace(string(hash)))
	}
	systray.Run(onReady, onExit)
}

func onReady() {
	systray.SetTitle(title)
	systray.SetTooltip("IT Agent 运行中")
	systray.SetTooltip("左键显示状态，右键退出")

	status := systray.AddMenuItem("查询状态…", "查看 IP/版本")
	systray.AddSeparator()
	quit := systray.AddMenuItem("退出 Agent", "需要管理员密码")

	go func() {
		for range status.ClickedCh {
			go showStatus()
		}
	}()
	go func() {
		for range quit.ClickedCh {
			if tryQuit() {
				systray.Quit()
				return
			}
		}
	}()
}

func showStatus() {
	conn, err := trayipc.Dial()
	if err != nil {
		systray.SetTooltip(fmt.Sprintf("无法连接 core-agent: %v", err))
		return
	}
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	resp, err := trayipc.Call(conn, trayipc.Request{Op: trayipc.OpStatus}, 5*time.Second)
	if err != nil {
		systray.SetTooltip(fmt.Sprintf("查询失败: %v", err))
		return
	}
	lips := strings.Join(resp.InternalIPs, ", ")
	systray.SetTooltip(fmt.Sprintf("主机: %s\n内网: %s\n公网: %s\nUptime: %ds\nSpool: %d 条待传",
		resp.Hostname, lips, resp.PublicIP, resp.UptimeSec, resp.SpoolPending))
}

func tryQuit() bool {
	hash := passwordHash.Load()
	if hash == nil {
		systray.SetTooltip("未设置退出密码，无法退出")
		return false
	}
	fmt.Print("输入退出密码: ")
	scan := bufio.NewScanner(os.Stdin)
	if !scan.Scan() {
		return false
	}
	pwd := strings.TrimSpace(scan.Text())
	if !password.Verify(pwd, hash.(string)) {
		systray.SetTooltip("密码错误，拒绝退出")
		return false
	}
	conn, err := trayipc.Dial()
	if err == nil {
		_, _ = trayipc.Call(conn, trayipc.Request{Op: trayipc.OpQuit}, 2*time.Second)
	}
	return true
}

func onExit() {}
