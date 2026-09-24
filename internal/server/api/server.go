package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"itagent/internal/server/alert"
	"itagent/internal/server/api/middleware"
	"itagent/internal/server/model"
	"itagent/internal/server/store"
	"itagent/internal/shared/hwfilter"
	"itagent/internal/shared/protocol"
)

type Config struct {
	InstallToken        string
	AdminToken          string
	DefaultHeartbeatSec int
	DefaultFullSec      int
	// A2 失联语义分层：资产联系状态的离线判定阈值（秒），0 = 默认 15 分钟
	OfflineThresholdSec int
	// Agent 安装包更新清单文件路径（JSON：version/file/sha256/notes），
	// 为空时不下发更新
	UpdateManifest string
}

type Handler struct {
	store store.Store
	cfg   Config
	mux   *http.ServeMux
}

// defaultOfflineThreshold A2 失联判定的默认离线阈值：15 分钟容忍一次心跳丢失
const defaultOfflineThreshold = 15 * time.Minute

// ResolveOfflineThreshold 把 server.json 的 offline_threshold_sec 归一为时长：
// 0/负值取默认 15 分钟。main（A4 Webhook 引擎注入）与 NewHandler 共用，
// 默认值只此一处，禁止各自硬编码
func ResolveOfflineThreshold(sec int) time.Duration {
	if sec <= 0 {
		return defaultOfflineThreshold
	}
	return time.Duration(sec) * time.Second
}

