package model

import "testing"

func TestAssetCategoryIDByName(t *testing.T) {
	cases := []struct {
		name     string
		wantID   int64
		wantName string
		wantOK   bool
	}{
		// 规范名
		{"台式整机", AssetCategoryPC, "台式整机", true},
		{"笔记本电脑", AssetCategoryNotebook, "笔记本电脑", true},
		{"显示器", AssetCategoryMonitor, "显示器", true},
		{"外设及其他", AssetCategoryPeripheral, "外设及其他", true},
		// 常见别名与容错（trim / 英文大小写归一）
		{"台式机", AssetCategoryPC, "台式整机", true},
		{" 笔记本 ", AssetCategoryNotebook, "笔记本电脑", true},
		{"pc", AssetCategoryPC, "台式整机", true},
		{"Laptop", AssetCategoryNotebook, "笔记本电脑", true},
		{"其他", AssetCategoryPeripheral, "外设及其他", true},
		// 未识别
		{"", 0, "", false},
		{"平板电脑", 0, "", false},
		{"台式", 0, "", false}, // 禁止模糊包含：类别是台账硬字段
	}
	for _, tc := range cases {
		id, canonical, ok := AssetCategoryIDByName(tc.name)
		if ok != tc.wantOK {
			t.Errorf("AssetCategoryIDByName(%q) ok=%v, want %v", tc.name, ok, tc.wantOK)
			continue
		}
		if ok && (id != tc.wantID || canonical != tc.wantName) {
			t.Errorf("AssetCategoryIDByName(%q) = (%d, %q), want (%d, %q)",
				tc.name, id, canonical, tc.wantID, tc.wantName)
		}
	}
}
