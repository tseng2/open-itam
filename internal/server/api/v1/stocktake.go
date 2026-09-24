package v1

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"itagent/internal/server/api/middleware"
	"itagent/internal/server/label"
	"itagent/internal/server/model"
	"itagent/internal/server/store"
)

// StocktakeHandler 盘点任务（阶段五 P0-β）：任务状态机（草稿→盘点中→完成/取消）、
// 明细快照圈定、管理端核对修正与扫码令牌管理；履历联动见 applyStocktakeSideEffects
type StocktakeHandler struct {
	store *store.GormStore
}

// RegisterStocktakeRoutes 注册盘点任务管理路由：资产管理动作，仅管理员与超管可操作
func RegisterStocktakeRoutes(protected *gin.RouterGroup) {
	g := protected.Group("/stocktakes")
	g.Use(middleware.RoleMiddleware("admin"))
	h := &StocktakeHandler{store: store.NewGormStore(store.DB)}
	{
		g.GET("", h.List)
		g.POST("", h.Create)
		g.GET("/:id", h.Get)
		g.POST("/:id/start", h.Start)
		g.POST("/:id/finish", h.Finish)
		g.POST("/:id/cancel", h.Cancel)
		g.POST("/:id/rotate-token", h.RotateToken)
		g.GET("/:id/items", h.ListItems)
		g.POST("/:id/items", h.CheckItems)
		g.POST("/labels", h.Labels)
	}
}

// maxLabelBatch 单次打印标签上限：500 枚 ≈ 24 页，防超大 PDF 拖垮请求
const maxLabelBatch = 500

// stocktakeResultNames 核对结果的展示名（履历事件标题用；前端另有渲染映射）
var stocktakeResultNames = map[int]string{
	model.StocktakeItemNormal:   "正常",
	model.StocktakeItemLost:     "丢失",
	model.StocktakeItemDamaged:  "损坏",
	model.StocktakeItemScrapped: "报废",
}

type listStocktakeQuery struct {
	CompanyID int64 `form:"company_id"`
	Status    int   `form:"status"`
	Page      int   `form:"page,default=1"`
	PageSize  int   `form:"page_size,default=20"`
}

// stocktakeListRow 列表行：任务 + 明细进度（建账总数与已核数）富化
type stocktakeListRow struct {
	model.Stocktake
	ItemTotal   int64 `json:"item_total"`
	ItemChecked int64 `json:"item_checked"`
}

func (h *StocktakeHandler) List(c *gin.Context) {
	var q listStocktakeQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}
	tasks, total, err := h.store.ListStocktakes(c.Request.Context(), store.StocktakeListFilter{
		CompanyID: q.CompanyID,
		Status:    q.Status,
		Page:      q.Page,
		PageSize:  q.PageSize,
	})
	if err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "查询盘点任务失败")
		return
	}

	// 一次 GROUP BY 富化整页任务的进度，避免逐行计数
	rows := make([]stocktakeListRow, 0, len(tasks))
	if len(tasks) > 0 {
		ids := make([]int64, 0, len(tasks))
		for _, t := range tasks {
			ids = append(ids, t.ID)
		}
		type progressRow struct {
			StocktakeID int64
			Total       int64
			Checked     int64
		}
		var progress []progressRow
		if err := store.DB.WithContext(c.Request.Context()).Model(&model.StocktakeItem{}).
			Select("stocktake_id, COUNT(*) AS total, SUM(CASE WHEN result <> ? THEN 1 ELSE 0 END) AS checked", model.StocktakeItemPending).
			Where("company_id = ? AND stocktake_id IN ?", q.CompanyID, ids).
			Group("stocktake_id").Scan(&progress).Error; err != nil {
			Fail(c, http.StatusInternalServerError, 50002, "统计盘点进度失败")
			return
		}
		byTask := make(map[int64]progressRow, len(progress))
		for _, p := range progress {
			byTask[p.StocktakeID] = p
		}
		for _, t := range tasks {
			rows = append(rows, stocktakeListRow{
				Stocktake:   t,
				ItemTotal:   byTask[t.ID].Total,
				ItemChecked: byTask[t.ID].Checked,
			})
		}
	}
	Success(c, PageResult{Total: total, Items: rows})
}

type createStocktakeRequest struct {
	CompanyID int64  `json:"company_id" binding:"required"`
	Name      string `json:"name" binding:"required"`
	Remark    string `json:"remark"`
	// 圈定范围：asset_ids 显式手选；为空时按过滤条件全量快照，
	// 过滤条件也不填 = 该公司全部未报废资产
	AssetIDs          []int64 `json:"asset_ids"`
	CategoryID        int64   `json:"category_id"`
	Status            int     `json:"status"`
	LocationKeyword   string `json:"location_keyword"`
	DepartmentKeyword string `json:"department_keyword"`
}

