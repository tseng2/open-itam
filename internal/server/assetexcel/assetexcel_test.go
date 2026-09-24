package assetexcel

import (
	"bytes"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"itagent/internal/server/model"
)

// buildSheet 用 excelize 原生构造测试输入，模拟用户手填的文件
//（含原始数值单元格：日期序列号/数字状态码）；首行为表头，数据从第二行起
func buildSheet(t *testing.T, headers []string, rows [][]interface{}) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	for i, h := range headers {
		cell, err := excelize.CoordinatesToCellName(i+1, 1)
		if err != nil {
			t.Fatalf("cell name: %v", err)
		}
		if err := f.SetCellValue("Sheet1", cell, h); err != nil {
			t.Fatalf("set header: %v", err)
		}
	}
	for r, row := range rows {
		for c, v := range row {
			cell, err := excelize.CoordinatesToCellName(c+1, r+2)
			if err != nil {
				t.Fatalf("cell name: %v", err)
			}
			if err := f.SetCellValue("Sheet1", cell, v); err != nil {
				t.Fatalf("set cell: %v", err)
			}
		}
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		t.Fatalf("write buffer: %v", err)
	}
	return buf.Bytes()
}

// sparseRow 按"列下标(0起)→值"稀疏构造一行，其余列写空串
func sparseRow(pairs map[int]interface{}, width int) []interface{} {
	row := make([]interface{}, width)
	for i, v := range pairs {
		row[i] = v
	}
	return row
}

var fullHeaders = []string{
	"资产编码", "中心", "部门", "部门2", "存放位置", "资产负责人", "类别",
	"U8订单号", "状态", "品牌", "型号", "序列号", "CPU", "内存", "主硬盘",
	"从硬盘", "显卡", "MAC地址", "购入日期", "验收人", "保修期", "原值",
	"净值", "加密软件", "备注",
}

