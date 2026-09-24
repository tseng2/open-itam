package v1

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"itagent/internal/server/api/middleware"
	"itagent/internal/server/model"
	"itagent/internal/server/store"
	"itagent/internal/server/webhook"
)

func setupWebhookRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := store.InitDB("sqlite", t.TempDir()+"/webhook_api_test.db")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})

	r := gin.New()
	apiV1 := r.Group("/api/v1")
	protected := apiV1.Group("/")
	protected.Use(middleware.AuthMiddleware())
	RegisterWebhookAlertRoutes(protected)
	return r
}

func decodeWebhookConfig(t *testing.T, rec *httptest.ResponseRecorder) gin.H {
	t.Helper()
	var resp dispatchAPIResp
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.Code != 0 {
		t.Fatalf("resp decode: err=%v code=%d body=%s", err, resp.Code, rec.Body.String())
	}
	var cfg gin.H
	if err := json.Unmarshal(resp.Data, &cfg); err != nil {
		t.Fatalf("decode config data: %v", err)
	}
	return cfg
}

func TestWebhookConfigGetDefaults(t *testing.T) {
	r := setupWebhookRouter(t)

	rec := doDispatchJSON(t, r, http.MethodGet, "/api/v1/webhook-alerts/config", adminToken(t), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get config status=%d body=%s", rec.Code, rec.Body.String())
	}
	cfg := decodeWebhookConfig(t, rec)
	if cfg["enabled"] != false || cfg["secret_set"] != false {
		t.Fatalf("default config must be disabled: %+v", cfg)
	}
	// secret 永不回传，只回传 secret_set 布尔
	if _, leaked := cfg["secret"]; leaked {
		t.Fatal("secret must never be returned to frontend")
	}
	if cfg["cooldown_minutes"].(float64) != float64(model.DefaultWebhookCooldownMinutes) {
		t.Fatalf("default cooldown mismatch: %+v", cfg)
	}
}

func TestWebhookConfigUpdateAndSecretKeep(t *testing.T) {
	r := setupWebhookRouter(t)
	token := adminToken(t)

	// 启用 + 配置地址与 secret
	rec := doDispatchJSON(t, r, http.MethodPut, "/api/v1/webhook-alerts/config", token, gin.H{
		"enabled":          true,
		"webhook_url":      "https://hooks.example.com/itam",
		"secret":           "signing-key",
		"cooldown_minutes": 30,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update config status=%d body=%s", rec.Code, rec.Body.String())
	}
	cfg := decodeWebhookConfig(t, rec)
	if cfg["enabled"] != true || cfg["secret_set"] != true || cfg["webhook_url"] != "https://hooks.example.com/itam" {
		t.Fatalf("updated config mismatch: %+v", cfg)
	}

	// 只改开关：secret 留空必须保留既有值（与 protection 密码语义一致）
	rec = doDispatchJSON(t, r, http.MethodPut, "/api/v1/webhook-alerts/config", token, gin.H{
		"enabled": false,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("toggle config status=%d body=%s", rec.Code, rec.Body.String())
	}
	cfg = decodeWebhookConfig(t, rec)
	if cfg["enabled"] != false || cfg["secret_set"] != true {
		t.Fatalf("secret must survive empty update: %+v", cfg)
	}

	// 落库校验：DB 中 secret 确实保留
	raw, err := store.NewGormStore(store.DB).GetWebhookAlertConfig(context.Background())
	if err != nil || raw.Secret != "signing-key" || raw.CooldownMinutes != 30 {
		t.Fatalf("persisted config mismatch: %+v err=%v", raw, err)
	}
}

func TestWebhookConfigValidation(t *testing.T) {
	r := setupWebhookRouter(t)
	token := adminToken(t)

	cases := []struct {
		name string
		body gin.H
	}{
		{"enable without url", gin.H{"enabled": true}},
		{"invalid url scheme", gin.H{"enabled": true, "webhook_url": "ftp://example.com/hook"}},
		{"cooldown below 1 minute", gin.H{"webhook_url": "https://a.com/h", "cooldown_minutes": 0}},
	}
	for _, tc := range cases {
		rec := doDispatchJSON(t, r, http.MethodPut, "/api/v1/webhook-alerts/config", token, tc.body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: expected 400, got %d body=%s", tc.name, rec.Code, rec.Body.String())
		}
	}
}

func TestWebhookTestPush(t *testing.T) {
	r := setupWebhookRouter(t)
	token := adminToken(t)

	// 未配置地址 → 400
	rec := doDispatchJSON(t, r, http.MethodPost, "/api/v1/webhook-alerts/test", token, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("test without config must 400, got %d body=%s", rec.Code, rec.Body.String())
	}

	var (
		gotBody []byte
		gotSig  string
	)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotBody, _ = io.ReadAll(req.Body)
		gotSig = req.Header.Get(webhook.SignatureHeader)
		w.WriteHeader(http.StatusOK)
	}))
	defer receiver.Close()

	// 配置后（即便未启用也可测连通性）：推送 itam.test 事件且带签名
	if err := store.NewGormStore(store.DB).PutWebhookAlertConfig(context.Background(), model.WebhookAlertConfig{
		Enabled: false, WebhookURL: receiver.URL, Secret: "k", CooldownMinutes: 60,
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	rec = doDispatchJSON(t, r, http.MethodPost, "/api/v1/webhook-alerts/test", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("test push status=%d body=%s", rec.Code, rec.Body.String())
	}

	if gotSig == "" {
		t.Fatal("test push must carry signature when secret configured")
	}
	if want := webhook.SignBody("k", gotBody); gotSig != want {
		t.Fatalf("signature mismatch: got %s want %s", gotSig, want)
	}
	var payload struct {
		Event string `json:"event"`
	}
	if err := json.Unmarshal(gotBody, &payload); err != nil || payload.Event != webhook.EventTypeTest {
		t.Fatalf("receiver got unexpected body: %s", gotBody)
	}
}

func TestWebhookTestPushFailure(t *testing.T) {
	r := setupWebhookRouter(t)
	token := adminToken(t)

	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer receiver.Close()

	if err := store.NewGormStore(store.DB).PutWebhookAlertConfig(context.Background(), model.WebhookAlertConfig{
		Enabled: true, WebhookURL: receiver.URL, CooldownMinutes: 60,
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	rec := doDispatchJSON(t, r, http.MethodPost, "/api/v1/webhook-alerts/test", token, nil)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("receiver failure must 502, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestWebhookRoutesRequireAdminRole(t *testing.T) {
	r := setupWebhookRouter(t)
	token, err := middleware.GenerateToken(2, "ordinary", "user")
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/webhook-alerts/config"},
		{http.MethodPut, "/api/v1/webhook-alerts/config"},
		{http.MethodPost, "/api/v1/webhook-alerts/test"},
	}
	for _, tc := range cases {
		rec := doDispatchJSON(t, r, tc.method, tc.path, token, nil)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("non-admin must get 403 on %s %s, got %d", tc.method, tc.path, rec.Code)
		}
	}
}
