package v1

import (
	"testing"
	"time"
)

// P2 报表中心：聚合纯函数测试（口径单源——hwdiff/depreciation 先例）。
// 覆盖：年度价值窗口填充与跨窗丢弃、月度趋势零填充与边界月份、
// 分布行百分比与排序、空数据防御

func TestGroupAnnualValue(t *testing.T) {
	now := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	samples := []annualValueSample{
		{PurchaseDate: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), Original: 10000, Net: 8000},
		{PurchaseDate: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC), Original: 5000, Net: 3000},
		{PurchaseDate: time.Date(2025, 11, 1, 0, 0, 0, 0, time.UTC), Original: 2000, Net: 1000},
		{PurchaseDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), Original: 4000, Net: 500},
		{PurchaseDate: time.Date(2023, 5, 1, 0, 0, 0, 0, time.UTC), Original: 9999, Net: 999}, // 窗口外丢弃
	}

	rows := groupAnnualValue(now, 3, samples)
	if len(rows) != 3 {
		t.Fatalf("3-year window must have 3 rows, got %d", len(rows))
	}
	// 新年份在前；各年聚合正确
	want := []reportAnnualRow{
		{Year: 2026, Count: 1, Original: 10000, Net: 8000},
		{Year: 2025, Count: 2, Original: 7000, Net: 4000},
		{Year: 2024, Count: 1, Original: 4000, Net: 500},
	}
	for i := range rows {
		if rows[i] != want[i] {
			t.Fatalf("row %d: got %+v want %+v", i, rows[i], want[i])
		}
	}

	// 空窗年零填充
	rows = groupAnnualValue(now, 2, samples[3:]) // 只剩 2024 与 2023
	if len(rows) != 2 || rows[0].Year != 2026 || rows[0].Count != 0 || rows[1].Year != 2025 {
		t.Fatalf("zero-fill window: %+v", rows)
	}
	// 空输入防御
	if rows := groupAnnualValue(now, 3, nil); len(rows) != 3 {
		t.Fatalf("empty samples must still fill window, got %d", len(rows))
	}
}

func TestBucketMonthlyTrend(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	created := []time.Time{
		now,                              // 2026-09
		now.AddDate(0, -1, 0),            // 2026-08
		now.AddDate(0, -3, 0),            // 2026-06（窗口起点）
		time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), // 窗口起点当月月初
		now.AddDate(0, -4, 0),            // 2026-05 窗口外丢弃
	}

	rows := bucketMonthlyTrend(now, 4, created)
	if len(rows) != 4 {
		t.Fatalf("4-month window must have 4 rows, got %d", len(rows))
	}
	// 升序 + 零填充
	want := []reportTrendRow{
		{Month: "2026-06", Count: 2},
		{Month: "2026-07", Count: 0},
		{Month: "2026-08", Count: 1},
		{Month: "2026-09", Count: 1},
	}
	for i := range rows {
		if rows[i] != want[i] {
			t.Fatalf("row %d: got %+v want %+v", i, rows[i], want[i])
		}
	}
	// 空输入防御
	if rows := bucketMonthlyTrend(now, 3, nil); len(rows) != 3 {
		t.Fatalf("empty input must still fill window, got %d", len(rows))
	}
}

func TestDistributionRows(t *testing.T) {
	counts := map[string]int64{"使用中": 2, "库存中": 1, "维修中": 1, "已报废": 1}
	rows := distributionRows(counts)
	if len(rows) != 4 {
		t.Fatalf("must have 4 rows, got %d", len(rows))
	}
	// 计数降序；百分比 = 计数/总数（四舍五入到整数百分比）
	if rows[0].Label != "使用中" || rows[0].Count != 2 || rows[0].Percent != 40 {
		t.Fatalf("top row: %+v", rows[0])
	}
	if rows[1].Percent != 20 {
		t.Fatalf("second row percent: %+v", rows[1])
	}
	// 总数为零 → 空结果（除零防御）
	if rows := distributionRows(map[string]int64{}); len(rows) != 0 {
		t.Fatalf("empty counts must return empty rows, got %d", len(rows))
	}
	if rows := distributionRows(map[string]int64{"使用中": 0}); len(rows) != 0 {
		t.Fatalf("all-zero counts must return empty rows, got %d", len(rows))
	}
}
