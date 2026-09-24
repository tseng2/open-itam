// Package label 渲染资产标签 PDF（阶段五 P0-β 扫码盘点链路的打印端）。
//
// 选型约束：零 CGO（服务端容器与 Agent 容错规范同源）——go-pdf/fpdf 与
// boombuler/barcode 均为纯 Go 实现。fpdf 核心字体仅 Latin-1，CJK 字段
// 不进标签（识别用途不需要：资产编码/SN/QR 均为 ASCII），中文信息在
// 扫码后的移动页展示；非可打印 ASCII 字段整行降级略过
package label

import (
	"bytes"
	"fmt"
	"image/png"

	"github.com/boombuler/barcode"
	"github.com/boombuler/barcode/qr"
	"github.com/go-pdf/fpdf"
)

// 版面：A4 纵向 3 列 × 7 行，65×35mm 单枚标签 + 网格间隙，
// 避免贴到纸张物理边缘被打印机裁掉
const (
	labelWidth   = 65.0 // mm
	labelHeight  = 35.0 // mm
	sheetMarginX = 7.5  // mm（3×65=195，A4 210，两侧各 7.5）
	sheetMarginY = 15.0 // mm
	gapX         = 0.0  // 列间距（65×3=195 已含余白）
	gapY         = 1.0  // 行间距
	qrSize       = 24.0 // mm，QR 印刷尺寸
	// qrPixels QR 栅格化分辨率：~300dpi（24mm ≈ 283px），保证打印锐利
	qrPixels = 288
)

// labelsPerSheet 每页标签数（3×7）
const labelsPerSheet = 21

// Label 单枚标签的渲染数据；QRText 为二维码内容（标签印
// `${origin}/#/a/${asset_tag}`，扫码直达移动核对页；为空时回退为 Tag）
type Label struct {
	Tag      string
	Category string
	Brand    string
	Model    string
	SN       string
	Location string
	QRText   string
}

// RenderLabelsPDF 渲染标签为 PDF 字节流（A4 分页，每页 3×7 枚）
func RenderLabelsPDF(labels []Label) ([]byte, error) {
	if len(labels) == 0 {
		return nil, fmt.Errorf("labels required")
	}

	pdf := fpdf.New("P", "mm", "A4", "")
	for i, lb := range labels {
		if i%labelsPerSheet == 0 {
			pdf.AddPage()
		}
		pos := i % labelsPerSheet
		drawLabel(pdf, lb, pos%3, pos/3)
	}

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("render pdf: %w", err)
	}
	return buf.Bytes(), nil
}

// drawLabel 绘制单枚标签：左列文本（资产编码大字 + 规格行），右侧 QR
func drawLabel(pdf *fpdf.Fpdf, lb Label, col, row int) {
	x := sheetMarginX + float64(col)*(labelWidth+gapX)
	y := sheetMarginY + float64(row)*(labelHeight+gapY)

	pdf.Rect(x, y, labelWidth, labelHeight, "D")

	// 资产编码是核对主键，用最大字号；规格行仅保留可打印 ASCII
	pdf.SetFont("helvetica", "B", 13)
	pdf.SetXY(x+2, y+2.5)
	pdf.CellFormat(labelWidth-qrSize-5, 7, asciiOnly(lb.Tag), "", 0, "L", false, 0, "")

	pdf.SetFont("helvetica", "", 7.5)
	lines := [][]string{
		{lb.Brand, lb.Model},
		{lb.Category},
		{lb.SN},
		{lb.Location},
	}
	ly := y + 11.5
	for _, parts := range lines {
		text := asciiOnly(joinFields(parts))
		if text == "" {
			continue
		}
		pdf.SetXY(x+2, ly)
		pdf.CellFormat(labelWidth-qrSize-5, 5, text, "", 0, "L", false, 0, "")
		ly += 5
	}

	drawQR(pdf, lb, x, y)
}

// drawQR 渲染右侧二维码：boombuler 生成后按打印分辨率栅格化再嵌入
func drawQR(pdf *fpdf.Fpdf, lb Label, x, y float64) {
	payload := lb.QRText
	if payload == "" {
		payload = lb.Tag
	}
	if payload == "" {
		return
	}
	code, err := qr.Encode(payload, qr.M, qr.Auto)
	if err != nil {
		// 单枚二维码失败不拖垮整批：该枚标签保留文本区，跳过 QR
		return
	}
	scaled, err := barcode.Scale(code, qrPixels, qrPixels)
	if err != nil {
		return
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, scaled); err != nil {
		return
	}
	name := fmt.Sprintf("qr-%d", int(x*100)+int(y)*7)
	opts := fpdf.ImageOptions{ImageType: "PNG"}
	if err := pdf.RegisterImageOptionsReader(name, opts, &buf); err != nil {
		return
	}
	pdf.ImageOptions(name, x+labelWidth-qrSize-2, y+(labelHeight-qrSize)/2, qrSize, qrSize, false, opts, 0, "")
}

// asciiOnly 过滤为可打印 ASCII：fpdf 核心字体仅 Latin-1，
// CJK 字段不进标签（扫码后移动页展示中文详情），不可打印字符整串略过
func asciiOnly(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return ""
		}
	}
	return s
}

// joinFields 拼接规格行（如 "DELL Latitude 5440"），过滤空段
func joinFields(parts []string) string {
	out := ""
	for _, p := range parts {
		if p == "" {
			continue
		}
		if out != "" {
			out += " "
		}
		out += p
	}
	return out
}
