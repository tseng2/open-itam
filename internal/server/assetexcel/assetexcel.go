// Package assetexcel 台账 Excel 批量导入导出（阶段五 P1 首项）。
// 纯函数包：xlsx 解析/生成与行级校验收口在此，gin API 只做装配与落库。
// 设计要点：
//   - 导入按表头名定位列（列序无关、未知表头忽略——导出文件可直接回传再导入）；
//   - 解析使用原始单元格值（RawCellValue）：真日期单元格读到的是 Excel 序列号，
//     货币样式不会被格式化成歧义文本；
//   - 行级问题（空编码/文件内重复/无法识别的类别状态日期金额）进 RowError 清单，
//     调用方整体拒收；文件级问题（格式/缺表头/超行数）直接返回 err
package assetexcel

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"

	"itagent/internal/server/model"
)

const (
	// SheetNameAssets 导入导出统一工作表名
	SheetNameAssets = "资产台账"
	// MaxImportRows 单次导入行数上限（防超大文件拖垮请求，超出请分批）
	MaxImportRows = 2000
	// MaxExportRows 单次导出行数上限（API 层查库前预检，超出请缩小筛选范围）
	MaxExportRows = 5000
)

// fieldKey 导入列字段标识；导入按表头名定位列，导出/模板按固定顺序输出
type fieldKey int

const (
	fieldAssetTag fieldKey = iota
	fieldCenterName
	fieldDepartmentName
	fieldDepartmentSub
	fieldLocation
	fieldManagerName
	fieldCategory
	fieldU8OrderNo
	fieldStatus
	fieldBrand
	fieldModelName
	fieldSerialNumber
	fieldCPUName
	fieldMemorySize
	fieldMainDisk
	fieldSecondaryDisk
	fieldGPUName
	fieldMACAddress
	fieldPurchaseDate
	fieldAcceptor
	fieldWarrantyPeriod
	fieldOriginalPrice
	fieldNetValue
	fieldSecEncrypted
	fieldRemark
	fieldCount
)

// importColumns 导入列规格：key 与表头名（导出/模板同序输出）
var importColumns = [fieldCount]struct {
	key    fieldKey
	header string
}{
	{fieldAssetTag, "资产编码"},
	{fieldCenterName, "中心"},
	{fieldDepartmentName, "部门"},
	{fieldDepartmentSub, "部门2"},
	{fieldLocation, "存放位置"},
	{fieldManagerName, "资产负责人"},
	{fieldCategory, "类别"},
	{fieldU8OrderNo, "U8订单号"},
	{fieldStatus, "状态"},
	{fieldBrand, "品牌"},
	{fieldModelName, "型号"},
	{fieldSerialNumber, "序列号"},
	{fieldCPUName, "CPU"},
	{fieldMemorySize, "内存"},
	{fieldMainDisk, "主硬盘"},
	{fieldSecondaryDisk, "从硬盘"},
	{fieldGPUName, "显卡"},
	{fieldMACAddress, "MAC地址"},
	{fieldPurchaseDate, "购入日期"},
	{fieldAcceptor, "验收人"},
	{fieldWarrantyPeriod, "保修期"},
	{fieldOriginalPrice, "原值"},
	{fieldNetValue, "净值"},
	{fieldSecEncrypted, "加密软件"},
	{fieldRemark, "备注"},
}

// 导出在导入列之后追加的富化列（导入解析时按未知表头忽略）
const (
	headerAssignee = "领用人"
	headerCompany  = "公司"
)