func TestParseFullRow(t *testing.T) {
	data := buildSheet(t, fullHeaders, [][]interface{}{
		{"IT-001", "研发中心", "研发部", "前端组", "3楼A-01", "张三", "笔记本电脑",
			"U8-2024-001", "使用中", "联想", "ThinkPad T14p", "PF3ABC12", "i7-13700H",
			"32G", "1T SSD", "", "", "AA:BB:CC:DD:EE:FF", "2024-06-01", "李四", "3年",
			8000.0, 6000.0, "是", "第一批采购"},
	})
	rows, errs, err := Parse(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("unexpected row errors: %+v", errs)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	r := rows[0]
	if r.PurchaseDate == nil || r.PurchaseDate.Format("2006-01-02") != "2024-06-01" {
		t.Fatalf("purchase date: %+v", r.PurchaseDate)
	}
	r.PurchaseDate = nil // 指针字段已单查，置空后做整体结构体比较
	want := Row{
		AssetTag:       "IT-001",
		CenterName:     "研发中心",
		DepartmentName: "研发部",
		DepartmentSub:  "前端组",
		Location:       "3楼A-01",
		ManagerName:    "张三",
		CategoryID:     model.AssetCategoryNotebook,
		CategoryName:   "笔记本电脑",
		U8OrderNo:      "U8-2024-001",
		Status:         model.AssetStatusInUse,
		Brand:          "联想",
		ModelName:      "ThinkPad T14p",
		SerialNumber:   "PF3ABC12",
		CPUName:        "i7-13700H",
		MemorySize:     "32G",
		MainDisk:       "1T SSD",
		MACAddress:     "AA:BB:CC:DD:EE:FF",
		Acceptor:       "李四",
		WarrantyPeriod: "3年",
		OriginalPrice:  8000,
		NetValue:       6000,
		SecEncrypted:   true,
		Remark:         "第一批采购",
	}
	if r != want {
		t.Fatalf("parsed row mismatch:\n got %+v\nwant %+v", r, want)
	}
}

func TestParseRawCellValues(t *testing.T) {
	// 用户真实填法：购入日期是 Excel 日期单元格（序列号 45444 = 2024-06-01）、
	// 状态填数字 20、原值带千分位文本、净值与加密软件留空
	data := buildSheet(t, fullHeaders, [][]interface{}{
		sparseRow(map[int]interface{}{
			0:  "IT-002",
			6:  "台式机",
			8:  20,
			18: 45444.0,
			21: "2,000.00",
		}, len(fullHeaders)),
	})
	rows, errs, err := Parse(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("unexpected row errors: %+v", errs)
	}
	r := rows[0]
	if r.Status != model.AssetStatusInUse {
		t.Fatalf("status: %+v", r.Status)
	}
	if r.CategoryID != model.AssetCategoryPC || r.CategoryName != "台式整机" {
		t.Fatalf("category: %d %q", r.CategoryID, r.CategoryName)
	}
	if r.PurchaseDate == nil || r.PurchaseDate.Format("2006-01-02") != "2024-06-01" {
		t.Fatalf("purchase date: %+v", r.PurchaseDate)
	}
	if r.OriginalPrice != 2000 {
		t.Fatalf("original price: %v", r.OriginalPrice)
	}
	if r.NetValue != 0 || r.SecEncrypted {
		t.Fatalf("net/sec: %v %v", r.NetValue, r.SecEncrypted)
	}
}

func TestParseHeaderMappingTolerant(t *testing.T) {
	// 列序打乱 + 未知表头（领用人/公司）忽略；缺可选列（原值）默认 0
	headers := []string{"净值", "资产编码", "类别", "状态", "公司"}
	data := buildSheet(t, headers, [][]interface{}{
		{"500", "IT-003", "显示器", "维修中", "某公司"},
	})
	rows, errs, err := Parse(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(errs) != 0 || len(rows) != 1 {
		t.Fatalf("rows=%d errs=%+v err=%v", len(rows), errs, err)
	}
	r := rows[0]
	if r.AssetTag != "IT-003" || r.NetValue != 500 ||
		r.CategoryID != model.AssetCategoryMonitor || r.Status != model.AssetStatusRepair {
		t.Fatalf("unexpected row: %+v", r)
	}
}

func TestParseRowErrors(t *testing.T) {
	data := buildSheet(t, fullHeaders, [][]interface{}{
		sparseRow(map[int]interface{}{1: "某中心"}, len(fullHeaders)),                        // 行2：编码为空
		sparseRow(map[int]interface{}{0: "IT-DUP", 6: "台式机"}, len(fullHeaders)),          // 行3：合法
		sparseRow(map[int]interface{}{0: "IT-DUP", 6: "台式机"}, len(fullHeaders)),          // 行4：文件内重复
		sparseRow(map[int]interface{}{0: "IT-CAT", 6: "平板电脑"}, len(fullHeaders)),         // 行5：类别未识别
		sparseRow(map[int]interface{}{0: "IT-ST", 6: "台式机", 8: "报废中"}, len(fullHeaders)),   // 行6：状态未识别
		sparseRow(map[int]interface{}{0: "IT-DT", 6: "台式机", 18: "2024-13-01"}, len(fullHeaders)), // 行7：日期非法
		sparseRow(map[int]interface{}{0: "IT-NEG", 6: "台式机", 21: -5.0}, len(fullHeaders)),  // 行8：金额为负
		sparseRow(map[int]interface{}{0: "IT-BOOL", 6: "台式机", 23: "也许"}, len(fullHeaders)), // 行9：是/否未识别
		sparseRow(map[int]interface{}{0: "IT-OK", 6: "其他"}, len(fullHeaders)),             // 行10：合法（全默认值）
		sparseRow(map[int]interface{}{}, len(fullHeaders)),                                  // 行11：整行空 → 跳过
	})
	rows, errs, err := Parse(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 valid rows (IT-DUP/IT-OK), got %d: %+v", len(rows), rows)
	}
	wantRows := []int{2, 4, 5, 6, 7, 8, 9}
	if len(errs) != len(wantRows) {
		t.Fatalf("want %d row errors, got %d: %+v", len(wantRows), len(errs), errs)
	}
	for i, want := range wantRows {
		if errs[i].Row != want {
			t.Fatalf("errs[%d].Row = %d, want %d (%+v)", i, errs[i].Row, want, errs[i])
		}
	}
	keywords := []string{"资产编码为空", "重复", "类别", "状态", "日期", "金额", "无法识别的是"}
	for i, kw := range keywords {
		if !strings.Contains(errs[i].Reason, kw) {
			t.Fatalf("errs[%d].Reason = %q, want contains %q", i, errs[i].Reason, kw)
		}
	}
}

func TestParseFileLevelErrors(t *testing.T) {
	if _, _, err := Parse([]byte("not an excel file")); err == nil {
		t.Fatal("junk bytes should fail")
	}
	// 仅表头无数据行
	if _, _, err := Parse(buildSheet(t, fullHeaders, nil)); err == nil {
		t.Fatal("header-only sheet should fail")
	}
	// 缺必填表头
	_, _, err := Parse(buildSheet(t, []string{"中心", "类别"}, [][]interface{}{{"a", "台式机"}}))
	if err == nil || !strings.Contains(err.Error(), "资产编码") {
		t.Fatalf("missing required header should fail with 资产编码, got %v", err)
	}
	// 超行数上限
	big := make([][]interface{}, MaxImportRows+1)
	for i := range big {
		big[i] = []interface{}{"IT-ROW-" + strconv.Itoa(i)}
	}
	if _, _, err := Parse(buildSheet(t, []string{"资产编码"}, big)); err == nil ||
		!strings.Contains(err.Error(), "最多") {
		t.Fatalf("row overflow should fail, got %v", err)
	}
}

func TestParseDateTextLayouts(t *testing.T) {
	headers := []string{"资产编码", "类别", "购入日期"}
	data := buildSheet(t, headers, [][]interface{}{
		{"IT-D1", "台式机", "2024/6/1"},
		{"IT-D2", "台式机", "2024.06.01"},
		{"IT-D3", "台式机", "2024-6-1"},
	})
	rows, errs, err := Parse(data)
	if err != nil || len(errs) != 0 || len(rows) != 3 {
		t.Fatalf("rows=%d errs=%+v err=%v", len(rows), errs, err)
	}
	for _, r := range rows {
		if r.PurchaseDate == nil || r.PurchaseDate.Format("2006-01-02") != "2024-06-01" {
			t.Fatalf("date layout parse failed: %+v", r.PurchaseDate)
		}
	}
}

func TestExportContent(t *testing.T) {
	date := time.Date(2024, 6, 1, 0, 0, 0, 0, time.Local)
	rows := []Row{
		{
			AssetTag:      "IT-001",
			CategoryID:    model.AssetCategoryNotebook,
			Status:        model.AssetStatusInUse,
			Brand:         "联想",
			PurchaseDate:  &date,
			OriginalPrice: 8000,
			NetValue:      6000,
			SecEncrypted:  true,
			Assignee:      "王五",
			CompanyName:   "某公司",
		},
		{AssetTag: "IT-002", CategoryID: model.AssetCategoryPC, Status: model.AssetStatusScrapped},
	}
	data, err := Export(rows)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("open exported file: %v", err)
	}
	defer f.Close()
	if sheets := f.GetSheetList(); len(sheets) == 0 || sheets[0] != SheetNameAssets {
		t.Fatalf("sheet name: %+v", sheets)
	}
	raw := excelize.Options{RawCellValue: true}
	cell := func(axis string) string {
		v, err := f.GetCellValue(SheetNameAssets, axis, raw)
		if err != nil {
			t.Fatalf("get cell %s: %v", axis, err)
		}
		return v
	}
	for _, tc := range []struct{ axis, want string }{
		{"A1", "资产编码"}, {"G1", "类别"}, {"I1", "状态"}, {"S1", "购入日期"},
		{"Z1", "领用人"}, {"AA1", "公司"},
		{"A2", "IT-001"}, {"G2", "笔记本电脑"}, {"I2", "使用中"},
		{"S2", "2024-06-01"}, {"V2", "8000"}, {"X2", "是"}, {"Z2", "王五"}, {"AA2", "某公司"},
		{"A3", "IT-002"}, {"I3", "已报废"}, {"X3", "否"},
	} {
		if got := cell(tc.axis); got != tc.want {
			t.Errorf("cell %s = %q, want %q", tc.axis, got, tc.want)
		}
	}
}

func TestTemplateAndRoundTrip(t *testing.T) {
	tpl, err := Template()
	if err != nil {
		t.Fatalf("template: %v", err)
	}
	rows, errs, err := Parse(tpl)
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}
	if len(errs) != 0 || len(rows) != 1 {
		t.Fatalf("template example row should parse: rows=%d errs=%+v", len(rows), errs)
	}
	r := rows[0]
	if r.AssetTag == "" || r.CategoryID == 0 || r.Status != model.AssetStatusStock ||
		r.OriginalPrice <= 0 || !r.SecEncrypted || !strings.Contains(r.Remark, "示例") {
		t.Fatalf("unexpected template example row: %+v", r)
	}

	// 导出 → 回导闭环：导出文件（含领用人/公司富化列）直接可被导入，
	// 富化列被忽略，业务字段逐项还原
	date := time.Date(2024, 6, 1, 0, 0, 0, 0, time.Local)
	src := []Row{{
		AssetTag:      "IT-RT-1",
		CenterName:    "研发中心",
		CategoryID:    model.AssetCategoryMonitor,
		CategoryName:  "显示器",
		Status:        model.AssetStatusInUse,
		Brand:         "DELL",
		PurchaseDate:  &date,
		OriginalPrice: 1200.5,
		NetValue:      900,
		SecEncrypted:  true,
		Remark:        "闭环",
		Assignee:      "张三",
		CompanyName:   "某公司",
	}}
	data, err := Export(src)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	back, errs, err := Parse(data)
	if err != nil || len(errs) != 0 || len(back) != 1 {
		t.Fatalf("round-trip parse: rows=%d errs=%+v err=%v", len(back), errs, err)
	}
	got := back[0]
	want := src[0]
	want.Assignee, want.CompanyName = "", "" // 富化列不参与导入
	// 指针字段先单独校验（*time.Time 指针不等值），再置空做整体比较
	if got.PurchaseDate == nil || got.PurchaseDate.Format("2006-01-02 15:04:05") != "2024-06-01 00:00:00" {
		t.Fatalf("round-trip purchase date: %+v", got.PurchaseDate)
	}
	got.PurchaseDate, want.PurchaseDate = nil, nil
	if got != want {
		t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", got, want)
	}
}

func TestAssetStatusText(t *testing.T) {
	for status, want := range map[int]string{
		model.AssetStatusStock:    "库存中",
		model.AssetStatusInUse:   "使用中",
		model.AssetStatusRepair:  "维修中",
		model.AssetStatusScrapped: "已报废",
		99:                       "未知",
	} {
		if got := AssetStatusText(status); got != want {
			t.Errorf("AssetStatusText(%d) = %q, want %q", status, got, want)
		}
	}
}
