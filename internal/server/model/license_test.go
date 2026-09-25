package model

import (
	"testing"
	"time"
)

// P2 软件许可：状态派生纯函数测试。状态不落库（与外派"超期是计算
// 属性"同口径），由 终止日 > 到期日 > 在用 的优先级从日期驱动，
// 日期变更自动传播，杜绝"改日期忘改状态"的脏数据

func licenseDate(ref time.Time, offsetDays int) *time.Time {
	t := ref.AddDate(0, 0, offsetDays)
	return &t
}

func TestResolveLicenseStatus(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name        string
		expiration  *time.Time
		termination *time.Time
		want        int
	}{
		{"永久授权（无到期日）", nil, nil, LicenseStatusActive},
		{"未到期", licenseDate(now, 30), nil, LicenseStatusActive},
		{"到期日当天仍在用（闭区间）", licenseDate(now, 0), nil, LicenseStatusActive},
		{"已过期", licenseDate(now, -1), nil, LicenseStatusExpired},
		{"终止日当天已终止", nil, licenseDate(now, 0), LicenseStatusTerminated},
		{"终止盖过未到期", licenseDate(now, 30), licenseDate(now, -10), LicenseStatusTerminated},
		{"终止盖过已过期", licenseDate(now, -30), licenseDate(now, -1), LicenseStatusTerminated},
	}
	for _, tc := range cases {
		if got := ResolveLicenseStatus(now, tc.expiration, tc.termination); got != tc.want {
			t.Fatalf("%s: got %d want %d", tc.name, got, tc.want)
		}
	}
}
