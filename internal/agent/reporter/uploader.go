package reporter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

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
		client = &http.Client{Timeout: 15 * time.Second}
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

func (u *Uploader) Register(installToken, hostname, osName, version string) (string, error) {
	req := protocol.RegisterRequest{
		DeviceID: u.deviceID, Hostname: hostname, OS: osName,
		AgentVersion: version, InstallToken: installToken,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return "", err
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
			return out.DeviceToken, nil
		}
	}
	return "", fmt.Errorf("register failed on all endpoints")
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
