package depreciation

import (
	"math"
	"testing"
	"time"
)

var ladderStages = `[{"period":12,"unit":"MONTH","ratio":0.35},{"period":12,"unit":"MONTH","ratio":0.30},{"period":12,"unit":"MONTH","ratio":0.30}]`

func mustStages(t *testing.T, raw string) []Stage {
	t.Helper()
	stages, err := ParseStages(raw)
	if err != nil {
		t.Fatalf("parse stages %q: %v", raw, err)
	}
	return stages
}

func TestParseStagesValid(t *testing.T) {
	stages := mustStages(t, ladderStages)
	if len(stages) != 3 {
		t.Fatalf("expected 3 stages, got %d", len(stages))
	}
	if stages[0].Period != 12 || stages[0].Unit != UnitMonth || stages[0].Ratio != 0.35 {
		t.Fatalf("unexpected first stage: %+v", stages[0])
	}
	if TotalStageMonths(stages) != 36 {
		t.Fatalf("expected total 36 months, got %d", TotalStageMonths(stages))
	}
	// YEAR 单位换算为月
	yearStages := mustStages(t, `[{"period":3,"unit":"YEAR","ratio":0.95}]`)
	if TotalStageMonths(yearStages) != 36 {
		t.Fatalf("expected 36 months for 3 years, got %d", TotalStageMonths(yearStages))
	}
}

func TestParseStagesEmpty(t *testing.T) {
	stages, err := ParseStages("")
	if err != nil || stages != nil {
		t.Fatalf("expected empty stages without error, got %v %v", stages, err)
	}
	stages, err = ParseStages("  ")
	if err != nil || stages != nil {
		t.Fatalf("expected whitespace-only as empty, got %v %v", stages, err)
	}
}

func TestParseStagesInvalid(t *testing.T) {
	cases := []string{
		`{"period":12}`,                       // 不是数组
		`[{"period":0,"unit":"MONTH","ratio":0.5}]`,    // 时长必须为正
		`[{"period":-3,"unit":"YEAR","ratio":0.5}]`,    // 负时长
		`[{"period":12,"unit":"WEEK","ratio":0.5}]`,    // 未知单位
		`[{"period":12,"unit":"","ratio":0.5}]`,        // 缺单位
		`[{"period":12,"unit":"MONTH","ratio":0}]`,     // 比例必须为正
		`[{"period":12,"unit":"MONTH","ratio":-0.5}]`,  // 负比例
		`[{"period":12,"unit":"MONTH","ratio":1.01}]`,  // 比例超过 1
		`[{"period":12,"unit":"MONTH","ratio":0.5},{"period":12,"unit":"MONTH","ratio":0.6}]`, // 累计超过 1
		`not-json`,
	}
	for i, raw := range cases {
		if _, err := ParseStages(raw); err == nil {
			t.Fatalf("case %d: expected error for %s", i, raw)
		}
	}
}

func TestMonthsElapsed(t *testing.T) {
	cases := []struct {
		from, to string
		want     int
	}{
		{"2025-01-15", "2025-01-15", 0},
		{"2025-01-15", "2025-01-31", 0},  // 同月不算整月
		{"2025-01-15", "2025-02-14", 0},  // 14 < 15 未满整月
		{"2025-01-15", "2025-02-15", 1},
		{"2025-01-31", "2025-02-28", 0},  // 月末边界：28 < 31 未满整月
		{"2025-01-15", "2025-03-14", 1},
		{"2025-01-15", "2026-02-15", 13}, // 跨年
		{"2025-01-15", "2024-12-15", -1}, // to 在前为负，调用方负责防
	}
	for _, c := range cases {
		from, _ := time.Parse("2006-01-02", c.from)
		to, _ := time.Parse("2006-01-02", c.to)
		if got := MonthsElapsed(from, to); got != c.want {
			t.Fatalf("MonthsElapsed(%s, %s) = %d, want %d", c.from, c.to, got, c.want)
		}
	}
}

func TestDepreciatedRatioLadder(t *testing.T) {
	stages := mustStages(t, ladderStages) // 35% + 30% + 30%，各 12 个月
	cases := []struct {
		months int
		want   float64
	}{
		{0, 0},
		{6, 0.175},    // 第 1 段过半：0.35 * 6/12
		{12, 0.35},    // 第 1 段走完
		{18, 0.5},     // 第 2 段过半：0.35 + 0.30*6/12
		{24, 0.65},    // 第 2 段走完
		{30, 0.8},     // 第 3 段过半
		{36, 0.95},    // 全部走完，累计 0.95（剩余 5% 靠残值语义表达）
		{48, 0.95},    // 超出阶梯后不再继续折旧
	}
	for _, c := range cases {
		if got := DepreciatedRatio(stages, c.months); math.Abs(got-c.want) > 1e-9 {
			t.Fatalf("DepreciatedRatio(months=%d) = %v, want %v", c.months, got, c.want)
		}
	}
}

func TestNetValueLadderFlow(t *testing.T) {
	purchase, _ := time.Parse("2006-01-02", "2023-01-15")
	spec := Spec{
		OriginalPrice: 10000,
		PurchaseDate:  purchase,
		FloorType:     FloorPercent,
		FloorVal:      0.05, // 5% 残值率
		Stages:        mustStages(t, ladderStages),
	}
	cases := []struct {
		now  string
		want float64
	}{
		{"2023-01-15", 10000},  // 购置当日未折旧
		{"2023-07-15", 8250},   // 6 个月：10000*(1-0.175)
		{"2024-01-15", 6500},   // 12 个月
		{"2025-01-15", 3500},   // 24 个月
		{"2025-07-15", 2000},   // 30 个月
		{"2026-01-15", 500},    // 36 个月：净值 = 残值 5%
		{"2028-06-01", 500},    // 阶梯走完后稳定在残值
	}
	for _, c := range cases {
		now, _ := time.Parse("2006-01-02", c.now)
		if got := NetValue(spec, now); math.Abs(got-c.want) > 0.005 {
			t.Fatalf("NetValue(%s) = %v, want %v", c.now, got, c.want)
		}
	}
}