// Row 一行台账数据：解析结果与导出输入共用同一形状；
// Assignee/CompanyName 仅导出富化用（当前领用人/所属公司），不参与导入
type Row struct {
	AssetTag       string
	CenterName     string
	DepartmentName string
	DepartmentSub  string
	Location       string
	ManagerName    string
	CategoryID     int64
	CategoryName   string
	U8OrderNo      string
	Status         int
	Brand          string
	ModelName      string
	SerialNumber   string
	CPUName        string
	MemorySize     string
	MainDisk       string
	SecondaryDisk  string
	GPUName        string
	MACAddress     string
	PurchaseDate   *time.Time
	Acceptor       string
	WarrantyPeriod string
	OriginalPrice  float64
	NetValue       float64
	SecEncrypted   bool
	Remark         string
	Assignee       string
	CompanyName    string
}

// RowError 行级校验错误：Row 为 Excel 物理行号（1 起，含表头行），便于用户按行修复
type RowError struct {
	Row    int    `json:"row"`
	Reason string `json:"reason"`
}

// Parse 解析导入文件：取第一个工作表、首行为表头、按表头名定位列。
// 行级问题进 errs（调用方应整体拒收并回显），文件级问题返回 err
func Parse(data []byte) ([]Row, []RowError, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data), excelize.Options{RawCellValue: true})
	if err != nil {
		return nil, nil, fmt.Errorf("无法读取 Excel 文件: %w", err)
	}
	defer f.Close()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, nil, errors.New("Excel 文件没有工作表")
	}
	rows, err := f.GetRows(sheets[0], excelize.Options{RawCellValue: true})
	if err != nil {
		return nil, nil, fmt.Errorf("读取工作表失败: %w", err)
	}
	if len(rows) < 2 {
		return nil, nil, errors.New("表格为空：首行必须是表头，数据从第二行开始")
	}
	if len(rows)-1 > MaxImportRows {
		return nil, nil, fmt.Errorf("单次最多导入 %d 行，当前 %d 行，请分批导入", MaxImportRows, len(rows)-1)
	}

	// 表头名 → 列下标；未知表头（领用人/公司等导出富化列）忽略
	headerToKey := make(map[string]fieldKey, fieldCount)
	for _, col := range importColumns {
		headerToKey[col.header] = col.key
	}
	colIdx := make([]int, fieldCount)
	for i := range colIdx {
		colIdx[i] = -1
	}
	for i, h := range rows[0] {
		if key, ok := headerToKey[strings.TrimSpace(h)]; ok {
			colIdx[key] = i
		}
	}
	if colIdx[fieldAssetTag] == -1 {
		return nil, nil, errors.New("缺少必填表头: 资产编码")
	}

	var (
		parsed []Row
		errs   []RowError
		seen   = make(map[string]int, len(rows)) // asset_tag → 首次出现的物理行号（查文件内重复）
	)
	for i, cells := range rows[1:] {
		line := i + 2 // Excel 物理行号（表头占第 1 行）
		if isEmptyRow(cells) {
			continue // 模板尾部整行空常见，静默跳过
		}
		get := func(key fieldKey) string {
			idx := colIdx[key]
			if idx < 0 || idx >= len(cells) {
				return ""
			}
			return strings.TrimSpace(cells[idx])
		}

		tag := get(fieldAssetTag)
		if tag == "" {
			errs = append(errs, RowError{Row: line, Reason: "资产编码为空"})
			continue
		}
		if first, dup := seen[tag]; dup {
			errs = append(errs, RowError{Row: line, Reason: fmt.Sprintf("资产编码 %s 与第 %d 行重复", tag, first)})
			continue
		}

		row, reason := parseRow(get)
		if reason != "" {
			errs = append(errs, RowError{Row: line, Reason: reason})
			continue
		}
		row.AssetTag = tag
		seen[tag] = line
		parsed = append(parsed, row)
	}
	return parsed, errs, nil
}

