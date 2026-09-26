// Package softwareaudit 阶段三软件合规比对：受控软件池 × Agent 采集的
// 终端软件清单，产出合规报表（识别超用与未受控商业软件）与超用提醒。
// 匹配/白名单/超用判定为纯函数（单源勿重写），扫描与冷却去重复用
// webhook/licensealert 引擎先例（同表 webhook_alert_states 独立键）
package softwareaudit

import (
	"context"
	"sort"
	"strings"

	"itagent/internal/server/model"

	"gorm.io/gorm"
)

// SoftwareInstall 终端软件安装聚合行：Installs 为去重终端数
//（同名不同版本按名归并；同终端多版本只计一次）
type SoftwareInstall struct {
	Name     string `json:"name"`
	Installs int64  `json:"installs"`
}

// PoolCompliance 受控池项合规视图：超用 = 挂接许可且安装数 > 席位
//（TotalSeats 0 = 不限席位，永不超用）
type PoolCompliance struct {
	PoolID      int64  `json:"pool_id"`
	Name        string `json:"name"`
	Vendor      string `json:"vendor"`
	Category    string `json:"category"`
	LicenseID   int64  `json:"license_id"`
	LicenseName string `json:"license_name"`
	TotalSeats  int    `json:"total_seats"`
	Installs    int64  `json:"installs"`
	Overused    bool   `json:"overused"`
}

// UnmanagedSoftware 未受控商业软件：不在任何池项且不命中系统组件白名单
type UnmanagedSoftware struct {
	Name     string `json:"name"`
	Installs int64  `json:"installs"`
}

// ComplianceSummary 合规汇总卡口径
type ComplianceSummary struct {
	PoolCount       int   `json:"pool_count"`        // 受控池项数
	OverusedCount   int   `json:"overused_count"`    // 超用池项数
	ManagedInstalls int64 `json:"managed_installs"`  // 命中池项的安装终端数合计
	UnmanagedCount  int64 `json:"unmanaged_count"`   // 未受控商业软件种数
}

// ComplianceReport 合规报表：池项全量 + 未受控清单（已按安装数降序）
type ComplianceReport struct {
	Summary   ComplianceSummary    `json:"summary"`
	PoolItems []PoolCompliance     `json:"pool_items"`
	Unmanaged []UnmanagedSoftware  `json:"unmanaged"`
}

// MatchPool 池项匹配（合规比对的唯一口径）：采集名与池名精确相等优先；
// 无精确时按包含匹配（采集名包含池名——注册表 DisplayName 变体多，
// 池里写「Microsoft 365」即可命中「Microsoft 365 Apps for enterprise」），
// 命中多个取池名最长者（最具体优先）。一个采集名只归一个池项
func MatchPool(softwareName string, pools []model.SoftwarePool) (model.SoftwarePool, bool) {
	for _, p := range pools {
		if p.Name == softwareName {
			return p, true
		}
	}
	best := -1
	for i := range pools {
		if strings.Contains(softwareName, pools[i].Name) {
			if best < 0 || len(pools[i].Name) > len(pools[best].Name) {
				best = i
			}
		}
	}
	if best >= 0 {
		return pools[best], true
	}
	return model.SoftwarePool{}, false
}

// systemSoftwareKeywords 内置系统组件/运行库/驱动降噪白名单（小写，
// 大小写不敏感包含匹配）。只收系统与明确免费的常装软件；商业软件
//（WinRAR / WPS / TeamViewer / IntelliJ 等）一律不收——宁误报给管理员
// 甄别，不漏报盗版嫌疑。注意关键词别误伤商业软件（如不能裸用 intel，
// 会把 IntelliJ IDEA 误判系统组件）。演进为可配置时以池语义替代
var systemSoftwareKeywords = []string{
	"microsoft visual c++", ".net", "redistributable", "driver",
	"intel(r)", "intel graphics", "intel management", "nvidia", "realtek",
	"windows", "microsoft edge", "google chrome", "mozilla firefox",
	"7-zip", "onedrive", "visual studio code", "python", "powershell",
	"wechat", "微信", "企业微信", "wxwork", "腾讯会议",
}

// IsSystemSoftware 系统组件白名单命中判定：命中的软件不进未受控清单
func IsSystemSoftware(name string) bool {
	ln := strings.ToLower(name)
	for _, kw := range systemSoftwareKeywords {
		if strings.Contains(ln, kw) {
			return true
		}
	}
	return false
}

// BuildCompliance 纯函数比对：受控池 × 安装聚合 → 合规报表。
// licenses 键为许可 ID（池项未挂接或许可已删时按不限席位处理）
func BuildCompliance(pools []model.SoftwarePool, licenses map[int64]model.License, installs []SoftwareInstall) ComplianceReport {
	report := ComplianceReport{PoolItems: make([]PoolCompliance, 0, len(pools))}
	installByPool := make(map[int64]int64, len(pools))
	for _, inst := range installs {
		if pool, ok := MatchPool(inst.Name, pools); ok {
			installByPool[pool.ID] += inst.Installs
		} else if !IsSystemSoftware(inst.Name) {
			report.Unmanaged = append(report.Unmanaged, UnmanagedSoftware{Name: inst.Name, Installs: inst.Installs})
		}
	}
	for _, p := range pools {
		item := PoolCompliance{
			PoolID: p.ID, Name: p.Name, Vendor: p.Vendor, Category: p.Category,
			Installs: installByPool[p.ID],
		}
		if p.LicenseID != nil {
			item.LicenseID = *p.LicenseID
			if lic, ok := licenses[*p.LicenseID]; ok {
				item.LicenseName = lic.Name
				item.TotalSeats = lic.TotalSeats
			}
		}
		item.Overused = item.TotalSeats > 0 && item.Installs > int64(item.TotalSeats)
		report.PoolItems = append(report.PoolItems, item)
	}
	sort.Slice(report.Unmanaged, func(i, j int) bool {
		if report.Unmanaged[i].Installs != report.Unmanaged[j].Installs {
			return report.Unmanaged[i].Installs > report.Unmanaged[j].Installs
		}
		return report.Unmanaged[i].Name < report.Unmanaged[j].Name
	})
	report.Summary.PoolCount = len(pools)
	for _, it := range report.PoolItems {
		if it.Overused {
			report.Summary.OverusedCount++
		}
		report.Summary.ManagedInstalls += it.Installs
	}
	report.Summary.UnmanagedCount = int64(len(report.Unmanaged))
	return report
}

// ListSoftwareInstalls 按公司聚合终端软件安装（去重终端数，安装数降序），
// 报表 API 与合规引擎共用此查询（口径单源）
func ListSoftwareInstalls(ctx context.Context, db *gorm.DB, companyID int64) ([]SoftwareInstall, error) {
	var rows []SoftwareInstall
	err := db.WithContext(ctx).Model(&model.DeviceSoftware{}).
		Select("name, COUNT(DISTINCT device_id) AS installs").
		Where("company_id = ?", companyID).
		Group("name").
		Order("installs DESC, name ASC").
		Find(&rows).Error
	return rows, err
}
