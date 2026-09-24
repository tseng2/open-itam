// Package depreciation 折旧规则引擎（阶段五 P0-β，CIYO 对标三件套第三项）。
// 计算核心是纯函数（与 model/asset 解耦，引擎与 API 共用同一实现）：
// stages 按时间顺序分段累计折旧比例，段内按整月线性折算；
// stages 为空时按总月数直线折旧；floor 残值下限兜底（amount 绝对额 / percent 残值率）
package depreciation

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
)

// 折旧规则常量：单位与残值类型收口，禁止散落字符串字面量
const (
	UnitMonth = "MONTH"
	UnitYear  = "YEAR"

	FloorAmount  = "amount"  // 残值以绝对金额表示
	FloorPercent = "percent" // 残值以原值百分比表示（残值率）
)

// Stage 一段折旧区间：Period 个 Unit 期间内累计折旧原值的 Ratio 比例。
// 阶段按购置时长顺序消费（前一段走完才进入下一段），段内按整月线性折算
type Stage struct {
	Period int     `json:"period"`
	Unit   string  `json:"unit"`
	Ratio  float64 `json:"ratio"`
}

// Spec 单资产折旧计算的完整输入，由引擎从 DepreciationRule + Asset 组装
type Spec struct {
	OriginalPrice float64
	PurchaseDate   time.Time
	TotalMonths    int     // stages 为空时的直线折旧总月数
	FloorType      string  // amount / percent
	FloorVal       float64 // amount: 绝对额；percent: [0,1] 残值率
	Stages         []Stage
}

// ParseStages 解析并校验 stages JSON。空串表示未配置阶梯（合法，走直线折旧）；
// 非法配置在入口拒绝，引擎与净值计算不再做防御性容错
func ParseStages(raw string) ([]Stage, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var stages []Stage
	if err := json.Unmarshal([]byte(raw), &stages); err != nil {
		return nil, fmt.Errorf("折旧阶梯必须是 JSON 数组: %w", err)
	}
	if len(stages) == 0 {
		return nil, nil
	}
	var total float64
	for i, s := range stages {
		if s.Period < 1 {
			return nil, fmt.Errorf("折旧阶梯第 %d 段时长必须为正整数", i+1)
		}
		switch strings.ToUpper(s.Unit) {
		case UnitMonth:
			s.Unit = UnitMonth
		case UnitYear:
			s.Unit = UnitYear
		default:
			return nil, fmt.Errorf("折旧阶梯第 %d 段单位必须是 MONTH 或 YEAR", i+1)
		}
		if s.Ratio <= 0 || s.Ratio > 1 {
			return nil, fmt.Errorf("折旧阶梯第 %d 段折旧比例必须在 (0, 1] 区间", i+1)
		}
		total += s.Ratio
		stages[i] = s
	}
	if total > 1+1e-9 {
		return nil, fmt.Errorf("折旧阶梯累计比例 %.4f 超过 1", total)
	}
	return stages, nil
}

// TotalStageMonths 阶梯覆盖的总月数（YEAR × 12）
func TotalStageMonths(stages []Stage) int {
	var months int
	for _, s := range stages {
		if s.Unit == UnitYear {
			months += s.Period * 12
		} else {
			months += s.Period
		}
	}
	return months
}

// ValidateRuleSpec 规则入口校验（API 创建/更新共用）：
// months 为总折旧月数（直线折旧依据 + 展示/校验），配置阶梯时必须与阶梯覆盖月数一致，
// 防止"阶梯 3 年、总月数 5 年"这类口径不一致的规则流入引擎
func ValidateRuleSpec(name string, months int, floorType string, floorVal float64, stagesJSON string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("折旧规则名称不能为空")
	}
	if months < 1 {
		return fmt.Errorf("总折旧月数必须为正整数")
	}
	switch floorType {
	case FloorAmount:
		if floorVal < 0 {
			return fmt.Errorf("残值金额不能为负")
		}
	case FloorPercent:
		if floorVal < 0 || floorVal > 1 {
			return fmt.Errorf("残值率必须在 [0, 1] 区间")
		}
	default:
		return fmt.Errorf("残值类型必须是 amount 或 percent")
	}
	stages, err := ParseStages(stagesJSON)
	if err != nil {
		return err
	}
	if len(stages) > 0 && TotalStageMonths(stages) != months {
		return fmt.Errorf("总折旧月数 %d 与阶梯覆盖月数 %d 不一致", months, TotalStageMonths(stages))
	}
	return nil
}

// MonthsElapsed 日历整月差：不满整月向下取整（15 日购入，次月 14 日仍是 0 个月）。
// to 早于 from 时返回负值，调用方需自行防（NetValue 已防）
func MonthsElapsed(from, to time.Time) int {
	months := (to.Year()-from.Year())*12 + int(to.Month()) - int(from.Month())
	if to.Day() < from.Day() {
		months--
	}
	return months
}

// DepreciatedRatio 阶梯匹配：按购置时长顺序消费各段，整段走完计整段比例，
// 落在段内按整月线性折算，全部走完或未到首段边界即收敛。累计比例封顶 1.0
func DepreciatedRatio(stages []Stage, monthsElapsed int) float64 {
	var (
		total float64
		start int // 已完整走过的整月数（当前段起点）
	)
	for _, s := range stages {
		var stageMonths int
		if s.Unit == UnitYear {
			stageMonths = s.Period * 12
		} else {
			stageMonths = s.Period
		}
		if stageMonths <= 0 {
			continue
		}
		switch {
		case monthsElapsed >= start+stageMonths:
			total += s.Ratio
			start += stageMonths
		case monthsElapsed > start:
			total += s.Ratio * float64(monthsElapsed-start) / float64(stageMonths)
			// 之后的段还没到
			return math.Min(total, 1)
		default:
			// 还没到该段
			return math.Min(total, 1)
		}
	}
	return math.Min(total, 1)
}

// NetValue 计算时点净值：原值 ×（1 - 折旧比例），再套残值下限，
// 四舍五入到分。原值非正返回 0；时点早于购置日返回原值（未开始折旧）
func NetValue(spec Spec, now time.Time) float64 {
	if spec.OriginalPrice <= 0 {
		return 0
	}
	if spec.PurchaseDate.IsZero() || now.Before(spec.PurchaseDate) {
		return round2(spec.OriginalPrice)
	}
	months := MonthsElapsed(spec.PurchaseDate, now)
	if months < 0 {
		months = 0
	}
	var ratio float64
	if len(spec.Stages) > 0 {
		ratio = DepreciatedRatio(spec.Stages, months)
	} else if spec.TotalMonths > 0 {
		ratio = float64(months) / float64(spec.TotalMonths)
	}
	value := spec.OriginalPrice * (1 - math.Min(ratio, 1))

	// 残值下限兜底；amount 误配大于原值时以原值封顶，防止反向抬升
	floor := 0.0
	switch spec.FloorType {
	case FloorAmount:
		floor = spec.FloorVal
	case FloorPercent:
		floor = spec.OriginalPrice * spec.FloorVal
	}
	value = math.Max(value, floor)
	value = math.Min(value, spec.OriginalPrice)
	if value < 0 {
		value = 0
	}
	return round2(value)
}

// round2 四舍五入到分（HALF_UP），与台账 decimal(10,2) 精度对齐
func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