// parseRow 解析一行业务字段；返回首个错误原因（空串即成功）。
// 逐字段出错即短路返回：行级错误一次报一个即可定位修复
func parseRow(get func(fieldKey) string) (Row, string) {
	row := Row{
		CenterName:     get(fieldCenterName),
		DepartmentName: get(fieldDepartmentName),
		DepartmentSub:  get(fieldDepartmentSub),
		Location:       get(fieldLocation),
		ManagerName:    get(fieldManagerName),
		U8OrderNo:      get(fieldU8OrderNo),
		Brand:          get(fieldBrand),
		ModelName:      get(fieldModelName),
		SerialNumber:   get(fieldSerialNumber),
		CPUName:        get(fieldCPUName),
		MemorySize:     get(fieldMemorySize),
		MainDisk:       get(fieldMainDisk),
		SecondaryDisk:  get(fieldSecondaryDisk),
		GPUName:        get(fieldGPUName),
		MACAddress:     get(fieldMACAddress),
		Acceptor:       get(fieldAcceptor),
		WarrantyPeriod: get(fieldWarrantyPeriod),
		Remark:         get(fieldRemark),
	}

	category := get(fieldCategory)
	if category == "" {
		return row, "类别为空"
	}
	id, canonical, ok := model.AssetCategoryIDByName(category)
	if !ok {
		return row, fmt.Sprintf("无法识别的类别: %q（应为 台式整机/笔记本电脑/显示器/外设及其他）", category)
	}
	row.CategoryID, row.CategoryName = id, canonical

	status, err := parseStatus(get(fieldStatus))
	if err != nil {
		return row, err.Error()
	}
	row.Status = status

	purchaseDate, err := parseDate(get(fieldPurchaseDate))
	if err != nil {
		return row, err.Error()
	}
	row.PurchaseDate = purchaseDate

	if row.OriginalPrice, err = parsePrice(get(fieldOriginalPrice)); err != nil {
		return row, err.Error()
	}
	if row.NetValue, err = parsePrice(get(fieldNetValue)); err != nil {
		return row, err.Error()
	}
	if row.SecEncrypted, err = parseBool(get(fieldSecEncrypted)); err != nil {
		return row, err.Error()
	}
	return row, ""
}

// Export 生成台账导出文件：导入列 + 领用人/公司富化列，可直接回传导入
func Export(rows []Row) ([]byte, error) {
	f, err := buildWorkbook(rows)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, fmt.Errorf("生成 Excel 文件失败: %w", err)
	}
	return buf.Bytes(), nil
}

// Template 生成导入模板：表头 + 一行示例 + 类别/状态下拉校验。
// 下拉之外的自由输入在解析时仍会被行级校验拦下
func Template() ([]byte, error) {
	f, err := buildWorkbook([]Row{exampleRow()})
	if err != nil {
		return nil, err
	}
	defer f.Close()

	dvCategory := excelize.NewDataValidation(true)
	dvCategory.Sqref = "G2:G1000"
	if err := dvCategory.SetDropList(categoryDropList()); err != nil {
		return nil, fmt.Errorf("设置类别下拉失败: %w", err)
	}
	if err := f.AddDataValidation(SheetNameAssets, dvCategory); err != nil {
		return nil, fmt.Errorf("设置类别下拉失败: %w", err)
	}
	dvStatus := excelize.NewDataValidation(true)
	dvStatus.Sqref = "I2:I1000"
	if err := dvStatus.SetDropList(statusDropList()); err != nil {
		return nil, fmt.Errorf("设置状态下拉失败: %w", err)
	}
	if err := f.AddDataValidation(SheetNameAssets, dvStatus); err != nil {
		return nil, fmt.Errorf("设置状态下拉失败: %w", err)
	}

	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, fmt.Errorf("生成模板文件失败: %w", err)
	}
	return buf.Bytes(), nil
}