// snapshotStocktakeScope 圈定范围内的资产快照为明细：
// 已报废资产不参与盘点；跨公司 asset_ids 由 company 条件天然过滤
func snapshotStocktakeScope(c *gin.Context, req createStocktakeRequest) ([]model.StocktakeItem, error) {
	q := store.DB.WithContext(c.Request.Context()).Model(&model.Asset{}).
		Where("company_id = ? AND status <> ?", req.CompanyID, model.AssetStatusScrapped)
	if len(req.AssetIDs) > 0 {
		q = q.Where("id IN ?", req.AssetIDs)
	}
	if req.CategoryID > 0 {
		q = q.Where("category_id = ?", req.CategoryID)
	}
	if req.Status > 0 {
		q = q.Where("status = ?", req.Status)
	}
	if req.LocationKeyword != "" {
		q = q.Where("location LIKE ?", "%"+req.LocationKeyword+"%")
	}
	if req.DepartmentKeyword != "" {
		q = q.Where("department_name LIKE ?", "%"+req.DepartmentKeyword+"%")
	}
	var assets []model.Asset
	if err := q.Order("id asc").Find(&assets).Error; err != nil {
		return nil, err
	}

	items := make([]model.StocktakeItem, 0, len(assets))
	for _, a := range assets {
		items = append(items, model.StocktakeItem{
			CompanyID:        req.CompanyID,
			AssetID:           a.ID,
			AssetTag:          a.AssetTag,
			ExpectedLocation: a.Location,
			ExpectedStatus:   a.Status,
		})
	}
	return items, nil
}

func (h *StocktakeHandler) Create(c *gin.Context) {
	var req createStocktakeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}
	items, err := snapshotStocktakeScope(c, req)
	if err != nil {
		Fail(c, http.StatusInternalServerError, 50002, "圈定盘点范围失败")
		return
	}
	if len(items) == 0 {
		Fail(c, http.StatusBadRequest, 40004, "圈定范围内没有可盘点的资产")
		return
	}

	created, err := h.store.CreateStocktake(c.Request.Context(), model.Stocktake{
		CompanyID: req.CompanyID,
		Name:      req.Name,
		Remark:    req.Remark,
		CreatedBy: currentUserID(c),
	}, items)
	if err != nil {
		Fail(c, http.StatusInternalServerError, 50003, "创建盘点任务失败")
		return
	}
	Success(c, stocktakeListRow{Stocktake: created, ItemTotal: int64(len(items))})
}

// stocktakeDetail 详情载荷：任务 + 各结果段位计数（漏盘即 pending 数）
type stocktakeDetail struct {
	model.Stocktake
	Counts map[int]int64 `json:"counts"`
}

type stocktakeCompanyQuery struct {
	CompanyID int64 `form:"company_id" binding:"required"`
}

func (h *StocktakeHandler) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, 40002, "invalid id")
		return
	}
	var q stocktakeCompanyQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}
	st, err := h.store.GetStocktake(c.Request.Context(), q.CompanyID, id)
	if err != nil {
		failStocktakeStoreError(c, err, "查询盘点任务失败")
		return
	}
	counts, err := h.store.CountStocktakeResults(c.Request.Context(), q.CompanyID, id)
	if err != nil {
		Fail(c, http.StatusInternalServerError, 50002, "统计盘点结果失败")
		return
	}
	Success(c, stocktakeDetail{Stocktake: st, Counts: counts})
}

type stocktakeActionRequest struct {
	CompanyID int64 `json:"company_id" binding:"required"`
}

func (h *StocktakeHandler) Start(c *gin.Context) {
	id, req, ok := parseStocktakeAction(c)
	if !ok {
		return
	}
	st, token, err := h.store.StartStocktake(c.Request.Context(), req.CompanyID, id)
	if err != nil {
		failStocktakeStoreError(c, err, "开始盘点失败")
		return
	}
	// 扫码令牌明文仅此一次返回；前端据此渲染二维码与复制链接
	Success(c, gin.H{"stocktake": st, "scan_token": token})
}

func (h *StocktakeHandler) Finish(c *gin.Context) {
	id, req, ok := parseStocktakeAction(c)
	if !ok {
		return
	}
	st, err := h.store.FinishStocktake(c.Request.Context(), req.CompanyID, id, time.Now().UTC())
	if err != nil {
		failStocktakeStoreError(c, err, "结束盘点失败")
		return
	}
	Success(c, st)
}

func (h *StocktakeHandler) Cancel(c *gin.Context) {
	id, req, ok := parseStocktakeAction(c)
	if !ok {
		return
	}
	st, err := h.store.CancelStocktake(c.Request.Context(), req.CompanyID, id)
	if err != nil {
		failStocktakeStoreError(c, err, "取消盘点任务失败")
		return
	}
	Success(c, st)
}

