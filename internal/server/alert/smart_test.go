package alert

import (
	"testing"

	"itagent/internal/shared/protocol"
)

func healthySSD() protocol.SmartHealth {
	return protocol.SmartHealth{
		DiskSerial: "S1", Model: "Samsung 990", OverallHealth: "PASSED",
		ReallocatedSectors: 0, PendingSectors: 0, PowerOnHours: 100,
		TemperatureC: 40, PercentLifetimeUsed: 20,
	}
}

func TestSmartHealthyNoAlert(t *testing.T) {
	events := CheckSMART("dev1", []protocol.SmartHealth{healthySSD()})
	if len(events) != 0 {
		t.Fatalf("healthy disk must not alert: %+v", events)
	}
}

func TestSmartReallocated(t *testing.T) {
	s := healthySSD()
	s.ReallocatedSectors = 3
	events := CheckSMART("dev1", []protocol.SmartHealth{s})
	if len(events) != 1 || events[0].Kind != EventSmartReallocated || events[0].Severity != SeverityWarning {
		t.Fatalf("wrong: %+v", events)
	}
}

func TestSmartPending(t *testing.T) {
	s := healthySSD()
	s.PendingSectors = 2
	events := CheckSMART("dev1", []protocol.SmartHealth{s})
	if events[0].Severity != SeverityCritical {
		t.Fatalf("pending sectors must be critical: %+v", events[0])
	}
}

func TestSmartHealthFailed(t *testing.T) {
	s := healthySSD()
	s.OverallHealth = "FAILED"
	events := CheckSMART("dev1", []protocol.SmartHealth{s})
	if events[0].Severity != SeverityEmergency {
		t.Fatalf("overall fail must be emergency: %+v", events[0])
	}
}

func TestSmartLifetimeUsed(t *testing.T) {
	s := healthySSD()
	s.PercentLifetimeUsed = 85
	events := CheckSMART("dev1", []protocol.SmartHealth{s})
	found := false
	for _, e := range events {
		if e.Kind == EventSmartLifetime {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected lifetime event: %+v", events)
	}
}

func TestSmartTemp(t *testing.T) {
	s := healthySSD()
	s.TemperatureC = 65
	// 单次不告警
	e1 := CheckSMART("dev1", []protocol.SmartHealth{s})
	if len(e1) != 0 {
		t.Fatalf("single temp spike must not alert: %+v", e1)
	}
	// 连续 3 次触发（用调用方传入的连续计数模拟）
	events := CheckSMARTWithStreak("dev1", []protocol.SmartHealth{s}, map[string]int{"S1": 3})
	found := false
	for _, e := range events {
		if e.Kind == EventSmartTemp {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected temp event after 3 streaks: %+v", events)
	}
}

func TestSmartMultipleDisks(t *testing.T) {
	a := healthySSD()
	b := healthySSD()
	b.DiskSerial = "S2"
	b.PendingSectors = 1
	events := CheckSMART("dev1", []protocol.SmartHealth{a, b})
	if len(events) != 1 {
		t.Fatalf("expected only one event: %+v", events)
	}
	if events[0].Detail["disk_serial"] != "S2" {
		t.Fatalf("event must attribute to S2: %+v", events[0].Detail)
	}
}