func NewHandler(s store.Store, cfg Config) *Handler {
	h := &Handler{store: s, cfg: cfg, mux: http.NewServeMux()}
	h.mux.HandleFunc("POST /api/v1/register", h.handleRegister)
	h.mux.HandleFunc("POST /api/v1/ingest", h.handleIngest)
	h.mux.HandleFunc("GET /api/v1/agent/config", h.handleAgentConfig)
	h.mux.HandleFunc("GET /api/v1/agent/update", h.handleAgentUpdate)
	h.mux.HandleFunc("GET /api/v1/agent/update/download", h.handleAgentUpdateDownload)
	h.mux.HandleFunc("POST /api/v1/agent/uninstall-code/verify", h.handleUninstallCodeVerify)
	h.mux.Handle("GET /api/v1/devices", h.admin(h.handleListDevices))
	h.mux.Handle("GET /api/v1/devices/{id}", h.admin(h.handleGetDevice))
	h.mux.Handle("GET /api/v1/devices/{id}/history", h.admin(h.handleDeviceHistory))
	h.mux.Handle("GET /api/v1/changes", h.admin(h.handleListChanges))
	h.mux.Handle("POST /api/v1/changes/{id}/ack", h.admin(h.handleAckChange))

	// 挂载全新 ITAM 资产管理与集团公司路由
	// A2 失联语义分层：资产列表联系状态的离线判定阈值，未配置时默认 15 分钟
	// （容忍一次心跳丢失），源头是 server.json 的 offline_threshold_sec
	ginEngine := SetupRouter(ResolveOfflineThreshold(cfg.OfflineThresholdSec))
	h.mux.Handle("/api/v1/auth", ginEngine)
	h.mux.Handle("/api/v1/auth/", ginEngine)
	h.mux.Handle("/api/v1/assets", ginEngine)
	h.mux.Handle("/api/v1/assets/", ginEngine)
	h.mux.Handle("/api/v1/companies", ginEngine)
	h.mux.Handle("/api/v1/companies/", ginEngine)
	h.mux.Handle("/api/v1/users", ginEngine)
	h.mux.Handle("/api/v1/users/", ginEngine)
	h.mux.Handle("/api/v1/storage-lendings", ginEngine)
	h.mux.Handle("/api/v1/storage-lendings/", ginEngine)
	h.mux.Handle("/api/v1/part-records", ginEngine)
	h.mux.Handle("/api/v1/part-records/", ginEngine)
	h.mux.Handle("/api/v1/agent/heartbeat", ginEngine)
	// 防护模块与卸载验证码管理（JWT + RoleMiddleware，高危安全面）
	h.mux.Handle("/api/v1/protection", ginEngine)
	h.mux.Handle("/api/v1/protection/", ginEngine)
	// 外派登记（阶段五 A1，JWT + RoleMiddleware("admin")）
	h.mux.Handle("/api/v1/dispatches", ginEngine)
	h.mux.Handle("/api/v1/dispatches/", ginEngine)
	// Webhook 告警配置（阶段五 A4，JWT + RoleMiddleware("admin")）
	// 易踩坑：不补这两行，外层 mux 直接 404 且构建测试全绿（A1 的教训）
	h.mux.Handle("/api/v1/webhook-alerts", ginEngine)
	h.mux.Handle("/api/v1/webhook-alerts/", ginEngine)
	// 盘点任务（阶段五 P0-β，JWT + RoleMiddleware("admin")）；
	// 免登录移动扫码面走 /api/public（一次性盘点令牌 + 限流），
	// 两行都不可漏：外层 mux 不补则部署后 404 且构建测试全绿（A1 的教训）
	h.mux.Handle("/api/v1/stocktakes", ginEngine)
	h.mux.Handle("/api/v1/stocktakes/", ginEngine)
	h.mux.Handle("/api/public", ginEngine)
	h.mux.Handle("/api/public/", ginEngine)
	// 设备申请（阶段五 P0-β，登录用户提交/撤回 + admin 审批同事务绑定领用人）
	h.mux.Handle("/api/v1/asset-requests", ginEngine)
	h.mux.Handle("/api/v1/asset-requests/", ginEngine)
	// 折旧规则引擎（阶段五 P0-β，读面登录可访问 + 写面 admin；销账/恢复挂 /api/v1/assets 已有前缀）
	h.mux.Handle("/api/v1/depreciations", ginEngine)
	h.mux.Handle("/api/v1/depreciations/", ginEngine)
	// 维度治理（P1）：厂商/供应商/位置库/型号库，读面登录可访问 + 写面 admin
	h.mux.Handle("/api/v1/manufacturers", ginEngine)
	h.mux.Handle("/api/v1/manufacturers/", ginEngine)
	h.mux.Handle("/api/v1/suppliers", ginEngine)
	h.mux.Handle("/api/v1/suppliers/", ginEngine)
	h.mux.Handle("/api/v1/locations", ginEngine)
	h.mux.Handle("/api/v1/locations/", ginEngine)
	h.mux.Handle("/api/v1/asset-models", ginEngine)
	h.mux.Handle("/api/v1/asset-models/", ginEngine)

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
		token := bearerToken(r)
		// 1. 兼容原配置的静态 AdminToken
		if h.cfg.AdminToken != "" && token == h.cfg.AdminToken {
			next(w, r)
			return
		}
		// 2. 兼容登录后颁发的 JWT Token (管理员或超管身份)
		if token != "" {
			claims, err := middleware.ParseToken(token)
			if err == nil && (claims.Role == "super_admin" || claims.Role == "admin") {
				next(w, r)
				return
			}
		}
		writeJSON(w, http.StatusUnauthorized, map[string]any{"code": 401, "message": "admin token or login required"})
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

	// 身份调和：新指纹携带特征包注册时，若与失联老终端高度重合，
	// 要求 Agent 收养老指纹，保持资产绑定与历史数据连续
	adoptID := ""
	if _, err := h.store.GetDevice(r.Context(), req.DeviceID); errors.Is(err, store.ErrNotFound) {
		adoptID = h.reconcileIdentity(req)
	}
	targetID := req.DeviceID
	if adoptID != "" {
		targetID = adoptID
	}

	token, err := h.store.RegisterDevice(r.Context(), store.Device{
		DeviceID: targetID, Hostname: req.Hostname, OS: req.OS, AgentVersion: req.AgentVersion,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, protocol.RegisterResponse{Code: 500, Message: "register failed"})
		return
	}
	writeJSON(w, http.StatusOK, protocol.RegisterResponse{Code: 0, Message: "ok", DeviceToken: token, AdoptDeviceID: adoptID})
}

