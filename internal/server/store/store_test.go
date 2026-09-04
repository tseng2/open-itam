package store

import (
	"context"
	"testing"
	"time"
)

func testStore(t *testing.T) Store {
	t.Helper()
	s, err := OpenSQLite(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestRegisterAndAuthenticate(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	token, err := s.RegisterDevice(ctx, Device{
		DeviceID: "dev001", Hostname: "PC-001", OS: "Windows 11", AgentVersion: "1.0.0",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	if err := s.Authenticate(ctx, "dev001", token); err != nil {
		t.Fatalf("authenticate with valid token: %v", err)
	}
	if err := s.Authenticate(ctx, "dev001", "wrong-token"); err != ErrUnauthorized {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
	if err := s.Authenticate(ctx, "no-such-device", token); err != ErrUnauthorized {
		t.Fatalf("expected ErrUnauthorized for unknown device, got %v", err)
	}
}

func TestRegisterDuplicateReturnsSameToken(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	t1, err := s.RegisterDevice(ctx, Device{DeviceID: "dev001"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	t2, err := s.RegisterDevice(ctx, Device{DeviceID: "dev001"})
	if err != nil {
		t.Fatalf("re-register should be idempotent: %v", err)
	}
	if t1 != t2 {
		t.Fatal("re-register should return the same token")
	}
}

func TestUpsertDeviceSeen(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	if _, err := s.RegisterDevice(ctx, Device{DeviceID: "dev001", Hostname: "PC-001"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	err := s.UpsertDeviceSeen(ctx, Device{DeviceID: "dev001", Hostname: "PC-002", AgentVersion: "1.0.1"})
	if err != nil {
		t.Fatalf("upsert seen: %v", err)
	}

	d, err := s.GetDevice(ctx, "dev001")
	if err != nil {
		t.Fatalf("get device: %v", err)
	}
	if d.Hostname != "PC-002" {
		t.Fatalf("hostname not updated, got %q", d.Hostname)
	}
	if d.AgentVersion != "1.0.1" {
		t.Fatalf("agent version not updated, got %q", d.AgentVersion)
	}
	if time.Since(d.LastSeenAt) > time.Minute {
		t.Fatal("last_seen_at not refreshed")
	}
}

func TestGetDeviceNotFound(t *testing.T) {
	s := testStore(t)
	_, err := s.GetDevice(context.Background(), "ghost")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestSaveAndListReports(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	if _, err := s.RegisterDevice(ctx, Device{DeviceID: "dev001"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	for i := 0; i < 3; i++ {
		_, err := s.SaveReport(ctx, Report{
			DeviceID:   "dev001",
			ReportType: "heartbeat",
			Payload:    []byte(`{"n":1}`),
			ReportedAt: time.Now(),
		})
		if err != nil {
			t.Fatalf("save report %d: %v", i, err)
		}
	}

	reports, err := s.ListReports(ctx, "dev001", 10, 0)
	if err != nil {
		t.Fatalf("list reports: %v", err)
	}
	if len(reports) != 3 {
		t.Fatalf("expected 3 reports, got %d", len(reports))
	}
	if reports[0].ReceivedAt.IsZero() {
		t.Fatal("received_at should be set by server")
	}

	reports, err = s.ListReports(ctx, "dev001", 2, 0)
	if err != nil {
		t.Fatalf("list reports paged: %v", err)
	}
	if len(reports) != 2 {
		t.Fatalf("expected 2 reports with limit=2, got %d", len(reports))
	}
}

func TestRegisterRequiresDeviceID(t *testing.T) {
	s := testStore(t)
	if _, err := s.RegisterDevice(context.Background(), Device{}); err == nil {
		t.Fatal("expected error for empty device_id")
	}
}

func TestUpsertSeenUnknownDevice(t *testing.T) {
	s := testStore(t)
	err := s.UpsertDeviceSeen(context.Background(), Device{DeviceID: "ghost"})
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestListDevicesPaging(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	for _, id := range []string{"a", "b", "c"} {
		if _, err := s.RegisterDevice(ctx, Device{DeviceID: id}); err != nil {
			t.Fatalf("register: %v", err)
		}
	}
	devices, err := s.ListDevices(ctx, 2, 2)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("expected 1 device at offset 2, got %d", len(devices))
	}
}

func TestListDevices(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	for _, id := range []string{"a", "b", "c"} {
		if _, err := s.RegisterDevice(ctx, Device{DeviceID: id, Hostname: id}); err != nil {
			t.Fatalf("register %s: %v", id, err)
		}
	}
	devices, err := s.ListDevices(ctx, 10, 0)
	if err != nil {
		t.Fatalf("list devices: %v", err)
	}
	if len(devices) != 3 {
		t.Fatalf("expected 3 devices, got %d", len(devices))
	}
	if devices[0].DeviceToken != "" {
		t.Fatal("ListDevices must not leak device tokens")
	}
}
