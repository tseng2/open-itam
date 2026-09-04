package ipc

import (
	"context"

	"itagent/internal/agent/collector"
	"itagent/internal/agent/config"
	"itagent/internal/agent/reporter"
	"itagent/internal/shared/protocol"
)

type Server struct {
	cfg       config.Config
	collector *collector.Collector
	spool     *reporter.Spool
	uploader  *reporter.Uploader
	quit      chan struct{}
}

func NewServer(cfg config.Config, c *collector.Collector, s *reporter.Spool, u *reporter.Uploader) *Server {
	return &Server{cfg: cfg, collector: c, spool: s, uploader: u}
}

func (s *Server) QuitChan() <-chan struct{} { return s.quit }

func (s *Server) Serve(ctx context.Context) error {
	l, err := Listen()
	if err != nil {
		return err
	}
	defer l.Close()
	s.quit = make(chan struct{})
	go func() {
		<-ctx.Done()
		l.Close()
	}()
	return Serve(l, s)
}

func (s *Server) Handle(req Request) Response {
	switch req.Op {
	case OpStatus:
		hb := s.collector.Heartbeat()
		n, _, _ := s.spool.Stats()
		return Response{
			Code:         0,
			Hostname:     hb.Hostname,
			InternalIPs:  flattenIPs(hb.Network),
			PublicIP:     hb.Network.PublicIP,
			UptimeSec:    hb.UptimeSec,
			Version:      "0.1.0",
			SpoolPending: n,
		}
	case OpQuit:
		close(s.quit)
		return Response{Code: 0, Message: "quitting"}
	default:
		return Response{Code: 400, Message: "unknown op"}
	}
}

func flattenIPs(network protocol.Network) []string {
	var out []string
	for _, n := range network.Interfaces {
		if !n.IsUp {
			continue
		}
		for _, ip := range n.IPs {
			out = append(out, ip)
		}
	}
	return out
}
