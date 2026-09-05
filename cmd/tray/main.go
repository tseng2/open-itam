//go:build windows

package main

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/getlantern/systray"

	trayipc "itagent/internal/agent/ipc"
	"itagent/internal/agent/password"
)

const (
	title   = "IT Agent"
	version = "0.1.0"
)

//go:embed icon.ico
var iconData []byte

var passwordHash atomic.Value

func main() {
	cfgPath := filepath.Join(filepath.Dir(os.Args[0]), "..", "configs", "agent.password")
	if hash, err := os.ReadFile(cfgPath); err == nil {
		passwordHash.Store(strings.TrimSpace(string(hash)))
	}
	systray.Run(onReady, onExit)
}

func onReady() {
	systray.SetIcon(iconData)
	systray.SetTitle(title)
	systray.SetTooltip("IT Agent 运行中")

	status := systray.AddMenuItem("查询状态…", "查看 IP/版本")
	systray.AddSeparator()
	quit := systray.AddMenuItem("退出 Agent", "需要管理员密码")

	go func() {
		for {
			refreshTooltip()
			time.Sleep(5 * time.Minute)
		}
	}()

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

func queryStatus() (string, error) {
	conn, err := trayipc.Dial()
	if err != nil {
		return "", err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(30 * time.Second))
	resp, err := trayipc.Call(conn, trayipc.Request{Op: trayipc.OpStatus}, 30*time.Second)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s\nIP: %s\n运行: %ds\n待传: %d 条",
		resp.Hostname, strings.Join(resp.InternalIPs, ", "), resp.UptimeSec, resp.SpoolPending), nil
}

func refreshTooltip() {
	msg, err := queryStatus()
	if err != nil {
		systray.SetTooltip("IT Agent（core-agent 未连接）")
		return
	}
	systray.SetTooltip("IT Agent 运行中\n" + msg)
}

func showStatus() {
	msg, err := queryStatus()
	if err != nil {
		showMessage("IT Agent 状态", "查询失败: "+err.Error())
		return
	}
	systray.SetTooltip("IT Agent 运行中\n" + msg)
	showMessage("IT Agent 状态", msg)
}

func showMessage(title, text string) {
	t, _ := syscall.UTF16PtrFromString(title)
	m, _ := syscall.UTF16PtrFromString(text)
	syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW").Call(0,
		uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), 0)
}

func tryQuit() bool {
	hash := passwordHash.Load()
	if hash == nil {
		systray.SetTooltip("未设置退出密码，无法退出")
		return false
	}
	pwd, ok := promptPassword()
	if !ok {
		return false
	}
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

// GUI 进程没有控制台，借用 PowerShell InputBox 弹密码框
func promptPassword() (string, bool) {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
		`Add-Type -AssemblyName Microsoft.VisualBasic; [Microsoft.VisualBasic.Interaction]::InputBox('请输入退出密码', 'IT Agent', '')`)
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	pwd := strings.TrimSpace(string(out))
	return pwd, pwd != ""
}
