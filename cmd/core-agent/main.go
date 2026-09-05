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

	"itagent/internal/agent/collector"
	"itagent/internal/agent/config"
	"itagent/internal/agent/identity"
	"itagent/internal/agent/ipc"
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

	// 心跳与全量
	heartbeatDue := time.Now()
	fullDue := time.Now().Add(10 * time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var ipcSrv *ipc.Server
	if runtime.GOOS == "windows" {
		ipcSrv = ipc.NewServer(cfg, col, spool, u)
		go func() {
			if err := ipcSrv.Serve(ctx); err != nil {
				log.Printf("ipc serve error: %v", err)
			}
		}()
		log.Printf("IPC server listening on pipe")
	}

	log.Printf("core-agent %s started: device=%s hb=%ds full=%ds", agentVersion, cfg.DeviceID, cfg.HeartbeatIntervalSec, cfg.FullIntervalSec)

	for {
		select {
		case <-sig:
			log.Printf("shutdown requested, flushing spool...")
			return
		case <-ipcSrv.QuitChan():
			log.Printf("IPC quit requested")
			return
		default:
		}

		now := time.Now()

		if now.After(heartbeatDue) {
			heartbeatDue = now.Add(time.Duration(cfg.HeartbeatIntervalSec) * time.Second)
			enqueue(spool, cfg, col, protocol.ReportTypeHeartbeat)
		}
		if now.After(fullDue) {
			fullDue = now.Add(time.Duration(cfg.FullIntervalSec) * time.Second)
			enqueue(spool, cfg, col, protocol.ReportTypeFull)
		}
		// 扫描 spool，发送队列
		flush := false
		spoolItems, _ := spool.Pending()
		if len(spoolItems) > 0 {
			for _, item := range spoolItems {
				raw, err := spool.Read(item)
				if err != nil {
					spool.Delete(item)
					continue
				}
				var env protocol.Envelope
				if err := json.Unmarshal(raw, &env); err != nil {
					spool.Delete(item)
					continue
				}
				resp, err := u.Upload(env)
				if err != nil {
					log.Printf("upload failed: %v", err)
					d := backoff.Next()
					time.Sleep(d)
					break
				}
				spool.Delete(item)
				flush = true
				if cfg.ApplyServerOverride(resp.NextHeartbeatSec, resp.NextFullSec) {
					log.Printf("interval override: hb=%ds full=%ds", cfg.HeartbeatIntervalSec, cfg.FullIntervalSec)
				}
			}
			if !flush {
				log.Printf("offline, sleeping")
			}
		}
		u.MaybeProbePrimary(now)
		time.Sleep(time.Duration(cfg.SpoolScanSec) * time.Second)
	}
}

func enqueue(spool *reporter.Spool, cfg config.Config, col *collector.Collector, reportType string) {
	hb := col.Heartbeat()
	var payload []byte
	var err error

	if reportType == protocol.ReportTypeFull {
		payload, _ = json.Marshal(col.Full())
	} else {
		payload, err = json.Marshal(hb)
	}
	if err != nil {
		log.Printf("marshal payload: %v", err)
		return
	}

	env := protocol.Envelope{
		DeviceID:    cfg.DeviceID,
		AgentVersion: cfg.AgentVersion,
		ReportType:  reportType,
		ReportedAt:  time.Now(),
		Payload:     payload,
	}
	raw, _ := json.Marshal(env)
	if _, err := spool.Enqueue(raw); err != nil {
		log.Printf("spool error: %v", err)
	}
	log.Printf("spool enqueued %s report", reportType)
}
