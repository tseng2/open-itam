package ipc

import (
	"net"
	"runtime"
)

// 平台无关工厂
func Listen() (net.Listener, error) {
	if runtime.GOOS == "windows" {
		return ListenWindows()
	}
	return nil, nil
}

func Dial() (net.Conn, error) {
	if runtime.GOOS == "windows" {
		return DialWindows()
	}
	return nil, nil
}

