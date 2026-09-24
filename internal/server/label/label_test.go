package label

import (
	"bytes"
	"testing"
)

func countPages(out []byte) int {
	// fpdf 同时写 "/Type /Page" 与父对象 "/Type /Pages"，后者包含前缀，需差集
	return bytes.Count(out, []byte("/Type /Page")) - bytes.Count(out, []byte("/Type /Pages"))
}

func TestRenderLabelsPDFMagicAndPages(t *testing.T) {
	labels := []Label{
		{Tag: "AST-0001", Category: "Laptop", Brand: "DELL", Model: "Latitude 5440", SN: "SN-001", Location: "SZ-Office", QRText: "http://itam.local/#/a/AST-0001"},
		{Tag: "AST-0002", Category: "Monitor", Brand: "Dell", Model: "U2723QE", SN: "SN-002", Location: "SZ-Office", QRText: "http://itam.local/#/a/AST-0002"},
	}
	out, err := RenderLabelsPDF(labels)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !bytes.HasPrefix(out, []byte("%PDF")) {
		t.Fatalf("not a PDF document: %q", out[:10])
	}
	if len(out) < 2000 {
		t.Fatalf("PDF unexpectedly small: %d bytes", len(out))
	}
	// 2 枚标签应只有 1 页
	if n := countPages(out); n != 1 {
		t.Fatalf("expected 1 page object, got %d", n)
	}
}

func TestRenderLabelsPDFPagination(t *testing.T) {
	labels := make([]Label, 0, labelsPerSheet+1)
	for i := 0; i < labelsPerSheet+1; i++ {
		labels = append(labels, Label{Tag: "AST-1000", SN: "SN", QRText: "AST-1000"})
	}
	out, err := RenderLabelsPDF(labels)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if n := countPages(out); n != 2 {
		t.Fatalf("expected 2 page objects for %d labels, got %d", len(labels), n)
	}
}

func TestRenderLabelsPDFToleratesCJKAndEmptyFields(t *testing.T) {
	// CJK 字段不进标签（核心字体仅 Latin-1）：不得报错，渲染降级为可打印字段
	labels := []Label{
		{Tag: "AST-0003", Category: "笔记本", Brand: "联想", Model: "ThinkPad T14", SN: "SN-003", Location: "深圳办公室", QRText: "AST-0003"},
		{}, // 全空标签也不允许 panic
	}
	out, err := RenderLabelsPDF(labels)
	if err != nil {
		t.Fatalf("render with CJK fields: %v", err)
	}
	if !bytes.HasPrefix(out, []byte("%PDF")) {
		t.Fatal("not a PDF document")
	}
}

func TestRenderLabelsPDFRejectsEmptyBatch(t *testing.T) {
	if _, err := RenderLabelsPDF(nil); err == nil {
		t.Fatal("empty batch must be rejected")
	}
}