// reconcileIdentity 用身份特征包在终端画像表中寻找本机的既有记录。
// 仅在指纹未见过时调用；命中即返回应收养的旧指纹，未命中返回空串
func (h *Handler) reconcileIdentity(req protocol.RegisterRequest) string {
	uuid := strings.ToUpper(strings.TrimSpace(req.BiosUUID))
	hostname := strings.ToLower(strings.TrimSpace(req.Hostname))
	boardSerial := strings.TrimSpace(req.BoardSerial)
	reqMACs := map[string]bool{}
	for _, m := range req.MACs {
		reqMACs[strings.ToUpper(strings.TrimSpace(m))] = true
	}
	if uuid == "" && len(reqMACs) == 0 && boardSerial == "" {
		return ""
	}

	var devices []model.Device
	if err := store.DB.Find(&devices).Error; err != nil {
		return ""
	}
	for _, d := range devices {
		// 最强锚点：BIOS UUID 相同（同一台物理机基本不可能 UUID 相同）
		if uuid != "" && !hwfilter.IsGarbageUUID(uuid) && strings.ToUpper(d.BIOSUUID) == uuid {
			return d.DeviceID
		}
		// 次强：主机名相同 + MAC 有交集（特征包 MAC 集合，或老数据的主 MAC 列）
		if hostname != "" && strings.ToLower(d.Hostname) == hostname {
			var oldMACs []string
			if json.Unmarshal([]byte(d.MACSet), &oldMACs) == nil {
				for _, m := range oldMACs {
					if reqMACs[m] {
						return d.DeviceID
					}
				}
			}
			// 兼容老版本 Agent 的画像行：没有 mac_set，只有主网卡 MAC
			if d.MacAddress != "" && reqMACs[strings.ToUpper(d.MacAddress)] {
				return d.DeviceID
			}
			// 再次：主机名相同 + 主板序列号有效且一致
			if boardSerial != "" && !hwfilter.IsGarbageSerial(boardSerial) && d.BoardSerial == boardSerial {
				return d.DeviceID
			}
		}
	}
	return ""
}

// macSetJSON 把采集到的物理网卡 MAC 列表整理为排序去重后的 JSON 数组，
// 作为身份特征包的一部分落库
func macSetJSON(nics []protocol.NIC) string {
	set := map[string]bool{}
	for _, n := range nics {
		m := strings.ToUpper(strings.TrimSpace(n.MAC))
		if m != "" {
			set[m] = true
		}
	}
	macs := make([]string, 0, len(set))
	for m := range set {
		macs = append(macs, m)
	}
	sort.Strings(macs)
	raw, _ := json.Marshal(macs)
	return string(raw)
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
		Update:           h.updateInfoFor(env.AgentVersion),
	})
}

func (h *Handler) handleAgentConfig(w http.ResponseWriter, r *http.Request) {
	deviceID := r.URL.Query().Get("device_id")
	if err := h.store.Authenticate(r.Context(), deviceID, bearerToken(r)); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"code": 401, "message": "invalid device token"})
		return
	}
	quit, err := h.store.GetProtectionModule(r.Context(), model.ProtectionModuleQuit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"code": 500, "message": "query protection failed"})
		return
	}
	uninstall, err := h.store.GetProtectionModule(r.Context(), model.ProtectionModuleUninstall)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"code": 500, "message": "query protection failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"code":                   0,
		"heartbeat_interval_sec": h.cfg.DefaultHeartbeatSec,
		"full_interval_sec":      h.cfg.DefaultFullSec,
		"quit_protection":        map[string]any{"enabled": quit.Enabled, "password_hash": quit.PasswordHash},
		"uninstall_protection":   map[string]any{"enabled": uninstall.Enabled, "password_hash": uninstall.PasswordHash},
	})
}

// handleUninstallCodeVerify 卸载验证码在线校验（device token 鉴权，QAX 同款模型）：
// 校验单次使用、10 分钟过期与设备绑定，通过即标记已用
func (h *Handler) handleUninstallCodeVerify(w http.ResponseWriter, r *http.Request) {
	var req protocol.UninstallCodeVerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Code == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"code": 400, "message": "invalid request"})
		return
	}
	deviceID := r.URL.Query().Get("device_id")
	if err := h.store.Authenticate(r.Context(), deviceID, bearerToken(r)); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"code": 401, "message": "invalid device token"})
		return
	}
	if err := h.store.VerifyUninstallCode(r.Context(), deviceID, req.Code); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"code": 401, "message": "invalid or expired uninstall code"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"code": 0, "message": "verified"})
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

	// 核心：将 Agent 采集上报的终端硬件画像与实物资产台账自动关联
	h.syncToAssetLedger(env.DeviceID, full)
}