func (h *StocktakeHandler) RotateToken(c *gin.Context) {
	id, req, ok := parseStocktakeAction(c)
	if !ok {
		return
	}
	token, err := h.store.RotateStocktakeToken(c.Request.Context(), req.CompanyID, id)
	if err != nil {
		failStocktakeStoreError(c, err, "重新生成盘点码失败")
		return
	}
	Success(c, gin.H{"scan_token": token})
}

type listStocktakeItemQuery struct {
	CompanyID   int64  `form:"company_id"`
	Result      int    `form:"result"`
	Keyword     string `form:"keyword"`
	Page        int    `form:"page,default=1"`
	PageSize    int    `form:"page_size,default=20"`
}

func (h *StocktakeHandler) ListItems(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, 40002, "invalid id")
		return
	}
	var q listStocktakeItemQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}
	items, total, err := h.store.ListStocktakeItems(c.Request.Context(), store.StocktakeItemListFilter{
		CompanyID:   q.CompanyID,
		StocktakeID: id,
		Result:      q.Result,
		Keyword:     q.Keyword,
		Page:        q.Page,
		PageSize:    q.PageSize,
	})
	if err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "查询盘点明细失败")
		return
	}
	Success(c, PageResult{Total: total, Items: items})
}

type checkStocktakeItemsRequest struct {
	CompanyID int64                   `json:"company_id" binding:"required"`
	ScannedBy string                  `json:"scanned_by"`
	Checks    []model.StocktakeCheck `json:"checks" binding:"required,min=1"`
}

func (h *StocktakeHandler) CheckItems(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, 40002, "invalid id")
		return
	}
	var req checkStocktakeItemsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}
	scannedBy := req.ScannedBy
	if scannedBy == "" {
		scannedBy = "管理员"
	}
	items, err := h.store.CheckStocktakeItems(c.Request.Context(), req.CompanyID, id, req.Checks, scannedBy)
	if err != nil {
		failStocktakeCheckError(c, err)
		return
	}

	st, err := h.store.GetStocktake(c.Request.Context(), req.CompanyID, id)
	if err != nil {
		failStocktakeStoreError(c, err, "回查盘点任务失败")
		return
	}
	applyStocktakeSideEffects(c, st, items, scannedBy, currentUserID(c))
	Success(c, gin.H{"items": items})
}

// applyStocktakeSideEffects 核对落库后的履历联动（失败不回滚主流程——
// 核对已生效，履历可事后补录；错误经 gin 错误链记录，与 dispatch 先例一致）：
//  1. 异常结果（丢失/损坏/报废）逐资产记 stocktake AssetEvent
//  2. "正常"结果自动确认该资产 pending 的 hardware_change 待审事件
//     ——A3 协同红利：盘点是天然的人工确认场景
func applyStocktakeSideEffects(c *gin.Context, st model.Stocktake, items []model.StocktakeItem, scannedBy string, operatorID *int64) {
	var normalAssetIDs []int64
	for _, it := range items {
		if it.Result == model.StocktakeItemNormal {
			normalAssetIDs = append(normalAssetIDs, it.AssetID)
		}
	}
	if len(normalAssetIDs) > 0 {
		res := store.DB.WithContext(c.Request.Context()).Model(&model.AssetEvent{}).
			Where("asset_id IN ? AND event_type = ? AND review_status = ?",
				normalAssetIDs, model.AssetEventHardwareChange, model.AssetEventReviewPending).
			Update("review_status", model.AssetEventReviewDone)
		if res.Error != nil {
			c.Error(fmt.Errorf("auto-confirm hardware_change events: %w", res.Error))
		}
	}

	for _, it := range items {
		name, abnormal := stocktakeResultNames[it.Result]
		if !abnormal {
			continue
		}
		event := model.AssetEvent{
			AssetID:     it.AssetID,
			EventType:   model.AssetEventStocktake,
			Title:       "盘点异常：" + name,
			Description: fmt.Sprintf("盘点任务《%s》；实盘位置：%s；备注：%s",
				st.Name, it.ActualLocation, it.Remark),
			TargetPerson: scannedBy, // 盘点人（移动端自由文本）
			ReviewStatus: model.AssetEventReviewDone,
			OperatorID:   operatorID,
		}
		if err := store.DB.WithContext(c.Request.Context()).Create(&event).Error; err != nil {
			c.Error(fmt.Errorf("record stocktake event for item %d: %w", it.ID, err))
		}
	}
}

// parseStocktakeAction 解析动作类接口的路径 id 与请求体 company_id：
// 盘点属公司维度实体，任何操作必须带公司边界
func parseStocktakeAction(c *gin.Context) (id int64, req stocktakeActionRequest, ok bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, 40002, "invalid id")
		return 0, req, false
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return 0, req, false
	}
	return id, req, true
}

