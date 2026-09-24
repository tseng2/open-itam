package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// SignatureHeader 签名头名：值为 HMAC-SHA256(secret, body) 的 hex 编码，
// 接收方用共享 secret 对原始请求体重算即可校验来源与完整性
const SignatureHeader = "X-ITAM-Signature"

// DefaultTimeout Webhook 出站 HTTP 超时：扫描协程严禁被慢接收端无限挂死
const DefaultTimeout = 10 * time.Second

// DefaultHTTPClient 引擎与手动测试共用的出站客户端（带超时）
var DefaultHTTPClient = &http.Client{Timeout: DefaultTimeout}

// SignBody 计算 HMAC-SHA256 签名（hex 小写）
func SignBody(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// Post 推送 JSON 载荷到接收端：secret 非空时带 HMAC-SHA256 签名头；
// 非 2xx 视为失败（供引擎下一轮重试）；client 为 nil 时用带超时的默认客户端
func Post(ctx context.Context, client *http.Client, url, secret string, payload Payload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal webhook payload: %w", err)
	}
	if client == nil {
		client = DefaultHTTPClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if secret != "" {
		req.Header.Set(SignatureHeader, SignBody(secret, body))
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("post webhook: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook receiver returned status %d", resp.StatusCode)
	}
	return nil
}
