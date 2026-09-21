package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"itagent/internal/agent/collector"
	"itagent/internal/agent/config"
	"itagent/internal/agent/identity"
	"itagent/internal/agent/ipc"
	"itagent/internal/agent/reporter"
	"itagent/internal/agent/updater"
	"itagent/internal/shared/protocol"
)

const agentVersion = "0.2.3"

var (
	cfgPathFlag = flag.String("config", "configs/agent.json", "path to agent config")
	serviceFlag = flag.Bool("service", false, "run as windows service")
)

// 防重复触发：一次进程生命周期内只执行一次自更新
var updateTriggered bool

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

// resolveDeviceID 确定本机终端指纹，三级来源：配置文件 → 注册表/备份文件 →
// 按硬件特征包现场计算。计算出新指纹后双写持久化，配置文件单点丢失不再导致身份漂移
func resolveDeviceID(cfgPath string, cfg *config.Config, bundle identity.Bundle) bool {
	if cfg.DeviceID != "" {
		identity.PersistDeviceID(cfg.DeviceID) // 老配置补写第二份持久化
		return true
	}
	if recovered := identity.LoadPersistedDeviceID(); recovered != "" {
		cfg.DeviceID = recovered
		if err := config.Save(cfgPath, *cfg); err != nil {
			log.Printf("save recovered device id: %v", err)
		}
		log.Printf("device id recovered from registry backup: %s", recovered)
		return true
	}
	if !bundle.Usable() {
		log.Printf("device id: no usable hardware identifier")
		return false
	}
	cfg.DeviceID = bundle.Fingerprint()
	if err := config.Save(cfgPath, *cfg); err != nil {
		log.Printf("save device id: %v", err)
	}
	identity.PersistDeviceID(cfg.DeviceID)
	return true
}

// installDir 推导安装目录：服务模式下工作目录是 System32，不能依赖相对路径
func installDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(filepath.Dir(exe)) // bin/core-agent.exe → 上级即安装目录
}

func innerMain(cfgPath string, done <-chan struct{}) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Printf("config: %v", err)
		return
	}
	cfg.AgentVersion = agentVersion

	col := collector.New()
	// 特征包采集一次即可：注册时上报用于服务端身份调和
	bundle := identity.CollectBundle(identity.PlatformProbe())
	if !resolveDeviceID(cfgPath, &cfg, bundle) {
		return
	}

	u := reporter.NewUploader(cfg.ServerPrimary, cfg.ServerBackup, cfg.DeviceID, cfg.DeviceToken, nil)
	if cfg.DeviceToken == "" {
		// 注册不能只试一次：服务端不可达或 token 未同步时，每 60s 重试直到成功，
		// 否则 agent 会以无 token 状态空转到下次进程重启
		go func() {
			hb := col.Heartbeat()
			for {
				tok, adoptID, err := u.Register(cfg.InstallToken, hb.Hostname, hb.OS.Name, cfg.AgentVersion, bundle)
				if err == nil {
					cfg.DeviceToken = tok
					if adoptID != "" && adoptID != cfg.DeviceID {
						// 服务端识别出本机是既有终端，收养旧指纹保持资产绑定连续
						log.Printf("adopted existing device id: %s -> %s", cfg.DeviceID, adoptID)
						cfg.DeviceID = adoptID
						identity.PersistDeviceID(adoptID)
						u = reporter.NewUploader(cfg.ServerPrimary, cfg.ServerBackup, cfg.DeviceID, cfg.DeviceToken, nil)
					}
					config.Save(cfgPath, cfg)
					log.Printf("registered, device_token saved")
					return
				}
				log.Printf("register failed, retry in 60s: %v", err)
				time.Sleep(60 * time.Second)
			}
		}()
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
			u := reporter.NewUploader(cfg.ServerPrimary, cfg.ServerBackup, cfg.DeviceID, cfg.DeviceToken, nil)
			if resp, err := u.Upload(m); err == nil && resp != nil {
				spool.Delete(item)
				// 服务端下发新版本时静默自更新：下载校验完成后退出，
				// 由脱离进程的替换脚本完成换包并拉起服务
				if resp.Update != nil && !updateTriggered {
					updateTriggered = true
					if err := updater.Apply(u, resp.Update, installDir()); err != nil {
						log.Printf("self update failed: %v", err)
						updateTriggered = false
						continue
					}
					os.Exit(0)
				}
			}
		}
	}
}
