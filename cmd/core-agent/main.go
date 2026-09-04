package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	agentipc "itagent/internal/agent/ipc"
	"itagent/internal/agent/collector"
	"itagent/internal/agent/config"
	"itagent/internal/agent/identity"
	"itagent/internal/agent/reporter"
	"itagent/internal/shared/protocol"
)

const agentVersion = "0.1.0"

func main() {
	configPath := flag.String("config", "configs/agent.json", "path to agent config")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	cfg.AgentVersion = agentVersion

	col := collector.New()

	if cfg.DeviceID == "" {
		id, err := identity.DeviceID(identity.PlatformProbe())
		if err != nil {
			log.Fatalf("cannot derive device id: %v", err)
		}
		cfg.DeviceID = id
		if err := config.Save(*configPath, cfg); err != nil {
			log.Fatalf("persist config: %v", err)
		}
		log.Printf("generated device_id=%s", cfg.DeviceID)
	}

	u := reporter.NewUploader(cfg.ServerPrimary, cfg.ServerBackup, cfg.DeviceID, cfg.DeviceToken, nil)
	if cfg.UsingBackup {
		log.Printf("resuming on backup server")
	}

	if cfg.DeviceToken == "" {
		hb := col.Heartbeat()
		token, err := u.Register(cfg.InstallToken, hb.Hostname, hb.OS.Name, agentVersion)
		if err != nil {
			log.Fatalf("register: %v", err)
		}
		cfg.DeviceToken = token
		if err := config.Save(*configPath, cfg); err != nil {
			log.Fatalf("persist token: %v", err)
		}
		log.Printf("registered ok")
	}

	spool := reporter.NewSpool(cfg.SpoolDir)
	backoff := reporter.NewBackoff(5*time.Second, 5*time.Minute)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	heartbeatDue := time.Now()
	fullDue := time.Now().Add(10 * time.Second)

	log.Printf("core-agent %s started: device=%s hb=%ds full=%ds", agentVersion, cfg.DeviceID, cfg.HeartbeatIntervalSec, cfg.FullIntervalSec)

	flush := func() {
		for {
			items, err := spool.Pending()
			if err != nil || len(items) == 0 {
				break
			}
			name := items[0]
			data, err := spool.Read(name)
			if err != nil {
				spool.Delete(name)
				continue
			}
			var env protocol.Envelope
			if err := json.Unmarshal(data, &env); err != nil {
				spool.Delete(name)
				continue
			}
			resp, err := u.Upload(env)
			if err != nil {
				d := backoff.Next()
				log.Printf("upload %s failed (%v); retry in %s", name, err, d)
				time.Sleep(d)
				continue
			}
			if cfg.ApplyServerOverride(resp.NextHeartbeatSec, resp.NextFullSec) {
				log.Printf("server overrode intervals: hb=%ds full=%ds", cfg.HeartbeatIntervalSec, cfg.FullIntervalSec)
			}
			u.MaybeProbePrimary(time.Now())
			backoff.Reset()
			spool.Delete(name)
		}
		cfg.UsingBackup = u.UsingBackup()
		_ = config.Save(*configPath, cfg)
	}

	enqueue := func(reportType string) {
		var payload any
		hb := col.Heartbeat()
		if reportType == protocol.ReportTypeFull {
			payload = col.Full()
		} else {
			payload = hb
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			log.Printf("marshal payload: %v", err)
			return
		}
		env := protocol.Envelope{
			DeviceID: cfg.DeviceID, AgentVersion: agentVersion,
			ReportType: reportType, ReportedAt: time.Now(),
			Payload: raw,
		}
		envRaw, err := json.Marshal(env)
		if err != nil {
			log.Printf("marshal envelope: %v", err)
			return
		}
		if _, err := spool.Enqueue(envRaw); err != nil {
			log.Printf("spool enqueue: %v", err)
		}
		flush()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var ipcSrv *agentipc.Server
	if runtime.GOOS == "windows" {
		ipcSrv = agentipc.NewServer(cfg, col, spool, u)
		go func() {
			if err := ipcSrv.Serve(ctx); err != nil {
				log.Printf("ipc serve: %v", err)
			}
		}()
	}

	ticker := time.NewTicker(time.Duration(cfg.SpoolScanSec) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-sig:
			log.Print("shutting down")
			return
		case <-ticker.C:
		case <-ipcSrv.QuitChan():
			log.Print("ipc quit received")
			return
		}

		now := time.Now()
		if now.After(heartbeatDue) {
			enqueue(protocol.ReportTypeHeartbeat)
			heartbeatDue = now.Add(time.Duration(cfg.HeartbeatIntervalSec) * time.Second)
		}
		if now.After(fullDue) {
			enqueue(protocol.ReportTypeFull)
			fullDue = now.Add(time.Duration(cfg.FullIntervalSec) * time.Second)
		}
		flush()
	}
}
