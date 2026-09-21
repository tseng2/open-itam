package reporter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"itagent/internal/agent/identity"
	"itagent/internal/shared/protocol"
)

const failoverThreshold = 3

type Uploader struct {
	primary string
	backup  string

	deviceID string
	token    string

	client        *http.Client
	usingBackup   bool
	primaryFails  int
	lastProbeTime time.Time
	probeInterval time.Duration
}

func NewUploader(primary, backup, deviceID, token string, client *http.Client) *Uploader {
	if client == nil {
		client = &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				Proxy: nil, // 局域网/企业内网请求直连，避免被系统 HTTP 代理劫持
			},
		}
	}
	return &Uploader{
		primary:       primary,
		backup:        backup,
		deviceID:      deviceID,
		token:         token,
		client:        client,
		probeInterval: 30 * time.Minute,
	}
}

func (u *Uploader) UsingBackup() bool { return u.usingBackup }

func (u *Uploader) post(base, path string, body []byte) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPost, base+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if u.token != "" {
		req.Header.Set("Authorization", "Bearer "+u.token)
	}
	return u.client.Do(req)
}

func (u *Uploader) Upload(env protocol.Envelope) (*protocol.IngestResponse, error) {
	body, err := json.Marshal(env)
	if err != nil {
		return nil, err
	}

	for attempt := 0; attempt < failoverThreshold; attempt++ {
		base := u.primary
		if u.usingBackup && u.backup != "" {
			base = u.backup
		}
		resp, err := u.post(base, "/api/v1/ingest", body)
		if err == nil && resp.StatusCode == http.StatusOK {
			var out protocol.IngestResponse
			json.NewDecoder(resp.Body).Decode(&out)
			resp.Body.Close()
			u.primaryFails = 0
			return &out, nil
		}
		if resp != nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
		if !u.usingBackup {
			u.primaryFails++
			if u.backup != "" && u.primaryFails >= failoverThreshold {
				u.usingBackup = true
				u.lastProbeTime = time.Now()
			}
		} else {
			return nil, fmt.Errorf("backup ingest failed: %v", err)
		}
	}
	return nil, fmt.Errorf("ingest failed after %d attempts", failoverThreshold)
}

// Register 用安装令牌换取设备令牌。携带身份特征包，服务端若识别出本机是
// 既有终端（重装系统后指纹重算的场景），会返回待收养的旧指纹
func (u *Uploader) Register(installToken, hostname, osName, version string, bundle identity.Bundle) (token, adoptID string, err error) {
	req := protocol.RegisterRequest{
		DeviceID: u.deviceID, Hostname: hostname, OS: osName,
		AgentVersion: version, InstallToken: installToken,
		BiosUUID: bundle.BIOSUUID, BoardSerial: bundle.BoardSerial, MACs: bundle.MACs,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return "", "", err
	}
	for _, base := range u.endpoints() {
		resp, err := u.post(base, "/api/v1/register", body)
		if err != nil {
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			io.Copy(io.Discard, resp.Body)
			continue
		}
		var out protocol.RegisterResponse
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			continue
		}
		if out.DeviceToken != "" {
			u.token = out.DeviceToken
			return out.DeviceToken, out.AdoptDeviceID, nil
		}
	}
	return "", "", fmt.Errorf("register failed on all endpoints")
}

// DownloadTo 下载服务端文件到本地路径（用于 Agent 自更新包），带设备令牌鉴权
func (u *Uploader) DownloadTo(path, destPath string) error {
	var lastErr error
	for _, base := range u.endpoints() {
		req, err := http.NewRequest(http.MethodGet, base+path, nil)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("Authorization", "Bearer "+u.token)
		resp, err := u.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			lastErr = fmt.Errorf("download %s: http %d", path, resp.StatusCode)
			continue
		}
		f, err := os.Create(destPath)
		if err != nil {
			resp.Body.Close()
			return err
		}
		_, copyErr := io.Copy(f, resp.Body)
		resp.Body.Close()
		f.Close()
		if copyErr != nil {
			os.Remove(destPath)
			lastErr = copyErr
			continue
		}
		return nil
	}
	return lastErr
}

func (u *Uploader) endpoints() []string {
	if u.usingBackup && u.backup != "" {
		return []string{u.backup, u.primary}
	}
	if u.backup != "" {
		return []string{u.primary, u.backup}
	}
	return []string{u.primary}
}

func (u *Uploader) TryPrimary() bool {
	if u.backup == "" {
		return true
	}
	req, err := http.NewRequest(http.MethodGet, u.primary+"/api/v1/agent/config?device_id="+u.deviceID, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+u.token)
	resp, err := u.client.Do(req)
	if err != nil {
		return false
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		u.usingBackup = false
		u.primaryFails = 0
		return true
	}
	return false
}

func (u *Uploader) MaybeProbePrimary(now time.Time) {
	if !u.usingBackup {
		return
	}
	if now.Sub(u.lastProbeTime) < u.probeInterval {
		return
	}
	u.lastProbeTime = now
	u.TryPrimary()
}

func (u *Uploader) SetProbeInterval(d time.Duration) {
	u.probeInterval = d
}

func (u *Uploader) Token() string { return u.token }
