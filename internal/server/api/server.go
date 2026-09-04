package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"itagent/internal/server/alert"
	"itagent/internal/server/store"
	"itagent/internal/shared/protocol"
)

type Config struct {
	InstallToken        string
	AdminToken          string
	DefaultHeartbeatSec int
	DefaultFullSec      int
}

type Handler struct {
	store store.Store
	cfg   Config
	mux   *http.ServeMux
}

func NewHandler(s store.Store, cfg Config) *Handler {
	h := &Handler{store: s, cfg: cfg, mux: http.NewServeMux()}
	h.mux.HandleFunc("POST /api/v1/register", h.handleRegister)
	h.mux.HandleFunc("POST /api/v1/ingest", h.handleIngest)
	h.mux.HandleFunc("GET /api/v1/agent/config", h.handleAgentConfig)
	h.mux.Handle("GET /api/v1/devices", h.admin(h.handleListDevices))
	h.mux.Handle("GET /api/v1/devices/{id}", h.admin(h.handleGetDevice))
	h.mux.Handle("GET /api/v1/devices/{id}/history", h.admin(h.handleDeviceHistory))
	h.mux.Handle("GET /api/v1/changes", h.admin(h.handleListChanges))
	h.mux.Handle("POST /api/v1/changes/{id}/ack", h.admin(h.handleAckChange))
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func bearerToken(r *http.Request) string {
	const prefix = "Bearer "
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(auth, prefix))
}

func (h *Handler) admin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.cfg.AdminToken == "" || bearerToken(r) != h.cfg.AdminToken {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"code": 401, "message": "admin token required"})
			return
		}
		next(w, r)
	}
}

func (h *Handler) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req protocol.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, protocol.RegisterResponse{Code: 400, Message: "invalid json"})
		return
	}
	if req.DeviceID == "" {
		writeJSON(w, http.StatusBadRequest, protocol.RegisterResponse{Code: 400, Message: "device_id required"})
		return
	}
	if h.cfg.InstallToken == "" || req.InstallToken != h.cfg.InstallToken {
		writeJSON(w, http.StatusForbidden, protocol.RegisterResponse{Code: 403, Message: "invalid install_token"})
		return
	}
	token, err := h.store.RegisterDevice(r.Context(), store.Device{
		DeviceID: req.DeviceID, Hostname: req.Hostname, OS: req.OS, AgentVersion: req.AgentVersion,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, protocol.RegisterResponse{Code: 500, Message: "register failed"})
		return
	}
	writeJSON(w, http.StatusOK, protocol.RegisterResponse{Code: 0, Message: "ok", DeviceToken: token})
}

func (h *Handler) handleIngest(w http.ResponseWriter, r *http.Request) {
	var env protocol.Envelope
	if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"code": 400, "message": "invalid json"})
		return
	}
	if err := h.store.Authenticate(r.Context(), env.DeviceID, bearerToken(r)); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"code": 401, "message": "invalid device token"})
		return
	}
	if err := env.Validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"code": 400, "message": err.Error()})
		return
	}

	if len(env.Payload) > 0 {
		var hb protocol.HeartbeatPayload
		if err := json.Unmarshal(env.Payload, &hb); err == nil {
			_ = h.store.UpsertDeviceSeen(r.Context(), store.Device{
				DeviceID: env.DeviceID, Hostname: hb.Hostname,
				OS: hb.OS.Name, AgentVersion: env.AgentVersion,
			})
		}
	}
	if env.ReportType == protocol.ReportTypeFull {
		h.processFullReport(r, env)
	}
	if _, err := h.store.SaveReport(r.Context(), store.Report{
		DeviceID: env.DeviceID, ReportType: env.ReportType,
		Payload: env.Payload, ReportedAt: env.ReportedAt,
	}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"code": 500, "message": "save failed"})
		return
	}
	writeJSON(w, http.StatusOK, protocol.IngestResponse{
		Code:             0,
		Message:          "ok",
		ServerTime:       time.Now().UTC().Format(time.RFC3339),
		NextHeartbeatSec: h.cfg.DefaultHeartbeatSec,
		NextFullSec:      h.cfg.DefaultFullSec,
	})
}