// buildWorkbook 表头 + 数据行 + 列宽 + 表头加粗；模板与导出共用同一形状
func buildWorkbook(rows []Row) (*excelize.File, error) {
	f := excelize.NewFile()
	if err := f.SetSheetName("Sheet1", SheetNameAssets); err != nil {
		return nil, fmt.Errorf("重命名工作表失败: %w", err)
	}

	headers := make([]string, 0, fieldCount+2)
	for _, col := range importColumns {
		headers = append(headers, col.header)
	}
	headers = append(headers, headerAssignee, headerCompany)
	if err := f.SetSheetRow(SheetNameAssets, "A1", &headers); err != nil {
		return nil, fmt.Errorf("写入表头失败: %w", err)
	}

	// 表头加粗与列宽属展示修饰，失败即文件生成异常，不静默吞掉
	style, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	if err != nil {
		return nil, fmt.Errorf("创建表头样式失败: %w", err)
	}
	lastHeader, err := excelize.CoordinatesToCellName(len(headers), 1)
	if err != nil {
		return nil, err
	}
	if err := f.SetCellStyle(SheetNameAssets, "A1", lastHeader, style); err != nil {
		return nil, fmt.Errorf("设置表头样式失败: %w", err)
	}
	for _, w := range []struct{ start, end string; width float64 }{
		{"A", "A", 16},
		{"B", "Z", 14},
		{"AA", "AB", 14},
	} {
		if err := f.SetColWidth(SheetNameAssets, w.start, w.end, w.width); err != nil {
			return nil, fmt.Errorf("设置列宽失败: %w", err)
		}
	}

	for i, r := range rows {
		axis, err := excelize.CoordinatesToCellName(1, i+2)
		if err != nil {
			return nil, err
		}
		values := rowValues(r)
		if err := f.SetSheetRow(SheetNameAssets, axis, &values); err != nil {
			return nil, fmt.Errorf("写入数据行失败: %w", err)
		}
	}
	return f, nil
}

// rowValues Row → 单元格值序列（与表头一一对应）。日期写 yyyy-MM-dd 文本、
// 加密软件写 是/否、类别取规范名：保证导出文件可直接回导
func rowValues(r Row) []interface{} {
	purchaseDate := ""
	if r.PurchaseDate != nil {
		purchaseDate = r.PurchaseDate.Format("2006-01-02")
	}
	category := model.AssetCategoryName(r.CategoryID)
	if category == "" {
		category = r.CategoryName
	}
	sec := "否"
	if r.SecEncrypted {
		sec = "是"
	}
	return []interface{}{
		r.AssetTag, r.CenterName, r.DepartmentName, r.DepartmentSub, r.Location,
		r.ManagerName, category, r.U8OrderNo, AssetStatusText(r.Status), r.Brand,
		r.ModelName, r.SerialNumber, r.CPUName, r.MemorySize, r.MainDisk,
		r.SecondaryDisk, r.GPUName, r.MACAddress, purchaseDate, r.Acceptor,
		r.WarrantyPeriod, r.OriginalPrice, r.NetValue, sec, r.Remark,
		r.Assignee, r.CompanyName,
	}
}

// exampleRow 模板示例行（备注中提示导入前删除）
func exampleRow() Row {
	date := time.Date(2024, 6, 1, 0, 0, 0, 0, time.Local)
	return Row{
		AssetTag:       "IT-2024-0001",
		CenterName:     "研发中心",
		DepartmentName: "研发部",
		DepartmentSub:  "前端组",
		Location:       "3楼工位A-01",
		ManagerName:    "张三",
		CategoryID:     model.AssetCategoryNotebook,
		CategoryName:   "笔记本电脑",
		U8OrderNo:      "U8-2024-001",
		Status:         model.AssetStatusStock,
		Brand:          "联想",
		ModelName:      "ThinkPad T14p",
		SerialNumber:   "PF3ABC12",
		CPUName:        "i7-13700H",
		MemorySize:     "32G",
		MainDisk:       "1T SSD",
		PurchaseDate:   &date,
		Acceptor:       "李四",
		WarrantyPeriod: "3年",
		OriginalPrice:  8000,
		SecEncrypted:   true,
		Remark:         "示例行，导入前请删除本行",
	}
}

