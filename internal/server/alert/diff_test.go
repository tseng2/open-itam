package alert

import (
	"testing"

	"itagent/internal/shared/protocol"
)

func baseline() protocol.Hardware {
	return protocol.Hardware{
		Brand:  "Dell Inc.",
		Model:  "OptiPlex 7010",
		Serial: "7XKQ1P3",
		CPU: []protocol.CPU{
			{Model: "i7-13700", Cores: 16, Threads: 24},
		},
		MemoryTotalMB: 32768,
		MemoryModules: []protocol.MemoryModule{
			{Slot: "DIMM1", SizeMB: 16384, Serial: "MEM-A"},
			{Slot: "DIMM2", SizeMB: 16384, Serial: "MEM-B"},
		},
		Disks: []protocol.Disk{
			{Model: "Samsung 990 PRO", Serial: "DISK-1", SizeGB: 1000},
		},
		GPUs: []protocol.GPU{{Model: "RTX 4060"}},
		NICs: []protocol.NIC{{Name: "I225-V", MAC: "AA:BB:CC:DD:EE:01"}},
	}
}

func TestNoChangesNoEvents(t *testing.T) {
	events := DiffHardware("dev1", baseline(), baseline())
	if len(events) != 0 {
		t.Fatalf("expected no events, got %+v", events)
	}
}

func TestDiskAdded(t *testing.T) {
	before := baseline()
	after := baseline()
	after.Disks = append(after.Disks, protocol.Disk{Model: "WD Blue", Serial: "DISK-2", SizeGB: 2000})

	events := DiffHardware("dev1", before, after)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %+v", events)
	}
	e := events[0]
	if e.Kind != EventDiskCountChanged || e.Severity != SeverityWarning {
		t.Fatalf("wrong event: %+v", e)
	}
	if e.Detail["after"] != "2" {
		t.Fatalf("detail wrong: %+v", e.Detail)
	}
}

func TestDiskRemoved(t *testing.T) {
	before := baseline()
	after := baseline()
	after.Disks = nil

	events := DiffHardware("dev1", before, after)
	if len(events) != 1 || events[0].Kind != EventDiskCountChanged {
		t.Fatalf("wrong: %+v", events)
	}
}

func TestDiskSerialSwap(t *testing.T) {
	before := baseline()
	after := baseline()
	after.Disks[0].Serial = "DISK-NEW"

	events := DiffHardware("dev1", before, after)
	if len(events) != 1 || events[0].Kind != EventDiskSerialSwapped {
		t.Fatalf("expected serial swap, got %+v", events)
	}
	if events[0].Detail["old_serial"] != "DISK-1" || events[0].Detail["new_serial"] != "DISK-NEW" {
		t.Fatalf("detail missing serials: %+v", events[0].Detail)
	}
}

func TestMemoryModuleReplaced(t *testing.T) {
	before := baseline()
	after := baseline()
	after.MemoryModules[1].Serial = "MEM-X"

	events := DiffHardware("dev1", before, after)
	if len(events) != 1 || events[0].Kind != EventMemorySerialSwapped {
		t.Fatalf("expected memory swap, got %+v", events)
	}
}

func TestMemoryCountChange(t *testing.T) {
	before := baseline()
	after := baseline()
	after.MemoryModules = after.MemoryModules[:1]

	events := DiffHardware("dev1", before, after)
	if len(events) != 1 || events[0].Kind != EventMemoryCountChanged {
		t.Fatalf("wrong: %+v", events)
	}
}

func TestCPUModelChanged(t *testing.T) {
	before := baseline()
	after := baseline()
	after.CPU[0].Model = "i9-14900"

	events := DiffHardware("dev1", before, after)
	found := false
	for _, e := range events {
		if e.Kind == EventCPUChanged {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected CPU changed event, got %+v", events)
	}
}

func TestGPUCountChanged(t *testing.T) {
	before := baseline()
	after := baseline()
	after.GPUs = nil

	events := DiffHardware("dev1", before, after)
	found := false
	for _, e := range events {
		if e.Kind == EventGPUCountChanged {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected GPU count event, got %+v", events)
	}
}

func TestBrandModelSerialChanged(t *testing.T) {
	before := baseline()
	after := baseline()
	after.Serial = "NEWSN999"

	events := DiffHardware("dev1", before, after)
	found := false
	for _, e := range events {
		if e.Kind == EventHostSerialChanged {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected host serial event, got %+v", events)
	}
}

func TestFirstReportNoBaseline(t *testing.T) {
	events := DiffHardware("dev1", protocol.Hardware{}, baseline())
	if len(events) != 0 {
		t.Fatalf("first full report must not alert (baseline seeding): %+v", events)
	}
}
