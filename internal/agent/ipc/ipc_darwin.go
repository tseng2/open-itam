//go:build darwin

package ipc

import (
	"net"
	"os"
)

func ListenUnix() (net.Listener, error) {
	path := "/var/run/itagent.sock"
	os.Remove(path)
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	os.Chmod(path, 0o600)
	return l, nil
}

func DialUnix() (net.Conn, error) {
	return net.Dial("unix", "/var/run/itagent.sock")
}
