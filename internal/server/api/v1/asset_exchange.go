package v1

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"itagent/internal/server/api/middleware"
	"itagent/internal/server/assetexcel"
	"itagent/internal/server/depreciation"
	"itagent/internal/server/model"
	"itagent/internal/server/store"
)

// AssetExchangeHandler 台账 Excel 批量导入导出（阶段五 P1 首项）。
// xlsx 解析/生成与行级校验收口在 internal/server/assetexcel 纯函数包，
// 此处只做查库装配、多租户/角色边界与事务落库
type AssetExchangeHandler struct{}

// maxImportFileSize 导入文件大小上限：5MB 对 2000 行数据绰绰有余，防超大上传拖垮请求
const maxImportFileSize = 5 << 20

// registerAssetExchangeRoutes 挂 /assets 前缀（由 RegisterAssetRoutes 调用）：
// 与 /:id 静态段同树注册 gin 允许（盘点 /labels 先例）
func registerAssetExchangeRoutes(r *gin.RouterGroup) {
	adminAssets := r.Group("/assets")
	adminAssets.Use(middleware.RoleMiddleware("admin"))
	h := &AssetExchangeHandler{}
	{
		adminAssets.GET("/export", h.Export)
		adminAssets.GET("/import-template", h.ImportTemplate)
		adminAssets.POST("/import", h.Import)
	}
}

// Export 按当前筛选条件导出台账 xlsx；过滤口径与列表页共用
// applyAssetListFilter，所见即所得
func (h *AssetExchangeHandler) Export(c *gin.Context) {
	var query ListAssetQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}

	db := applyAssetListFilter(store.DB.Model(&model.Asset{}), query)

	var total int64
	if err := db.Count(&total).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "failed to count assets")
		return
	}
	if total > assetexcel.MaxExportRows {
		Fail(c, http.StatusBadRequest, 40007,
			fmt.Sprintf("导出结果 %d 行超过上限 %d 行，请缩小筛选范围后分批导出", total, assetexcel.MaxExportRows))
		return
	}

	var items []model.Asset
	if err := db.Preload("Company").Preload("User").
		Order("assets.id desc").Find(&items).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "failed to query assets")
		return
	}

	rows := make([]assetexcel.Row, 0, len(items))
	for _, a := range items {
		rows = append(rows, exchangeRowFromAsset(a))
	}
	data, err := assetexcel.Export(rows)
	if err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "生成导出文件失败: "+err.Error())
		return
	}

	filename := fmt.Sprintf("assets-%s.xlsx", time.Now().Format("20060102-150405"))
	c.Header("Content-Disposition", `attachment; filename="`+filename+`"`)
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", data)
}

// exchangeRowFromAsset model.Asset → assetexcel.Row；领用人/公司为导出富化列，
// 回传导入时被忽略（公司归属由导入表单决定）
func exchangeRowFromAsset(a model.Asset) assetexcel.Row {
	row := assetexcel.Row{
		AssetTag:       a.AssetTag,
		CenterName:     a.CenterName,
		DepartmentName: a.DepartmentName,
		DepartmentSub:  a.DepartmentSub,
		Location:       a.Location,
		ManagerName:    a.ManagerName,
		CategoryID:     a.CategoryID,
		CategoryName:   a.CategoryName,
		U8OrderNo:      a.U8OrderNo,
		Status:         a.Status,
		Brand:          a.Brand,
		ModelName:      a.ModelName,
		SerialNumber:   a.SerialNumber,
		CPUName:        a.CPUName,
		MemorySize:     a.MemorySize,
		MainDisk:       a.MainDisk,
		SecondaryDisk:  a.SecondaryDisk,
		GPUName:        a.GPUName,
		MACAddress:     a.MACAddress,
		PurchaseDate:   a.PurchaseDate,
		Acceptor:       a.Acceptor,
		WarrantyPeriod: a.WarrantyPeriod,
		OriginalPrice:  a.OriginalPrice,
		NetValue:       a.NetValue,
		SecEncrypted:   a.SecEncrypted,
		Remark:         a.Remark,
	}
	if a.User != nil {
		row.Assignee = a.User.RealName
	}
	if a.Company.ID > 0 {
		row.CompanyName = a.Company.Name
	}
	return row
}

