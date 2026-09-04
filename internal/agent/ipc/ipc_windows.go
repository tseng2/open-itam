package ipc

import (
	"fmt"
	"net"

	"github.com/Microsoft/go-winio"
)

func ListenWindows() (net.Listener, error) {
	return winio.ListenPipe(`\\.\pipe\itagent`, &winio.PipeConfig{})
}

func DialWindows() (net.Conn, error) {
	return winio.DialPipe(`\\.\pipe\itagent`, nil)
}

const pipePath = `\\.\pipe\itagent`

var _ = fmt.Sprintf