func categoryDropList() []string {
	return []string{
		model.AssetCategoryName(model.AssetCategoryPC),
		model.AssetCategoryName(model.AssetCategoryNotebook),
		model.AssetCategoryName(model.AssetCategoryMonitor),
		model.AssetCategoryName(model.AssetCategoryPeripheral),
	}
}

func statusDropList() []string {
	return []string{
		AssetStatusText(model.AssetStatusStock),
		AssetStatusText(model.AssetStatusInUse),
		AssetStatusText(model.AssetStatusRepair),
		AssetStatusText(model.AssetStatusScrapped),
	}
}

// AssetStatusText 状态码 → 展示文本（导出与模板下拉共用同一映射）
func AssetStatusText(status int) string {
	switch status {
	case model.AssetStatusStock:
		return "库存中"
	case model.AssetStatusInUse:
		return "使用中"
	case model.AssetStatusRepair:
		return "维修中"
	case model.AssetStatusScrapped:
		return "已报废"
	}
	return "未知"
}

// parseStatus 状态文本/状态码 → 状态码；空串默认库存中（新建账默认在库）。
// 中文与数字都收：用户手填文件两种都常见
func parseStatus(s string) (int, error) {
	switch strings.TrimSpace(s) {
	case "", "10", "库存中", "库存":
		return model.AssetStatusStock, nil
	case "20", "使用中", "在用":
		return model.AssetStatusInUse, nil
	case "30", "维修中", "维修":
		return model.AssetStatusRepair, nil
	case "40", "已报废", "报废":
		return model.AssetStatusScrapped, nil
	}
	return 0, fmt.Errorf("无法识别的状态: %q（应为 库存中/使用中/维修中/已报废）", s)
}

// dateTextLayouts 购入日期常见文本布局
var dateTextLayouts = []string{
	"2006-01-02", "2006/01/02", "2006.01.02",
	"2006-1-2", "2006/1/2", "2006.1.2",
	"2006-01-02 15:04:05", "2006-01-02T15:04:05",
}

// Excel 日期序列号合法区间（约 1982-07 ~ 2100 年）：
// 防止把普通数字（如金额 8000）误判成日期
const (
	minExcelSerialDays = 30000
	maxExcelSerialDays = 73415
)

// excelSerialBase Excel 日期序列号纪元基准：1899-12-30
var excelSerialBase = time.Date(1899, 12, 30, 0, 0, 0, 0, time.Local)

// parseDate 购入日期解析：文本布局优先，Excel 序列号兜底
//（RawCellValue 下真日期单元格读到的是序列号）；空串返回 nil 表示未填
func parseDate(s string) (*time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	for _, layout := range dateTextLayouts {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return &t, nil
		}
	}
	if serial, err := strconv.ParseFloat(s, 64); err == nil {
		if days := int(serial); days >= minExcelSerialDays && days <= maxExcelSerialDays {
			t := excelSerialBase.AddDate(0, 0, days)
			return &t, nil
		}
	}
	return nil, fmt.Errorf("无法识别的日期: %q（请使用 2024-06-01 格式）", s)
}

// parsePrice 金额解析：清千分位与货币符号后转 float；空串返回 0，负数报错
func parsePrice(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	s = strings.NewReplacer(",", "", "¥", "", "￥", "", "$", "").Replace(s)
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 {
		return 0, fmt.Errorf("无法识别的金额: %q", s)
	}
	return v, nil
}

// parseBool 是/否解析（加密软件纳管标记）；空串按否处理
func parseBool(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "否", "n", "no", "false", "0":
		return false, nil
	case "是", "y", "yes", "true", "1":
		return true, nil
	}
	return false, fmt.Errorf("无法识别的是/否值: %q", s)
}

// isEmptyRow 整行单元格 trim 后全空（模板尾部空行）
func isEmptyRow(cells []string) bool {
	for _, c := range cells {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}
