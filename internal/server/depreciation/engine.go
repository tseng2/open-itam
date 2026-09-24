package depreciation

import (
	"context"
	"log"
	"math"
	"time"

	"itagent/internal/server/model"

	"gorm.io/gorm"
)

// DefaultScanInterval 定时刷净值周期：净值只在整月边界变化，小时级扫描
// 保证跨月当天内即完成刷新；规则/台账编辑另有手动重算入口，无需更密
const DefaultScanInterval = time.Hour

// netValueDiffTol 净值写入阈值（分）：低于一分的差异视为浮点噪声不落库，
// 避免每轮扫描空转写库
const netValueDiffTol = 0.005

// Engine 折旧引擎（阶段五 P0-β）：定时按规则刷资产净值。
// 单进程 goroutine + time.Ticker，无需分布式锁；计算核心复用本包纯函数，
// 与 API 手动重算共用同一 ScanOnce 实现
type Engine struct {
	db  *gorm.DB
	now func() time.Time // 注入时钟，测试跨月边界用
}

func NewEngine(db *gorm.DB) *Engine {
	return &Engine{db: db, now: time.Now}
}

// ScanOnce 执行一轮净值刷新，返回本轮实际更新的资产数。
// 规则停用/资产缺购置日期或原值非法（≤0）一律跳过，不视为错误；
// 单资产规则解析失败只跳过该规则名下资产（规则在入口已校验，此处防御异常数据）
func (e *Engine) ScanOnce(ctx context.Context) (int, error) {
	var rules []model.DepreciationRule
	if err := e.db.WithContext(ctx).Where("enabled = ?", true).Find(&rules).Error; err != nil {
		return 0, err
	}
	if len(rules) == 0 {
		return 0, nil
	}
	// 规则含公司边界：资产与规则必须同公司，悬空跨公司引用不刷
	type ruleSpec struct {
		companyID int64
		spec      Spec
	}
	specs := make(map[int64]ruleSpec, len(rules))
	for _, r := range rules {
		stages, err := ParseStages(r.Stages)
		if err != nil {
			log.Printf("[depreciation] skip rule %d %q: %v", r.ID, r.Name, err)
			continue
		}
		specs[r.ID] = ruleSpec{
			companyID: r.CompanyID,
			spec: Spec{
				TotalMonths: r.Months,
				FloorType:   r.FloorType,
				FloorVal:    r.FloorVal,
				Stages:      stages,
			},
		}
	}
	if len(specs) == 0 {
		return 0, nil
	}

	var assets []model.Asset
	if err := e.db.WithContext(ctx).
		Select("id", "company_id", "depreciation_id", "purchase_date", "original_price", "net_value").
		Where("depreciation_id IS NOT NULL").
		Find(&assets).Error; err != nil {
		return 0, err
	}

	now := e.now().UTC()
	type netUpdate struct {
		id    int64
		value float64
	}
	updates := make([]netUpdate, 0, len(assets))
	for _, a := range assets {
		if a.PurchaseDate == nil || a.OriginalPrice <= 0 {
			continue
		}
		rs, ok := specs[*a.DepreciationID]
		if !ok || rs.companyID != a.CompanyID {
			continue // 规则不存在（悬空引用/停用）或跨公司：冻结现值不动
		}
		spec := rs.spec
		spec.OriginalPrice = a.OriginalPrice
		spec.PurchaseDate = a.PurchaseDate.UTC()
		value := NetValue(spec, now)
		if math.Abs(value-a.NetValue) < netValueDiffTol {
			continue
		}
		updates = append(updates, netUpdate{id: a.ID, value: value})
	}
	if len(updates) == 0 {
		return 0, nil
	}

	err := e.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, u := range updates {
			if err := tx.Model(&model.Asset{}).Where("id = ?", u.id).
				Update("net_value", u.value).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return len(updates), nil
}

// Run 定时刷净值主循环：随服务进程常驻，ctx 取消即退出（部署重启即停）
func (e *Engine) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultScanInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := e.ScanOnce(ctx)
			if err != nil {
				log.Printf("[depreciation] scan round failed: %v", err)
				continue
			}
			if n > 0 {
				log.Printf("[depreciation] refreshed net value for %d asset(s)", n)
			}
		}
	}
}
