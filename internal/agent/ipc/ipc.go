package ipc

import (
	"bufio"
	"encoding/json"
	"net"
	"time"
)

type Request struct {
	Op       string `json:"op"`
	Password string `json:"password,omitempty"`
}

const (
	OpStatus   = "status"
	OpQuit     = "quit"
	OpVersion  = "version"
)

type Response struct {
	Code        int    `json:"code"`
	Message     string `json:"message"`
	Hostname    string `json:"hostname,omitempty"`
	InternalIPs []string `json:"internal_ips,omitempty"`
	PublicIP    string `json:"public_ip,omitempty"`
	UptimeSec   int64  `json:"uptime_sec,omitempty"`
	LastSeen    string `json:"last_seen,omitempty"`
	Version     string `json:"version,omitempty"`
	SpoolPending int   `json:"spool_pending,omitempty"`
}

type Handler interface {
	Handle(req Request) Response
}

func Serve(listener net.Listener, h Handler) error {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		go handleConn(conn, h)
	}
}

func handleConn(conn net.Conn, h Handler) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		return
	}
	var req Request
	if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
		writeJSON(conn, Response{Code: 400, Message: "invalid request"})
		return
	}
	resp := h.Handle(req)
	writeJSON(conn, resp)
}

func writeJSON(conn net.Conn, v any) {
	enc := json.NewEncoder(conn)
	_ = enc.Encode(v)
}

func Call(conn net.Conn, req Request, timeout time.Duration) (Response, error) {
	conn.SetDeadline(time.Now().Add(timeout))
	enc := json.NewEncoder(conn)
	if err := enc.Encode(req); err != nil {
		return Response{}, err
	}
	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		return Response{}, scanner.Err()
	}
	var resp Response
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		return Response{}, err
	}
	return resp, nil
}
