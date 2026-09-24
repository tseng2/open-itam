package model

import (
	"testing"
	"time"
)

func presenceFixture() (time.Time, time.Duration) {
	return time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC), 15 * time.Minute
}

func activeDispatch(expectedReturn time.Time, isolation bool) *AssetDispatch {
	return &AssetDispatch{
		Status:           DispatchStatusActive,
		ExpectedReturnAt: expectedReturn,
		IsolationOffline: isolation,
	}
}

func TestResolvePresenceOnline(t *testing.T) {
	now, threshold := presenceFixture()
	got := ResolvePresence(PresenceInput{
		Now: now, HeartbeatTimeout: threshold,
		LastSeenAt: now.Add(-5 * time.Minute),
	})
	if got != PresenceOnline {
		t.Fatalf("expected online, got %q", got)
	}
}

func TestResolvePresenceRoaming(t *testing.T) {
	now, threshold := presenceFixture()
	got := ResolvePresence(PresenceInput{
		Now: now, HeartbeatTimeout: threshold,
		LastSeenAt: now.Add(-5 * time.Minute),
		PublicIP:   "203.0.113.7",
	})
	if got != PresenceRoaming {
		t.Fatalf("expected roaming, got %q", got)
	}
}

func TestResolvePresenceMissing(t *testing.T) {
	now, threshold := presenceFixture()
	got := ResolvePresence(PresenceInput{
		Now: now, HeartbeatTimeout: threshold,
		LastSeenAt: now.Add(-time.Hour),
	})
	if got != PresenceMissing {
		t.Fatalf("expected missing, got %q", got)
	}
}

func TestResolvePresenceNeverSeenIsMissing(t *testing.T) {
	now, threshold := presenceFixture()
	got := ResolvePresence(PresenceInput{Now: now, HeartbeatTimeout: threshold})
	if got != PresenceMissing {
		t.Fatalf("zero last_seen (never seen) must be missing, got %q", got)
	}
}

func TestResolvePresenceOverdueBeatsOnline(t *testing.T) {
	now, threshold := presenceFixture()
	got := ResolvePresence(PresenceInput{
		Now: now, HeartbeatTimeout: threshold,
		LastSeenAt:     now.Add(-5 * time.Minute),
		ActiveDispatch: activeDispatch(now.Add(-time.Hour), false),
	})
	if got != PresenceOverdue {
		t.Fatalf("overdue dispatch must win even when online, got %q", got)
	}
}

func TestResolvePresenceOverdueBeatsOffline(t *testing.T) {
	now, threshold := presenceFixture()
	got := ResolvePresence(PresenceInput{
		Now: now, HeartbeatTimeout: threshold,
		LastSeenAt:     now.Add(-time.Hour),
		ActiveDispatch: activeDispatch(now.Add(-time.Hour), true),
	})
	if got != PresenceOverdue {
		t.Fatalf("overdue dispatch must win even when offline, got %q", got)
	}
}

func TestResolvePresenceDispatchOffline(t *testing.T) {
	now, threshold := presenceFixture()
	got := ResolvePresence(PresenceInput{
		Now: now, HeartbeatTimeout: threshold,
		LastSeenAt:     now.Add(-time.Hour),
		ActiveDispatch: activeDispatch(now.Add(24*time.Hour), true),
	})
	if got != PresenceDispatchOffline {
		t.Fatalf("isolated dispatch offline within period must be expected, got %q", got)
	}
}

func TestResolvePresenceDispatchWithoutIsolationOfflineIsMissing(t *testing.T) {
	now, threshold := presenceFixture()
	// 外派未标隔离而离线：无豁免依据，保守报疑似失联，
	// 管理员应核实或补登隔离标记
	got := ResolvePresence(PresenceInput{
		Now: now, HeartbeatTimeout: threshold,
		LastSeenAt:     now.Add(-time.Hour),
		ActiveDispatch: activeDispatch(now.Add(24*time.Hour), false),
	})
	if got != PresenceMissing {
		t.Fatalf("dispatch offline without isolation flag must be missing, got %q", got)
	}
}

func TestResolvePresenceDispatchIsolationButOnlineShowsFactual(t *testing.T) {
	now, threshold := presenceFixture()
	// 隔离标记意味着"应离线"，设备实际在线时如实展示在线/漫游，
	// 违规联网的告警语义交给后续 A4/网卡识别，不在五态内混判
	got := ResolvePresence(PresenceInput{
		Now: now, HeartbeatTimeout: threshold,
		LastSeenAt:     now.Add(-5 * time.Minute),
		ActiveDispatch: activeDispatch(now.Add(24*time.Hour), true),
	})
	if got != PresenceOnline {
		t.Fatalf("isolation-marked device that is online must show online, got %q", got)
	}

	got = ResolvePresence(PresenceInput{
		Now: now, HeartbeatTimeout: threshold,
		LastSeenAt:     now.Add(-5 * time.Minute),
		PublicIP:       "203.0.113.7",
		ActiveDispatch: activeDispatch(now.Add(24*time.Hour), true),
	})
	if got != PresenceRoaming {
		t.Fatalf("isolation-marked device online via public ip must show roaming, got %q", got)
	}
}

func TestResolvePresenceThresholdBoundary(t *testing.T) {
	now, threshold := presenceFixture()
	// 恰好等于阈值不算离线（严格大于才判），容忍心跳抖动的边界语义
	got := ResolvePresence(PresenceInput{
		Now: now, HeartbeatTimeout: threshold,
		LastSeenAt: now.Add(-threshold),
	})
	if got != PresenceOnline {
		t.Fatalf("last_seen exactly at threshold must be online, got %q", got)
	}
}

func TestResolvePresenceOverdueBoundary(t *testing.T) {
	now, threshold := presenceFixture()
	// 恰好到达预计归期不算超期（严格晚于才判）
	got := ResolvePresence(PresenceInput{
		Now: now, HeartbeatTimeout: threshold,
		LastSeenAt:     now.Add(-5 * time.Minute),
		ActiveDispatch: activeDispatch(now, false),
	})
	if got != PresenceOnline {
		t.Fatalf("expected_return exactly now must not be overdue, got %q", got)
	}
}
