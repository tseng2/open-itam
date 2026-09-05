package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"itagent/internal/agent/collector"
	"itagent/internal/agent/config"
	"itagent/internal/agent/identity"
	"itagent/internal/agent/ipc"
	"itagent/internal/agent/reporter"
	"itagent/internal/shared/protocol"
)

const agentVersion = "0.1.0"

var (
	cfgPathFlag = flag.String("config", "configs/agent.json", "path to agent config")
	serviceFlag = flag.Bool("service", false, "run as windows service")
)

func main() {
	flag.Parse()
	if tryRunAsService(*cfgPathFlag, *serviceFlag) {
		return
	}
	runConsole(*cfgPathFlag)
}

func runConsole(cfgPath string) {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	done := make(chan struct{})
	go func() { <-stop; close(done) }()
	innerMain(cfgPath, done)
}

func innerMain(cfgPath string, done <-chan struct{}) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Printf("config: %v", err)
		return
	}
	cfg.AgentVersion = agentVersion

	col := collector.New()
	if cfg.DeviceID == "" {
		id, err := identity.DeviceID(identity.PlatformProbe())
		if err != nil {
			log.Printf("device id: %v", err)
			return
		}
		cfg.DeviceID = id
		config.Save(cfgPath, cfg)
	}

	u := reporter.NewUploader(cfg.ServerPrimary, cfg.ServerBackup, cfg.DeviceID, cfg.DeviceToken, nil)
	if cfg.DeviceToken == "" {
		hb := col.Heartbeat()
		tok, err := u.Register(cfg.InstallToken, hb.Hostname, hb.OS.Name, cfg.AgentVersion)
		if err == nil {
			cfg.DeviceToken = tok
			config.Save(cfgPath, cfg)
		} else {
			log.Printf("register retry later: %v", err)
		}
	}

	spool := reporter.NewSpool(cfg.SpoolDir)
	ipcSrv := ipc.NewServer(cfg, col, spool, u)
	go ipcSrv.Serve(context.Background())

	log.Printf("ITAgent v%s dev=%s", agentVersion, cfg.DeviceID)

	hb := time.Now()
	full := time.Now().Add(15 * time.Second)
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()

	for {
		select {
		case <-done:
			log.Printf("stopping")
			return
		case now := <-tick.C:
			if now.After(hb) {
				enqueue(spool, cfg, col, "heartbeat")
				hb = now.Add(10 * time.Minute)
			}
			if now.After(full) {
				enqueue(spool, cfg, col, "full")
				full = now.Add(time.Hour)
			}
		}
	}
}

func enqueue(spool *reporter.Spool, cfg config.Config, col *collector.Collector, typ string) {
	var payload []byte
	if typ == "full" {
		payload, _ = json.Marshal(col.Full())
	} else {
		payload, _ = json.Marshal(col.Heartbeat())
	}
	env := protocol.Envelope{DeviceID: cfg.DeviceID, AgentVersion: agentVersion, ReportType: typ, ReportedAt: time.Now(), Payload: payload}
	raw, _ := json.Marshal(env)
	_, _ = spool.Enqueue(raw)

	items, _ := spool.Pending()
	for _, item := range items {
		if d, err := spool.Read(item); err == nil {
			var m protocol.Envelope
			_ = json.Unmarshal(d, &m)
			if resp, err := reporter.NewUploader(cfg.ServerPrimary, cfg.ServerBackup, cfg.DeviceID, cfg.DeviceToken, nil).Upload(m); err == nil && resp != nil {
				spool.Delete(item)
			}
		}
	}
}
