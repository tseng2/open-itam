package ipc

import (
	"fmt"
	"net"

	"github.com/Microsoft/go-winio"
)

func ListenWindows() (net.Listener, error) {
	// 服务端以 SYSTEM 运行，tray 是普通登录用户；默认管道 ACL 只允许 SYSTEM/Administrators，需显式放开
	return winio.ListenPipe(`\\.\pipe\itagent`, &winio.PipeConfig{
		SecurityDescriptor: "D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;GA;;;WD)",
	})
}

func DialWindows() (net.Conn, error) {
	return winio.DialPipe(`\\.\pipe\itagent`, nil)
}

const pipePath = `\\.\pipe\itagent`

var _ = fmt.Sprintf