func (h *Handler) handleAgentConfig(w http.ResponseWriter, r *http.Request) {
	deviceID := r.URL.Query().Get("device_id")
	if err := h.store.Authenticate(r.Context(), deviceID, bearerToken(r)); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"code": 401, "message": "invalid device token"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"code":                   0,
		"heartbeat_interval_sec": h.cfg.DefaultHeartbeatSec,
		"full_interval_sec":      h.cfg.DefaultFullSec,
	})
}

func (h *Handler) handleListDevices(w http.ResponseWriter, r *http.Request) {
	limit, offset := paging(r)
	devices, err := h.store.ListDevices(r.Context(), limit, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"code": 500, "message": "query failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"code": 0, "devices": devices})
}

func (h *Handler) handleGetDevice(w http.ResponseWriter, r *http.Request) {
	d, err := h.store.GetDevice(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]any{"code": 404, "message": "device not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"code": 500, "message": "query failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"code": 0, "device": d})
}

func (h *Handler) handleDeviceHistory(w http.ResponseWriter, r *http.Request) {
	limit, offset := paging(r)
	reports, err := h.store.ListReports(r.Context(), r.PathValue("id"), limit, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"code": 500, "message": "query failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"code": 0, "reports": reports})
}

func (h *Handler) processFullReport(r *http.Request, env protocol.Envelope) {
	ctx := r.Context()
	var full protocol.FullPayload
	if err := json.Unmarshal(env.Payload, &full); err != nil {
		return
	}
	prevSnap, err := h.store.GetSnapshot(ctx, env.DeviceID)
	if err == nil {
		var prev protocol.FullPayload
		if json.Unmarshal(prevSnap.Payload, &prev) == nil {
			h.saveEvents(ctx, env.DeviceID, alert.DiffHardware(env.DeviceID, prev.Hardware, full.Hardware))
		}
	}
	streaks := h.recentTempStreaks(ctx, env.DeviceID, full.Hardware.SMART)
	h.saveEvents(ctx, env.DeviceID, alert.CheckSMARTWithStreak(env.DeviceID, full.Hardware.SMART, streaks))

	snapPayload, _ := json.Marshal(full)
	_ = h.store.SaveSnapshot(ctx, store.Snapshot{DeviceID: env.DeviceID, Payload: snapPayload})
}

func (h *Handler) saveEvents(ctx context.Context, deviceID string, events []alert.Event) {
	for _, e := range events {
		_, _ = h.store.SaveChangeEvent(ctx, store.ChangeEvent{
			DeviceID: deviceID, Kind: e.Kind, Severity: e.Severity,
			Message: e.Message, Detail: e.Detail,
		})
	}
}

func (h *Handler) recentTempStreaks(ctx context.Context, deviceID string, smart []protocol.SmartHealth) map[string]int {
	const threshold = 60
	streaks := map[string]int{}
	for _, s := range smart {
		if s.TemperatureC >= threshold {
			streaks[s.DiskSerial] = 1
		}
	}
	if len(streaks) == 0 {
		return nil
	}
	reports, err := h.store.ListReports(ctx, deviceID, 5, 0)
	if err != nil {
		return streaks
	}
	for _, rep := range reports {
		if rep.ReportType != protocol.ReportTypeFull {
			continue
		}
		var prev protocol.FullPayload
		if json.Unmarshal(rep.Payload, &prev) != nil {
			continue
		}
		prevTemp := map[string]int{}
		for _, s := range prev.Hardware.SMART {
			prevTemp[s.DiskSerial] = s.TemperatureC
		}
		for serial := range streaks {
			if prevTemp[serial] >= threshold {
				streaks[serial]++
			}
		}
	}
	return streaks
}

func (h *Handler) handleListChanges(w http.ResponseWriter, r *http.Request) {
	limit, offset := paging(r)
	includeAcked := r.URL.Query().Get("all") == "true"
	events, err := h.store.ListChangeEvents(r.Context(), includeAcked, limit, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"code": 500, "message": "query failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"code": 0, "events": events})
}

func (h *Handler) handleAckChange(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"code": 400, "message": "invalid id"})
		return
	}
	if err := h.store.AckChangeEvent(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]any{"code": 404, "message": "event not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"code": 500, "message": "ack failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"code": 0, "message": "acked"})
}

func paging(r *http.Request) (limit, offset int) {
	limit = 100
	offset = 0
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 1000 {
		limit = v
	}
	if v, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && v >= 0 {
		offset = v
	}
	return
}
