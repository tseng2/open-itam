package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"itagent/internal/server/model"
	"itagent/internal/server/store"
	"itagent/internal/shared/protocol"
)

// TestMain 为 api 包统一初始化全局 GORM DB：syncToAssetLedger 依赖 store.DB，
// 此前该函数在 store.DB == nil 时整体跳过、从未被测试覆盖。
// 初始化后老栈测试的 full 上报会真实走台账同步路径，写 GORM 侧表
// （assets/asset_events/agent_devices），不影响各测试基于自身 Store 的断言
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "api-gorm-*")
	if err != nil {
		panic(err)
	}
	db, err := store.InitDB("sqlite", filepath.Join(dir, "gorm.db"))
	if err != nil {
		panic(err)
	}
	code := m.Run()
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func setupE2E(t *testing.T) http.Handler {
	t.Helper()
	return NewHandler(store.NewGormStore(store.DB), Config{
		InstallToken: "install-secret",
		AdminToken:   "admin-secret",
	})
}

func postFullFor(t *testing.T, h http.Handler, token, deviceID, hostname string, hw protocol.Hardware) {
	t.Helper()
	full := protocol.FullPayload{
		HeartbeatPayload: protocol.HeartbeatPayload{Hostname: hostname, OS: protocol.OSInfo{Name: "Windows 11"}},
		Hardware:         hw,
	}
	p, _ := json.Marshal(full)
	env, _ := json.Marshal(protocol.Envelope{
		DeviceID: deviceID, ReportType: protocol.ReportTypeFull,
		ReportedAt: time.Now(), Payload: p,
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ingest", bytes.NewReader(env))
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("full ingest failed: %d %s", rec.Code, rec.Body.String())
	}
}

func assetIDOfDevice(t *testing.T, deviceID string) int64 {
	t.Helper()
	var dev model.Device
	if err := store.DB.Where("device_id = ?", deviceID).First(&dev).Error; err != nil {
		t.Fatalf("device %s not bound to ledger: %v", deviceID, err)
	}
	if dev.AssetID == nil || *dev.AssetID == 0 {
		t.Fatalf("device %s has no asset binding", deviceID)
	}
	return *dev.AssetID
}

func countPendingHardwareEvents(t *testing.T, assetID int64) int {
	t.Helper()
	var events []model.AssetEvent
	store.DB.Where("asset_id = ? AND event_type = 'hardware_change' AND review_status = 20", assetID).Find(&events)
	return len(events)
}

func e2eHardware() protocol.Hardware {
	return protocol.Hardware{
		Brand: "Dell", Model: "Latitude 7440", Serial: "SN-E2E",
		MemoryTotalMB: 16 * 1024,
		CPU:  []protocol.CPU{{Model: "Intel i5-1240P", Cores: 12, Threads: 16}},
		Disks: []protocol.Disk{
			{Serial: "D1", Model: "Samsung SSD", SizeGB: 512, Type: "SSD"},
			{Serial: "D2", Model: "WD HDD", SizeGB: 1024, Type: "HDD"},
		},
	}
}

// 端到端：full 上报建基线 → 换盘（数量容量不变仅 SN 变）→ 自动生成待审核
// hardware_change 事件；重复上报同硬件保持单条（幂等）；再变更也不重复（审核前单条语义）
func TestIngestHardwareDiffGeneratesPendingEvent(t *testing.T) {
	h := setupE2E(t)
	token := register(t, h, "dev-hw")

	hw1 := e2eHardware()
	postFullFor(t, h, token, "dev-hw", "PC-HW", hw1)
	assetID := assetIDOfDevice(t, "dev-hw")
	if n := countPendingHardwareEvents(t, assetID); n != 0 {
		t.Fatalf("baseline ingest must not raise events, got %d", n)
	}

	// 换盘：数量 2->2、容量不变、仅序列号变化（旧实现检测不到的篡改场景）
	hw2 := e2eHardware()
	hw2.Disks[0].Serial = "D1-REPLACED"
	postFullFor(t, h, token, "dev-hw", "PC-HW", hw2)

	if n := countPendingHardwareEvents(t, assetID); n != 1 {
		t.Fatalf("expected exactly 1 pending hardware_change event, got %d", n)
	}
	var event model.AssetEvent
	store.DB.Where("asset_id = ? AND event_type = 'hardware_change'", assetID).First(&event)
	if !strings.Contains(event.Description, "磁盘更换") || !strings.Contains(event.Description, "D1-REPLACED") {
		t.Fatalf("event description must name swapped serial, got: %s", event.Description)
	}

	// 幂等：同硬件重复上报不重复生成
	postFullFor(t, h, token, "dev-hw", "PC-HW", hw2)
	if n := countPendingHardwareEvents(t, assetID); n != 1 {
		t.Fatalf("duplicate ingest must stay idempotent, got %d", n)
	}

	// 待审期间硬件再变：保持单条待审（审核通过更新基线后才会再次检测）
	hw3 := e2eHardware()
	hw3.Disks[0].Serial = "D1-REPLACED"
	hw3.CPU[0].Model = "Intel i7-1360P"
	postFullFor(t, h, token, "dev-hw", "PC-HW", hw3)
	if n := countPendingHardwareEvents(t, assetID); n != 1 {
		t.Fatalf("pending event must remain single before review, got %d", n)
	}
}

// U 盘插拔回归：可移动介质不参与比对，不得产生 hardware_change 事件
func TestIngestRemovableDiskNotReported(t *testing.T) {
	h := setupE2E(t)
	token := register(t, h, "dev-usb")

	// 独立序列号：台账按 SN 归属资产，与上一个测试共用 SN 会串台到同一资产
	hw1 := e2eHardware()
	hw1.Serial = "SN-E2E-USB"
	postFullFor(t, h, token, "dev-usb", "PC-USB", hw1)
	assetID := assetIDOfDevice(t, "dev-usb")

	hwUSB := e2eHardware()
	hwUSB.Serial = "SN-E2E-USB"
	hwUSB.Disks = append(hwUSB.Disks, protocol.Disk{Serial: "USB-001", Model: "Kingston", SizeGB: 64, Removable: true})
	postFullFor(t, h, token, "dev-usb", "PC-USB", hwUSB)

	if n := countPendingHardwareEvents(t, assetID); n != 0 {
		t.Fatalf("removable disk plug must not raise hardware_change, got %d", n)
	}
}
