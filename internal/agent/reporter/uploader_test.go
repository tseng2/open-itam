package reporter

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"itagent/internal/agent/identity"
	"itagent/internal/shared/protocol"
)

type fakeServer struct {
	*httptest.Server
	failPrimary atomic.Bool
	sawToken    atomic.Value
	sawBodies   atomic.Int32
}

func newFakeServer(t *testing.T) *fakeServer {
	fs := &fakeServer{}
	fs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fs.failPrimary.Load() && r.Header.Get("X-Test-Role") == "primary" {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		if r.URL.Path == "/api/v1/ingest" {
			fs.sawToken.Store(r.Header.Get("Authorization"))
			fs.sawBodies.Add(1)
			resp := protocol.IngestResponse{
				Code:             0,
				Message:          "ok",
				ServerTime:       time.Now().UTC().Format(time.RFC3339),
				NextHeartbeatSec: 111,
				NextFullSec:      2222,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(fs.Server.Close)
	return fs
}

func TestUploadSuccessReturnsIntervals(t *testing.T) {
	fs := newFakeServer(t)
	u := NewUploader(fs.URL, "", "dev-1", "tok-1", nil)

	resp, err := u.Upload(protocol.Envelope{DeviceID: "dev-1", ReportType: protocol.ReportTypeHeartbeat, ReportedAt: time.Now()})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if resp.NextHeartbeatSec != 111 || resp.NextFullSec != 2222 {
		t.Fatalf("unexpected intervals: %+v", resp)
	}
	if auth, _ := fs.sawToken.Load().(string); auth != "Bearer tok-1" {
		t.Fatalf("auth header wrong: %q", auth)
	}
}

func TestFailoverToBackup(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer primary.Close()
	backup := newFakeServer(t)

	u := NewUploader(primary.URL, backup.URL, "dev-1", "tok-1", nil)
	var resp *protocol.IngestResponse
	var err error
	for i := 0; i < 3; i++ {
		resp, err = u.Upload(protocol.Envelope{DeviceID: "dev-1", ReportType: protocol.ReportTypeHeartbeat, ReportedAt: time.Now()})
	}
	if err != nil {
		t.Fatalf("after failover should succeed on backup: %v", err)
	}
	if resp == nil || resp.Code != 0 {
		t.Fatalf("backup response wrong: %+v", resp)
	}
	if !u.UsingBackup() {
		t.Fatal("uploader should mark using-backup after 3 primary failures")
	}
}

func TestStickyBackupPersists(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer primary.Close()
	backup := newFakeServer(t)

	u := NewUploader(primary.URL, backup.URL, "dev-1", "tok-1", nil)
	for i := 0; i < 3; i++ {
		u.Upload(protocol.Envelope{DeviceID: "dev-1", ReportType: protocol.ReportTypeHeartbeat, ReportedAt: time.Now()})
	}
	if backup.sawBodies.Load() == 0 {
		t.Fatal("backup never received traffic")
	}
	before := backup.sawBodies.Load()
	u.Upload(protocol.Envelope{DeviceID: "dev-1", ReportType: protocol.ReportTypeHeartbeat, ReportedAt: time.Now()})
	if backup.sawBodies.Load() != before+1 {
		t.Fatal("after failover, uploads must stick to backup")
	}
}

func TestRegisterFlow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/register" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(protocol.RegisterResponse{Code: 0, Message: "ok", DeviceToken: "dev-tok-xyz"})
	}))
	defer srv.Close()

	u := NewUploader(srv.URL, "", "dev-9", "", nil)
	token, _, err := u.Register("install-secret", "PC-9", "Windows 11", "0.1.0", identity.Bundle{})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if token != "dev-tok-xyz" || u.Token() != "dev-tok-xyz" {
		t.Fatalf("token not stored: %q", token)
	}
}

func TestRegisterAllEndpointsFail(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bad.Close()
	u := NewUploader(bad.URL, "http://127.0.0.1:1", "dev-9", "", nil)
	if _, _, err := u.Register("x", "h", "os", "v", identity.Bundle{}); err == nil {
		t.Fatal("expected error when all endpoints fail")
	}
	if u.Token() != "" {
		t.Fatal("token must not be set on failed register")
	}
}

func TestMaybeProbePrimaryTiming(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"code": 0})
	}))
	defer primary.Close()
	backup := newFakeServer(t)

	u := NewUploader(primary.URL, backup.URL, "dev-1", "tok", nil)
	u.usingBackup = true
	u.lastProbeTime = time.Now()
	u.SetProbeInterval(time.Minute)

	u.MaybeProbePrimary(time.Now())
	if !u.UsingBackup() {
		t.Fatal("probe interval not reached, must not switch back yet")
	}
	u.lastProbeTime = time.Now().Add(-2 * time.Minute)
	u.MaybeProbePrimary(time.Now())
	if u.UsingBackup() {
		t.Fatal("must switch back after successful probe")
	}

	u2 := NewUploader(primary.URL, "", "dev-1", "tok", nil)
	if !u2.TryPrimary() {
		t.Fatal("with no backup configured, TryPrimary must return true")
	}
}

func TestProbePrimaryRecovery(t *testing.T) {
	var primaryOK atomic.Bool
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !primaryOK.Load() {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(protocol.IngestResponse{Code: 0})
	}))
	defer primary.Close()

	backup := newFakeServer(t)
	u := NewUploader(primary.URL, backup.URL, "dev-1", "tok-1", nil)
	for i := 0; i < 3; i++ {
		u.Upload(protocol.Envelope{DeviceID: "dev-1", ReportType: protocol.ReportTypeHeartbeat, ReportedAt: time.Now()})
	}
	if !u.UsingBackup() {
		t.Fatal("should be on backup")
	}

	primaryOK.Store(true)
	if !u.TryPrimary() {
		t.Fatal("probe should succeed after primary recovers")
	}
	if u.UsingBackup() {
		t.Fatal("should switch back to primary")
	}
}
