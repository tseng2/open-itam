package main

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"itagent/internal/agent/collector"
	"itagent/internal/agent/config"
	"itagent/internal/agent/reporter"
	"itagent/internal/shared/protocol"
)

func main() {
	cfg, err := config.Load("C:\\ProgramData\\ITAgent\\configs\\agent.json")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	col := collector.New()
	uploader := reporter.NewUploader(cfg.ServerPrimary, cfg.ServerBackup, cfg.DeviceID, cfg.DeviceToken, nil)

	fmt.Printf("[1] Triggering Heartbeat for %s (%s) to %s...\n", cfg.DeviceID, col.Heartbeat().Hostname, cfg.ServerPrimary)
	hb := col.Heartbeat()
	hbPayload, _ := json.Marshal(hb)
	envHb := protocol.Envelope{
		DeviceID:     cfg.DeviceID,
		AgentVersion: "0.1.0",
		ReportType:   protocol.ReportTypeHeartbeat,
		ReportedAt:   time.Now(),
		Payload:      hbPayload,
	}
	resp, err := uploader.Upload(envHb)
	if err != nil {
		fmt.Printf("Heartbeat upload failed: %v\n", err)
	} else {
		fmt.Printf("Heartbeat upload SUCCESS: Code=%d Message=%s\n", resp.Code, resp.Message)
	}

	fmt.Printf("[2] Triggering Full Hardware & Software Inventory for %s...\n", cfg.DeviceID)
	full := col.Full()
	fullPayload, _ := json.Marshal(full)
	envFull := protocol.Envelope{
		DeviceID:     cfg.DeviceID,
		AgentVersion: "0.1.0",
		ReportType:   protocol.ReportTypeFull,
		ReportedAt:   time.Now(),
		Payload:      fullPayload,
	}
	respFull, err := uploader.Upload(envFull)
	if err != nil {
		fmt.Printf("Full inventory upload failed: %v\n", err)
	} else {
		fmt.Printf("Full inventory upload SUCCESS: Code=%d Message=%s\n", respFull.Code, respFull.Message)
	}
}
