package softwareaudit

import (
	"testing"

	"itagent/internal/server/model"
)

func pool(id int64, name string, licenseID *int64) model.SoftwarePool {
	return model.SoftwarePool{CompanyID: 1, Name: name, LicenseID: licenseID}
}

// BaseModel.ID 通过嵌入结构填充
func poolWithID(id int64, name string, licenseID *int64) model.SoftwarePool {
	p := pool(id, name, licenseID)
	p.ID = id
	return p
}

func intPtr(v int64) *int64 { return &v }

func TestMatchPoolExactBeatsContains(t *testing.T) {
	pools := []model.SoftwarePool{
		poolWithID(1, "Microsoft 365", nil),
		poolWithID(2, "Office", nil),
	}
	// 精确命中优先：即使另一个池名也被包含
	if got, ok := MatchPool("Microsoft 365", pools); !ok || got.ID != 1 {
		t.Fatalf("exact match failed: %+v ok=%v", got, ok)
	}
	// 包含命中：采集名包含池名
	if got, ok := MatchPool("Microsoft 365 Apps for enterprise - zh-cn", pools); !ok || got.ID != 1 {
		t.Fatalf("contains match failed: %+v ok=%v", got, ok)
	}
	// 无命中
	if _, ok := MatchPool("AutoCAD 2026", pools); ok {
		t.Fatal("unmatched software must not hit pool")
	}
}

func TestMatchPoolLongestContainsWins(t *testing.T) {
	pools := []model.SoftwarePool{
		poolWithID(1, "Office", nil),
		poolWithID(2, "Microsoft Office", nil),
	}
	// 「Microsoft Office 专业增强版」同时包含两个池名，取更具体的
	if got, ok := MatchPool("Microsoft Office 专业增强版 2024", pools); !ok || got.ID != 2 {
		t.Fatalf("longest contains should win: %+v ok=%v", got, ok)
	}
	// 仅包含短池名时归短池
	if got, ok := MatchPool("WPS Office", pools); !ok || got.ID != 1 {
		t.Fatalf("short contains should hit office: %+v ok=%v", got, ok)
	}
}

func TestIsSystemSoftware(t *testing.T) {
	systems := []string{
		"Microsoft Visual C++ 2015-2019 Redistributable (x64)",
		"Microsoft .NET Runtime - 8.0.2 (x64)",
		"Intel(R) Management Engine Components",
		"NVIDIA GeForce Experience",
		"Microsoft Edge WebView2 Runtime",
		"Google Chrome",
		"企业微信",
		"Windows Software Development Kit",
	}
	for _, name := range systems {
		if !IsSystemSoftware(name) {
			t.Fatalf("system software not recognized: %q", name)
		}
	}
	commercial := []string{
		"IntelliJ IDEA 2026.1",           // intel 前缀陷阱：不能被 intel(r) 之外的裸 intel 误伤
		"Microsoft 365 Apps for enterprise",
		"WinRAR 7.0 (64-bit)",
		"WPS Office 2023",
		"TeamViewer 15",
		"Adobe Acrobat DC",
		"AutoCAD 2026",
	}
	for _, name := range commercial {
		if IsSystemSoftware(name) {
			t.Fatalf("commercial software must not be whitelisted: %q", name)
		}
	}
}

func TestBuildComplianceOverusedAndUnmanaged(t *testing.T) {
	pools := []model.SoftwarePool{
		poolWithID(1, "Microsoft 365", intPtr(10)), // 席位 10
		poolWithID(2, "WPS Office", nil),           // 未挂许可：不限席位
	}
	licenses := map[int64]model.License{10: {Name: "M365 商业高级版", TotalSeats: 3}}
	installs := []SoftwareInstall{
		{Name: "Microsoft 365 Apps for enterprise", Installs: 5}, // 命中池 1：5 > 3 超用
		{Name: "WPS Office 2023", Installs: 8},                   // 命中池 2：不限席位
		{Name: "AutoCAD 2026", Installs: 2},                      // 未受控商业软件
		{Name: "Microsoft Visual C++ 2015 Redistributable", Installs: 40}, // 白名单排除
	}
	report := BuildCompliance(pools, licenses, installs)

	if report.Summary.PoolCount != 2 || report.Summary.OverusedCount != 1 {
		t.Fatalf("summary mismatch: %+v", report.Summary)
	}
	if report.Summary.ManagedInstalls != 13 {
		t.Fatalf("managed installs = %d, want 13", report.Summary.ManagedInstalls)
	}
	if report.Summary.UnmanagedCount != 1 {
		t.Fatalf("unmanaged count = %d, want 1 (AutoCAD)", report.Summary.UnmanagedCount)
	}
	if len(report.PoolItems) != 2 {
		t.Fatalf("pool items = %d", len(report.PoolItems))
	}
	m365 := report.PoolItems[0]
	if !m365.Overused || m365.Installs != 5 || m365.TotalSeats != 3 || m365.LicenseName != "M365 商业高级版" {
		t.Fatalf("overused pool item wrong: %+v", m365)
	}
	wps := report.PoolItems[1]
	if wps.Overused || wps.Installs != 8 {
		t.Fatalf("wps pool item wrong: %+v", wps)
	}
	if len(report.Unmanaged) != 1 || report.Unmanaged[0].Name != "AutoCAD 2026" || report.Unmanaged[0].Installs != 2 {
		t.Fatalf("unmanaged rows wrong: %+v", report.Unmanaged)
	}
}

func TestBuildComplianceUnmanagedSortedDesc(t *testing.T) {
	pools := []model.SoftwarePool{}
	report := BuildCompliance(pools, nil, []SoftwareInstall{
		{Name: "AutoCAD 2026", Installs: 2},
		{Name: "Adobe Acrobat DC", Installs: 9},
		{Name: "TeamViewer 15", Installs: 9},
	})
	if len(report.Unmanaged) != 3 {
		t.Fatalf("unmanaged = %d", len(report.Unmanaged))
	}
	// 同安装数按名称字典序稳定
	if report.Unmanaged[0].Name != "Adobe Acrobat DC" || report.Unmanaged[1].Name != "TeamViewer 15" || report.Unmanaged[2].Name != "AutoCAD 2026" {
		t.Fatalf("unmanaged order wrong: %+v", report.Unmanaged)
	}
	if report.Summary.PoolCount != 0 || report.Summary.UnmanagedCount != 3 {
		t.Fatalf("empty pool summary wrong: %+v", report.Summary)
	}
}

func TestBuildComplianceMissingLicenseMeansUnlimited(t *testing.T) {
	// 池项挂的许可已删除：licenses 缺键 → 不限席位处理，不误报超用
	pools := []model.SoftwarePool{poolWithID(1, "AutoCAD", intPtr(99))}
	report := BuildCompliance(pools, map[int64]model.License{}, []SoftwareInstall{
		{Name: "AutoCAD 2026", Installs: 50},
	})
	if report.PoolItems[0].Overused || report.PoolItems[0].TotalSeats != 0 {
		t.Fatalf("missing license must mean unlimited: %+v", report.PoolItems[0])
	}
}