// failStocktakeStoreError store 业务哨兵映射为标准错误信封：
// 不存在或跨公司→404；状态不允许流转→400；其余→500
func failStocktakeStoreError(c *gin.Context, err error, fallbackMsg string) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		Fail(c, http.StatusNotFound, 40401, "盘点任务不存在")
	case errors.Is(err, store.ErrInvalidState):
		Fail(c, http.StatusBadRequest, 40003, "盘点任务当前状态不允许该操作")
	default:
		Fail(c, http.StatusInternalServerError, 50001, fallbackMsg)
	}
}

// failStocktakeCheckError 核对失败映射：明细未命中（范围外/不存在）单列 40404，
// 便于前端提示"资产不在本次盘点范围"
func failStocktakeCheckError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		Fail(c, http.StatusNotFound, 40404, "明细不存在或不在本次盘点范围内")
	case errors.Is(err, store.ErrInvalidState):
		Fail(c, http.StatusBadRequest, 40003, "盘点任务当前状态不允许核对")
	default:
		Fail(c, http.StatusBadRequest, 40005, err.Error())
	}
}

type stocktakeLabelsRequest struct {
	CompanyID   int64    `json:"company_id" binding:"required"`
	StocktakeID int64    `json:"stocktake_id"` // 按任务明细整批打印（优先）
	AssetTags   []string `json:"asset_tags"`   // 或手选资产编码
	// BaseURL 标签二维码的前缀（前端传 window.location.origin），
	// QR 内容 `${base}/#/a/${tag}`；为空时 QR 直接编码资产编码
	BaseURL string `json:"base_url"`
}

// Labels 渲染资产标签 PDF：标签与盘点任务解耦（可重复利用），
// 扫码直达免登录移动核对页 /#/a/:asset_tag
func (h *StocktakeHandler) Labels(c *gin.Context) {
	var req stocktakeLabelsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}

	var assets []model.Asset
	switch {
	case req.StocktakeID > 0:
		// 先验任务边界，防止盲扫他司任务
		if _, err := h.store.GetStocktake(c.Request.Context(), req.CompanyID, req.StocktakeID); err != nil {
			failStocktakeStoreError(c, err, "查询盘点任务失败")
			return
		}
		var assetIDs []int64
		if err := store.DB.WithContext(c.Request.Context()).Model(&model.StocktakeItem{}).
			Where("company_id = ? AND stocktake_id = ?", req.CompanyID, req.StocktakeID).
			Order("id asc").Limit(maxLabelBatch).Pluck("asset_id", &assetIDs).Error; err != nil {
			Fail(c, http.StatusInternalServerError, 50002, "查询盘点明细失败")
			return
		}
		if len(assetIDs) == 0 {
			Fail(c, http.StatusBadRequest, 40004, "该任务没有可打印的明细")
			return
		}
		if err := store.DB.WithContext(c.Request.Context()).
			Where("company_id = ? AND id IN ?", req.CompanyID, assetIDs).
			Order("id asc").Find(&assets).Error; err != nil {
			Fail(c, http.StatusInternalServerError, 50003, "查询资产失败")
			return
		}
	case len(req.AssetTags) > 0:
		if len(req.AssetTags) > maxLabelBatch {
			Fail(c, http.StatusBadRequest, 40002, "单次最多打印 500 枚标签")
			return
		}
		if err := store.DB.WithContext(c.Request.Context()).
			Where("company_id = ? AND asset_tag IN ?", req.CompanyID, req.AssetTags).
			Order("id asc").Find(&assets).Error; err != nil {
			Fail(c, http.StatusInternalServerError, 50003, "查询资产失败")
			return
		}
		if len(assets) == 0 {
			Fail(c, http.StatusBadRequest, 40004, "没有匹配的资产")
			return
		}
	default:
		Fail(c, http.StatusBadRequest, 40006, "必须指定盘点任务或资产编码范围")
		return
	}

	labels := make([]label.Label, 0, len(assets))
	for _, a := range assets {
		qrText := a.AssetTag
		if req.BaseURL != "" {
			qrText = strings.TrimSuffix(req.BaseURL, "/") + "/#/a/" + a.AssetTag
		}
		labels = append(labels, label.Label{
			Tag:      a.AssetTag,
			Category: a.CategoryName,
			Brand:    a.Brand,
			Model:    a.ModelName,
			SN:       a.SerialNumber,
			Location: a.Location,
			QRText:   qrText,
		})
	}
	pdfBytes, err := label.RenderLabelsPDF(labels)
	if err != nil {
		Fail(c, http.StatusInternalServerError, 50004, "渲染标签失败")
		return
	}
	c.Header("Content-Disposition", `attachment; filename="stocktake-labels.pdf"`)
	c.Data(http.StatusOK, "application/pdf", pdfBytes)
}
