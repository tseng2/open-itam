package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSignBodyKnownVector(t *testing.T) {
	// RFC 4231 风格的固定向量断言：签名实现必须与标准 HMAC-SHA256 完全一致
	body := []byte(`{"hello":"itam"}`)
	mac := hmac.New(sha256.New, []byte("secret"))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))

	if got := SignBody("secret", body); got != want {
		t.Fatalf("signature mismatch: got %s want %s", got, want)
	}
	// 不同 secret 必须产生不同签名
	if SignBody("other", body) == want {
		t.Fatal("different secret must produce different signature")
	}
}

func TestPostSignsWhenSecretSet(t *testing.T) {
	var (
		gotBody   []byte
		gotSig    string
		gotHasSig bool
	)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotSig, gotHasSig = r.Header.Get(SignatureHeader), true
		w.WriteHeader(http.StatusOK)
	}))
	defer receiver.Close()

	payload := TestPayload(time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC))
	if err := Post(context.Background(), nil, receiver.URL, "s3cr3t", payload); err != nil {
		t.Fatalf("post: %v", err)
	}
	if !gotHasSig || gotSig == "" {
		t.Fatal("secret set but signature header missing")
	}
	if want := SignBody("s3cr3t", gotBody); gotSig != want {
		t.Fatalf("signature header mismatch: got %s want %s", gotSig, want)
	}
	var back Payload
	if err := json.Unmarshal(gotBody, &back); err != nil || back.Event != EventTypeTest {
		t.Fatalf("receiver got unexpected body: %s", gotBody)
	}
}

func TestPostNoSignatureWithoutSecret(t *testing.T) {
	var gotSigHeader http.Header
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSigHeader = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer receiver.Close()

	payload := TestPayload(time.Now())
	if err := Post(context.Background(), nil, receiver.URL, "", payload); err != nil {
		t.Fatalf("post: %v", err)
	}
	if gotSigHeader.Get(SignatureHeader) != "" {
		t.Fatal("empty secret must not carry signature header")
	}
	if gotSigHeader.Get("Content-Type") != "application/json" {
		t.Fatalf("content-type must be json, got %q", gotSigHeader.Get("Content-Type"))
	}
}

func TestPostRejectsNon2xx(t *testing.T) {
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer receiver.Close()

	payload := TestPayload(time.Now())
	if err := Post(context.Background(), nil, receiver.URL, "", payload); err == nil {
		t.Fatal("receiver 4xx/5xx must be an error so engine can retry next round")
	}
}

func TestPostHasClientTimeout(t *testing.T) {
	// DefaultHTTPClient 必须带超时：HTTP 出站严禁无限等待挂死扫描协程
	if DefaultHTTPClient.Timeout <= 0 {
		t.Fatalf("default client must set timeout, got %v", DefaultHTTPClient.Timeout)
	}
}
