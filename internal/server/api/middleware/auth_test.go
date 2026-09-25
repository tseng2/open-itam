package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// 任务 B（JWT secret 配置化）：密钥注入与错密拒绝的单测。
// JWTSecret 是包级 var，生产由 main 启动时注入一次；测试改动后必须
// 恢复现值，避免污染同包其他用例

func restoreJWTSecret(t *testing.T) {
	t.Helper()
	prev := JWTSecret
	t.Cleanup(func() { JWTSecret = prev })
}

func TestGenerateAndParseTokenRoundTrip(t *testing.T) {
	restoreJWTSecret(t)
	token, err := GenerateToken(7, "alice", "admin")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	claims, err := ParseToken(token)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if claims.UserID != 7 || claims.Username != "alice" || claims.Role != "admin" {
		t.Fatalf("claims mismatch: %+v", claims)
	}
}

func TestSetJWTSecretRotatesSigningKey(t *testing.T) {
	restoreJWTSecret(t)

	// 默认密钥签发的存量 token
	legacy, err := GenerateToken(1, "u1", "user")
	if err != nil {
		t.Fatalf("generate legacy: %v", err)
	}

	SetJWTSecret("rotated-secret-456")

	// secret 变更后存量 token 全失效（401 → 前端跳登录），属预期行为
	if _, err := ParseToken(legacy); err == nil {
		t.Fatal("token signed with old secret must be rejected after rotation")
	}

	// 新密钥签发的 token 正常解析
	fresh, err := GenerateToken(2, "u2", "admin")
	if err != nil {
		t.Fatalf("generate fresh: %v", err)
	}
	claims, err := ParseToken(fresh)
	if err != nil {
		t.Fatalf("parse fresh: %v", err)
	}
	if claims.UserID != 2 {
		t.Fatalf("fresh claims: %+v", claims)
	}
}

func TestSetJWTSecretEmptyKeepsCurrent(t *testing.T) {
	restoreJWTSecret(t)
	before := string(JWTSecret)
	SetJWTSecret("")
	if string(JWTSecret) != before {
		t.Fatal("empty secret must be ignored (keep current value)")
	}
}

func TestAuthMiddlewareRejectsForeignSecretToken(t *testing.T) {
	restoreJWTSecret(t)
	gin.SetMode(gin.TestMode)

	// 模拟「他处签发/密钥漂移」：结构合法但签名密钥不对的 token
	claims := Claims{
		UserID: 1, Username: "intruder", Role: "admin",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			Issuer:    "itagent",
		},
	}
	foreign, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("some-other-secret"))
	if err != nil {
		t.Fatalf("sign foreign: %v", err)
	}

	r := gin.New()
	var gotUserID any
	r.GET("/p", AuthMiddleware(), func(c *gin.Context) {
		gotUserID, _ = c.Get("userID")
		c.Status(http.StatusOK)
	})

	// 错密 token → 401 拒绝（不注入 claims）
	req := httptest.NewRequest(http.MethodGet, "/p", nil)
	req.Header.Set("Authorization", "Bearer "+foreign)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("foreign-secret token must 401, got %d", rec.Code)
	}

	// 当前密钥 token → 200 且注入 userID
	ok, err := GenerateToken(1, "alice", "admin")
	if err != nil {
		t.Fatalf("generate ok token: %v", err)
	}
	req = httptest.NewRequest(http.MethodGet, "/p", nil)
	req.Header.Set("Authorization", "Bearer "+ok)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid token must pass, got %d body=%s", rec.Code, rec.Body.String())
	}
	id, _ := gotUserID.(int64)
	if id != 1 {
		t.Fatalf("claims injection: got %v", gotUserID)
	}
}