// ImportTemplate 下发导入模板（表头 + 示例行 + 类别/状态下拉）
func (h *AssetExchangeHandler) ImportTemplate(c *gin.Context) {
	data, err := assetexcel.Template()
	if err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "生成导入模板失败: "+err.Error())
		return
	}
	c.Header("Content-Disposition", `attachment; filename="asset-import-template.xlsx"`)
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", data)
}

// importAssetForm 导入表单：company_id 必填（多租户边界）；
// depreciation_id 可选——挂折旧规则让导入的存量资产直接进入自动折旧闭环
type importAssetForm struct {
	CompanyID      int64 `form:"company_id" binding:"required"`
	DepreciationID int64 `form:"depreciation_id"`
}

func (h *AssetExchangeHandler) Import(c *gin.Context) {
	var form importAssetForm
	if err := c.ShouldBind(&form); err != nil {
		Fail(c, http.StatusBadRequest, 40001, "company_id 为必填参数")
		return
	}
	fileHeader, err := c.FormFile("file")
	if err != nil {
		Fail(c, http.StatusBadRequest, 40003, "请上传 Excel 文件（表单字段名 file）")
		return
	}
	if fileHeader.Size > maxImportFileSize {
		Fail(c, http.StatusBadRequest, 40004, "文件超过 5MB 上限")
		return
	}
	src, err := fileHeader.Open()
	if err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "读取上传文件失败")
		return
	}
	defer src.Close()
	data, err := io.ReadAll(src)
	if err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "读取上传文件失败")
		return
	}

	rows, rowErrs, err := assetexcel.Parse(data)
	if err != nil {
		Fail(c, http.StatusBadRequest, 40005, err.Error())
		return
	}
	if len(rows) == 0 {
		Fail(c, http.StatusBadRequest, 40005, "表格没有可导入的数据行")
		return
	}

	// DB 已占用编码：asset_tag 是全局唯一索引（跨公司也不允许重复），
	// 且软删除记录仍占位，必须 Unscoped 一并查出，否则批量插入撞唯一键
	tags := make([]string, 0, len(rows))
	for _, r := range rows {
		tags = append(tags, r.AssetTag)
	}
	var occupied []model.Asset
	if err := store.DB.Unscoped().Where("asset_tag IN ?", tags).Find(&occupied).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "查询资产编码占用失败: "+err.Error())
		return
	}
	for _, a := range occupied {
		// Row=0 表示非文件行号（系统查重），前端按"已存在"渲染
		rowErrs = append(rowErrs, assetexcel.RowError{
			Reason: fmt.Sprintf("资产编码 %s 已存在（含回收站中的已删除记录）", a.AssetTag),
		})
	}
	if len(rowErrs) > 0 {
		// 行级校验全量拒收：台账建账宁可整批重来，不留半截数据
		failImportRows(c, rowErrs)
		return
	}

	// 折旧规则挂接（可选）：跨公司/不存在直接拒收。净值优先级：
	// Excel 账面净值 > 规则即时计算 > 0；挂上规则后折旧引擎每小时兜底重刷
	var (
		rule   *model.DepreciationRule
		stages []depreciation.Stage
	)
	if form.DepreciationID > 0 {
		var r model.DepreciationRule
		if err := store.DB.Where("id = ? AND company_id = ?", form.DepreciationID, form.CompanyID).
			First(&r).Error; err != nil {
			Fail(c, http.StatusNotFound, 40402, "折旧规则不存在或不属于该公司")
			return
		}
		parsed, err := depreciation.ParseStages(r.Stages)
		if err != nil {
			Fail(c, http.StatusBadRequest, 40008, "折旧规则阶梯配置非法: "+err.Error())
			return
		}
		rule, stages = &r, parsed
	}

	now := time.Now()
	assets := make([]model.Asset, 0, len(rows))
	for _, row := range rows {
		a := model.Asset{
			CompanyID:      form.CompanyID,
			CenterName:     row.CenterName,
			DepartmentName: row.DepartmentName,
			DepartmentSub:  row.DepartmentSub,
			Location:       row.Location,
			ManagerName:    row.ManagerName,
			CategoryID:     row.CategoryID,
			CategoryName:   row.CategoryName,
			AssetTag:       row.AssetTag,
			U8OrderNo:      row.U8OrderNo,
			Status:         row.Status,
			Brand:          row.Brand,
			ModelName:      row.ModelName,
			SerialNumber:   row.SerialNumber,
			CPUName:        row.CPUName,
			MemorySize:     row.MemorySize,
			MainDisk:       row.MainDisk,
			SecondaryDisk:  row.SecondaryDisk,
			GPUName:        row.GPUName,
			MACAddress:     row.MACAddress,
			PurchaseDate:   row.PurchaseDate,
			Acceptor:       row.Acceptor,
			WarrantyPeriod: row.WarrantyPeriod,
			OriginalPrice:  row.OriginalPrice,
			NetValue:       row.NetValue,
			SecEncrypted:   row.SecEncrypted,
			Remark:         row.Remark,
		}
		if rule != nil {
			depID := rule.ID
			a.DepreciationID = &depID
			if a.NetValue == 0 {
				a.NetValue = depreciation.NetValue(depreciation.Spec{
					OriginalPrice: a.OriginalPrice,
					PurchaseDate:  derefTime(row.PurchaseDate),
					TotalMonths:   rule.Months,
					FloorType:     rule.FloorType,
					FloorVal:      rule.FloorVal,
					Stages:        stages,
				}, now)
			}
		}
		assets = append(assets, a)
	}

	operatorID := operatorIDFromContext(c)
	if err := store.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.CreateInBatches(assets, 200).Error; err != nil {
			return fmt.Errorf("批量写入资产失败: %w", err)
		}
		events := make([]model.AssetEvent, 0, len(assets))
		for i := range assets {
			events = append(events, model.AssetEvent{
				AssetID:     assets[i].ID,
				EventType:   model.AssetEventCreate,
				Title:       "Excel 批量导入建账",
				Description: "通过台账 Excel 批量导入创建",
				OperatorID:  operatorID,
			})
		}
		if err := tx.CreateInBatches(events, 200).Error; err != nil {
			return fmt.Errorf("写入建账履历失败: %w", err)
		}
		return nil
	}); err != nil {
		// 预检之外的并发撞码由 DB 唯一索引兜底，事务整体回滚
		Fail(c, http.StatusInternalServerError, 50006, "导入失败: "+err.Error())
		return
	}
	Success(c, gin.H{"created": len(assets)})
}

// failImportRows 行级校验失败：整体拒收并回传错误明细（统一信封，code 非零）
func failImportRows(c *gin.Context, rowErrs []assetexcel.RowError) {
	c.JSON(http.StatusBadRequest, Response{
		Code:    40006,
		Message: fmt.Sprintf("共 %d 行数据未通过校验，未导入任何资产，请修正后重新上传", len(rowErrs)),
		Data:    gin.H{"errors": rowErrs, "error_count": len(rowErrs)},
	})
}

// derefTime *time.Time → time.Time（nil 返回零值；NetValue 对零值按未开始折旧处理）
func derefTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

// operatorIDFromContext 从 JWT claims 取操作人 ID（缺失返回 nil）
func operatorIDFromContext(c *gin.Context) *int64 {
	if uid, ok := c.Get("userID"); ok {
		if id, ok := uid.(int64); ok && id > 0 {
			return &id
		}
	}
	return nil
}