func TestNetValueStraightLine(t *testing.T) {
	purchase, _ := time.Parse("2006-01-02", "2023-01-15")
	spec := Spec{
		OriginalPrice: 12000,
		PurchaseDate:  purchase,
		TotalMonths:   36, // stages 为空：按总月数直线折旧
		FloorType:     FloorPercent,
		FloorVal:      0.05,
	}
	cases := []struct {
		now  string
		want float64
	}{
		{"2023-01-15", 12000},
		{"2024-07-15", 6000},   // 18 个月折半
		{"2025-07-14", 2333.33}, // 29 个月（14<15 未满整月）：12000*(1-29/36)
	}
	for _, c := range cases {
		now, _ := time.Parse("2006-01-02", c.now)
		if got := NetValue(spec, now); math.Abs(got-c.want) > 0.005 {
			t.Fatalf("NetValue(%s) = %v, want %v", c.now, got, c.want)
		}
	}
	// 36 个月折完 → 残值兜底 5%
	end, _ := time.Parse("2006-01-02", "2026-01-15")
	if got := NetValue(spec, end); math.Abs(got-600) > 0.005 {
		t.Fatalf("straight line floor: got %v, want 600", got)
	}
}

func TestNetValueFloorAmount(t *testing.T) {
	purchase, _ := time.Parse("2006-01-02", "2023-01-15")
	spec := Spec{
		OriginalPrice: 12000,
		PurchaseDate:  purchase,
		TotalMonths:   36,
		FloorType:     FloorAmount,
		FloorVal:      500, // 绝对金额残值 500 元
	}
	end, _ := time.Parse("2006-01-02", "2026-01-15")
	if got := NetValue(spec, end); math.Abs(got-500) > 0.005 {
		t.Fatalf("amount floor: got %v, want 500", got)
	}
	mid, _ := time.Parse("2006-01-02", "2024-07-15")
	if got := NetValue(spec, mid); math.Abs(got-6000) > 0.005 {
		t.Fatalf("above floor should stay linear: got %v, want 6000", got)
	}
}

func TestNetValueGuards(t *testing.T) {
	purchase, _ := time.Parse("2006-01-02", "2023-01-15")
	now, _ := time.Parse("2006-01-02", "2025-01-15")

	if got := NetValue(Spec{OriginalPrice: 0, PurchaseDate: purchase, TotalMonths: 36}, now); got != 0 {
		t.Fatalf("price <= 0 should return 0, got %v", got)
	}
	if got := NetValue(Spec{OriginalPrice: -5, PurchaseDate: purchase, TotalMonths: 36}, now); got != 0 {
		t.Fatalf("negative price should return 0, got %v", got)
	}
	// 购置日期晚于当前：未开始折旧，保持原值
	future, _ := time.Parse("2006-01-02", "2030-01-15")
	if got := NetValue(Spec{OriginalPrice: 888, PurchaseDate: future, TotalMonths: 36}, now); got != 888 {
		t.Fatalf("before purchase should keep original price, got %v", got)
	}
	// 金额四舍五入到分（HALF_UP）
	spec := Spec{OriginalPrice: 3333.33, PurchaseDate: purchase, TotalMonths: 36}
	at, _ := time.Parse("2006-01-02", "2024-07-15") // 18 个月，折半
	if got := NetValue(spec, at); math.Abs(got-1666.67) > 0.005 {
		t.Fatalf("rounding: got %v, want 1666.67", got)
	}
	// amount 残值配置大于原值时以原值封顶（防配置错误反向抬升净值）
	spec = Spec{OriginalPrice: 100, PurchaseDate: purchase, TotalMonths: 12, FloorType: FloorAmount, FloorVal: 9999}
	if got := NetValue(spec, at); got != 100 {
		t.Fatalf("floor above price must clamp to price, got %v", got)
	}
}

func TestValidateRuleSpec(t *testing.T) {
	ok := func(name string, months int, floorType string, floorVal float64, stages string) error {
		return ValidateRuleSpec(name, months, floorType, floorVal, stages)
	}
	if err := ok("3年加速", 36, FloorPercent, 0.05, ladderStages); err != nil {
		t.Fatalf("valid rule rejected: %v", err)
	}
	if err := ok("直线3年", 36, FloorPercent, 0.05, ""); err != nil {
		t.Fatalf("straight-line rule rejected: %v", err)
	}

	errs := []struct {
		name   string
		gotErr error
	}{
		{"空名称", ok("", 36, FloorPercent, 0.05, "")},
		{"零月数", ok("x", 0, FloorPercent, 0.05, "")},
		{"负月数", ok("x", -1, FloorPercent, 0.05, "")},
		{"未知残值类型", ok("x", 36, "ratio", 0.05, "")},
		{"负残值", ok("x", 36, FloorAmount, -1, "")},
		{"残值率超1", ok("x", 36, FloorPercent, 1.5, "")},
		{"非法stages", ok("x", 36, FloorPercent, 0.05, "not-json")},
		{"stages总月数不匹配", ok("x", 24, FloorPercent, 0.05, ladderStages)}, // stages 合计 36 个月
	}
	for _, c := range errs {
		if c.gotErr == nil {
			t.Fatalf("%s: expected error", c.name)
		}
	}
}