func (h *Handler) syncToAssetLedger(deviceID string, full protocol.FullPayload) {
	if store.DB == nil {
		return
	}

	// 1. 确保默认集团公司存在
	var company model.Company
	if err := store.DB.First(&company).Error; err != nil {
		company = model.Company{
			Name: "默认集团公司",
			Code: "DEFAULT",
		}
		store.DB.Create(&company)
	}

	// 2. 如果上报了 AD 登录域账号，自动建立或关联用户档案
	var userID *int64
	if full.Logon.User != "" {
		var user model.User
		err := store.DB.Where("username = ?", full.Logon.User).First(&user).Error
		if err != nil {
			user = model.User{
				CompanyID: company.ID,
				Username:  full.Logon.User,
				RealName:  full.Logon.User,
				Status:    "active",
			}
			store.DB.Create(&user)
		}
		userID = &user.ID
	}

	// 3. 自动同步或创建该终端对应的实物资产台账
	// 提取主物理网卡 IP 与 MAC
	primaryIP := ""
	primaryMAC := ""
	for _, nic := range full.Hardware.NICs {
		if primaryMAC == "" && nic.MAC != "" {
			primaryMAC = nic.MAC
		}
		for _, ip := range nic.IPs {
			cleanIP := strings.Split(ip, "/")[0]
			if cleanIP != "" && cleanIP != "127.0.0.1" && !strings.HasPrefix(cleanIP, "169.254.") {
				primaryIP = cleanIP
				if nic.MAC != "" {
					primaryMAC = nic.MAC
				}
				break
			}
		}
		if primaryIP != "" {
			break
		}
	}
	if primaryIP == "" && len(full.Network.Interfaces) > 0 {
		for _, iface := range full.Network.Interfaces {
			for _, ip := range iface.IPs {
				cleanIP := strings.Split(ip, "/")[0]
				if cleanIP != "" && cleanIP != "127.0.0.1" && !strings.HasPrefix(cleanIP, "169.254.") {
					primaryIP = cleanIP
					if primaryMAC == "" {
						primaryMAC = iface.MAC
					}
					break
				}
			}
			if primaryIP != "" {
				break
			}
		}
	}

	sn := strings.TrimSpace(full.Hardware.Serial)
	if sn == "" || sn == "Default string" {
		sn = strings.TrimSpace(full.Hardware.BIOSSerial)
	}
	if sn == "" || sn == "Default string" {
		sn = deviceID
	}

	var asset model.Asset
	// 先检查当前设备是否已经绑定过实物资产
	var existingDev model.Device
	if err := store.DB.Where("device_id = ?", deviceID).First(&existingDev).Error; err == nil && existingDev.AssetID != nil && *existingDev.AssetID > 0 {
		_ = store.DB.First(&asset, *existingDev.AssetID).Error
	}

	// 若未通过设备关联找到，则通过出厂硬件序列号查找已有资产
	if asset.ID == 0 {
		if sn != deviceID {
			_ = store.DB.Where("serial_number = ? AND serial_number != '' AND serial_number != 'Default string'", sn).First(&asset).Error
		}
	}

	if asset.ID == 0 {
		// 生成规范临时资产编码，拼接指纹短码防止重名冲突
		suffix := deviceID
		if len(suffix) > 6 {
			suffix = suffix[:6]
		}
		assetTag := "待编-" + full.Hostname
		var conflict model.Asset
		if err := store.DB.Where("asset_tag = ?", assetTag).First(&conflict).Error; err == nil {
			assetTag = "待编-" + full.Hostname + "-" + suffix
		}

		brand := full.Hardware.Brand
		if brand == "" {
			brand = "OEM"
		}
		modelName := full.Hardware.Model
		if modelName == "" {
			modelName = "PC工作站"
		}
		asset = model.Asset{
			CompanyID:    company.ID,
			CategoryID:   1, // 默认为电脑整机
			AssetTag:     assetTag,
			Status:       20, // 自动置为使用中
			Brand:        brand,
			ModelName:    modelName,
			SerialNumber: sn,
			UserID:       userID,
			Remark:       "计算机名称: " + full.Hostname + " (由 Agent 自动采集关联，待补充固定资产编码)",
		}
		// 新资产直接以采集值初始化账面规格，后续以人工维护为准
		applyAssetBookSpecs(&asset, &full.Hardware, primaryMAC)
		if err := store.DB.Create(&asset).Error; err == nil {
			event := model.AssetEvent{
				AssetID:     asset.ID,
				EventType:   "auto_discover",
				Title:       "Agent 自动发现并关联固定资产",
				Description: "终端设备 " + full.Hostname + " 自动上报入库",
				OperatorID:  userID,
			}
			store.DB.Create(&event)
		}
	} else {
		// 已存在资产，更新当前使用人与状态
		dirty := false
		if userID != nil && (asset.UserID == nil || *asset.UserID != *userID) {
			asset.UserID = userID
			asset.Status = 20
			dirty = true
		}
		// 账面规格为空时以采集值回填；人工已维护的字段保持不动
		if applyAssetBookSpecs(&asset, &full.Hardware, primaryMAC) {
			dirty = true
		}
		if dirty {
			store.DB.Save(&asset)
		}
	}

	// 4. 同步更新 agent_devices 关联表
	var dev model.Device
	errDev := store.DB.Where("device_id = ?", deviceID).First(&dev).Error
	now := time.Now()
	var assetIDPtr *int64
	if asset.ID > 0 {
		assetIDPtr = &asset.ID
	}

	if errDev != nil {
		// 资产可能已有 Excel 导入的占位终端行（ledger_*）或本机旧指纹的终端行：
		// agent_devices.asset_id 有唯一索引，新建会冲突，必须原地升级为当前真实终端
		var bound model.Device
		if assetIDPtr != nil {
			_ = store.DB.Where("asset_id = ?", *assetIDPtr).First(&bound).Error
		}
		if bound.ID > 0 {
			dev = bound
		} else {
			dev = model.Device{AssetID: assetIDPtr}
		}
		dev.DeviceID = deviceID
		dev.Hostname = full.Hostname
		dev.OSName = full.OS.Name
		dev.IPAddress = primaryIP
		dev.PublicIP = full.Network.PublicIP
		dev.MacAddress = primaryMAC
		dev.BIOSUUID = strings.ToUpper(strings.TrimSpace(full.Hardware.UUID))
		dev.BoardSerial = strings.TrimSpace(full.Hardware.BoardSerial)
		dev.MACSet = macSetJSON(full.Hardware.NICs)
		if len(full.Hardware.CPU) > 0 {
			dev.CPUModel = full.Hardware.CPU[0].Model
		}
		dev.MemoryTotalGB = float64(full.Hardware.MemoryTotalMB) / 1024.0
		dev.LastSeenAt = now
		if bound.ID > 0 {
			store.DB.Save(&dev)
		} else if err := store.DB.Create(&dev).Error; err != nil {
			log.Printf("syncToAssetLedger: create device row failed for %s: %v", deviceID, err)
		}
	} else {
		if assetIDPtr != nil {
			dev.AssetID = assetIDPtr
		}
		dev.Hostname = full.Hostname
		dev.OSName = full.OS.Name
		if primaryIP != "" {
			dev.IPAddress = primaryIP
		}
		if full.Network.PublicIP != "" {
			dev.PublicIP = full.Network.PublicIP
		}
		if primaryMAC != "" {
			dev.MacAddress = primaryMAC
		}
		dev.BIOSUUID = strings.ToUpper(strings.TrimSpace(full.Hardware.UUID))
		dev.BoardSerial = strings.TrimSpace(full.Hardware.BoardSerial)
		dev.MACSet = macSetJSON(full.Hardware.NICs)
		if len(full.Hardware.CPU) > 0 {
			dev.CPUModel = full.Hardware.CPU[0].Model
		}
		dev.MemoryTotalGB = float64(full.Hardware.MemoryTotalMB) / 1024.0
		dev.LastSeenAt = now
		store.DB.Save(&dev)
	}

	// 5. 硬件基线与版本控制
	if asset.ID > 0 {
		var currentVersion model.AssetVersion
		errVer := store.DB.Where("asset_id = ? AND version = ?", asset.ID, asset.CurrentVersion).First(&currentVersion).Error
		
		hwBytes, _ := json.Marshal(full.Hardware)
		hwSnapshot := string(hwBytes)
		
		if errVer != nil {
			// 如果没有基线（通常是第一次同步），初始化基线
			currentVersion = model.AssetVersion{
				AssetID:          asset.ID,
				Version:          1,
				HardwareSnapshot: hwSnapshot,
				ChangeReason:     "初始化硬件基线",
			}
			store.DB.Create(&currentVersion)
			
			if asset.CurrentVersion == 0 {
				asset.CurrentVersion = 1
				store.DB.Save(&asset)
			}
		} else {
			// 对比基线（A3：比对逻辑提炼为 compareHardware 纯函数，
			// 覆盖内存/内置磁盘数量/磁盘序列号/CPU 数量与型号）
			var baseHw protocol.Hardware
			if json.Unmarshal([]byte(currentVersion.HardwareSnapshot), &baseHw) == nil {
				if diff := compareHardware(baseHw, full.Hardware); len(diff) > 0 {
					// 检查是否已有待审核的事件（避免重复提交待审）：
					// 管理员驳回或审核通过前，同一资产的硬件变更事件保持单条
					var pending model.AssetEvent
					errPending := store.DB.Where("asset_id = ? AND event_type = 'hardware_change' AND review_status = 20", asset.ID).First(&pending).Error
					if errPending != nil {
						event := model.AssetEvent{
							AssetID:      asset.ID,
							EventType:    "hardware_change",
							Title:        "自动发现硬件配置变更",
							Description:  "变更详情: " + strings.Join(diff, ", ") + "\n当前快照: " + hwSnapshot,
							ReviewStatus: 20, // 待审核
						}
						store.DB.Create(&event)
					}
				}
			}
		}
	}
}

