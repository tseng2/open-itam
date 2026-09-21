package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"itagent/internal/shared/protocol"
)

// agentUpdateManifest 描述 data/updates/manifest.json 的内容，
// 运维只需把新 core-agent.exe 与清单放进该目录即可灰度下发
type agentUpdateManifest struct {
	Version string `json:"version"`
	File    string `json:"file"`   // 安装包文件名（相对清单所在目录）
	SHA256  string `json:"sha256"` // 十六进制摘要，Agent 下载后强校验
	Notes   string `json:"notes"`
}

// loadUpdateManifest 每次请求重新读清单：文件小、更新频率低，
// 换来的是替换文件即生效，不用重启服务
func (h *Handler) loadUpdateManifest() (*agentUpdateManifest, string, error) {
	if h.cfg.UpdateManifest == "" {
		return nil, "", nil
	}
	data, err := os.ReadFile(h.cfg.UpdateManifest)
	if err != nil {
		return nil, "", err
	}
	var m agentUpdateManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, "", err
	}
	if m.Version == "" || m.File == "" {
		return nil, "", nil
	}
	return &m, filepath.Join(filepath.Dir(h.cfg.UpdateManifest), m.File), nil
}

// newerVersion 语义化版本比较：a 比 b 新时返回 true
func newerVersion(a, b string) bool {
	pa := strings.Split(strings.TrimPrefix(a, "v"), ".")
	pb := strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := 0; i < 3; i++ {
		na, nb := 0, 0
		if i < len(pa) {
			na, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			nb, _ = strconv.Atoi(pb[i])
		}
		if na != nb {
			return na > nb
		}
	}
	return false
}

// updateInfoFor 返回该 Agent 版本可用的更新；已是最新或无清单时返回 nil
func (h *Handler) updateInfoFor(currentVersion string) *protocol.UpdateInfo {
	m, filePath, err := h.loadUpdateManifest()
	if err != nil || m == nil {
		return nil
	}
	if !newerVersion(m.Version, currentVersion) {
		return nil
	}
	info := &protocol.UpdateInfo{
		Version: m.Version,
		SHA256:  m.SHA256,
		Notes:   m.Notes,
	}
	if st, err := os.Stat(filePath); err == nil {
		info.Size = st.Size()
	}
	// 清单未配置 sha256 时现场计算并容忍（开发期便利）；生产建议在清单里写死
	if info.SHA256 == "" {
		if sum, err := fileSHA256(filePath); err == nil {
			info.SHA256 = sum
		}
	}
	return info
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (h *Handler) handleAgentUpdate(w http.ResponseWriter, r *http.Request) {
	deviceID := r.URL.Query().Get("device_id")
	if err := h.store.Authenticate(r.Context(), deviceID, bearerToken(r)); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"code": 401, "message": "invalid device token"})
		return
	}
	info := h.updateInfoFor(r.URL.Query().Get("current"))
	if info == nil {
		writeJSON(w, http.StatusOK, map[string]any{"code": 0, "update": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"code": 0, "update": info})
}

func (h *Handler) handleAgentUpdateDownload(w http.ResponseWriter, r *http.Request) {
	deviceID := r.URL.Query().Get("device_id")
	if err := h.store.Authenticate(r.Context(), deviceID, bearerToken(r)); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"code": 401, "message": "invalid device token"})
		return
	}
	m, filePath, err := h.loadUpdateManifest()
	if err != nil || m == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"code": 404, "message": "no update package"})
		return
	}
	f, err := os.Open(filePath)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"code": 404, "message": "package file missing"})
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename="+m.File)
	io.Copy(w, f)
}