// applyAssetBookSpecs 用 Agent 采集值填充资产的账面硬件规格。
// 只写入仍为空的字段，返回是否有改动：账面值以人工台账维护为准，采集仅负责初始化
func applyAssetBookSpecs(asset *model.Asset, hw *protocol.Hardware, primaryMAC string) bool {
	dirty := false
	if asset.CPUName == "" && len(hw.CPU) > 0 {
		asset.CPUName = hw.CPU[0].Model
		dirty = true
	}
	if asset.MemorySize == "" && hw.MemoryTotalMB > 0 {
		asset.MemorySize = fmt.Sprintf("%dG", hw.MemoryTotalMB/1024)
		dirty = true
	}
	// 跳过 U 盘等可移动介质，仅落内置磁盘
	var fixedDisks []protocol.Disk
	for _, d := range hw.Disks {
		if !d.Removable {
			fixedDisks = append(fixedDisks, d)
		}
	}
	formatDisk := func(d protocol.Disk) string {
		t := strings.TrimSpace(d.Type)
		if t == "" {
			return fmt.Sprintf("%dG", d.SizeGB)
		}
		return fmt.Sprintf("%dG %s", d.SizeGB, t)
	}
	if asset.MainDisk == "" && len(fixedDisks) > 0 {
		asset.MainDisk = formatDisk(fixedDisks[0])
		dirty = true
	}
	if asset.SecondaryDisk == "" && len(fixedDisks) > 1 {
		asset.SecondaryDisk = formatDisk(fixedDisks[1])
		dirty = true
	}
	if asset.GPUName == "" && len(hw.GPUs) > 0 {
		// 跳过向日葵/ToDesk 等远控虚拟显卡，取第一块物理显卡（兼容未过滤的旧版 Agent）
		for _, g := range hw.GPUs {
			if !hwfilter.IsVirtualDisplay(g.Model) {
				asset.GPUName = g.Model
				break
			}
		}
		if asset.GPUName != "" {
			dirty = true
		}
	}
	if asset.MACAddress == "" && primaryMAC != "" {
		asset.MACAddress = primaryMAC
		dirty = true
	}
	return dirty
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
