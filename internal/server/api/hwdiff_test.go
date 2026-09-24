package api

import (
	"strings"
	"testing"

	"itagent/internal/shared/protocol"
)

func baseHardware() protocol.Hardware {
	return protocol.Hardware{
		Brand: "Dell", Model: "M1", Serial: "SN1",
		MemoryTotalMB: 16 * 1024,
		CPU:  []protocol.CPU{{Model: "Intel i5-1240P", Cores: 12, Threads: 16}},
		Disks: []protocol.Disk{
			{Serial: "WD-AAAA", Model: "SSD", SizeGB: 512, Type: "SSD"},
			{Serial: "WD-BBBB", Model: "HDD", SizeGB: 1024, Type: "HDD"},
		},
	}
}

func TestCompareHardwareNoChange(t *testing.T) {
	base := baseHardware()
	same := baseHardware()
	if diff := compareHardware(base, same); len(diff) != 0 {
		t.Fatalf("identical hardware must yield no diff, got %v", diff)
	}
}

func TestCompareHardwareMemory(t *testing.T) {
	base := baseHardware()
	cur := baseHardware()
	cur.MemoryTotalMB = 32 * 1024
	diff := compareHardware(base, cur)
	if len(diff) != 1 || !strings.Contains(diff[0], "内存") || !strings.Contains(diff[0], "16GB") || !strings.Contains(diff[0], "32GB") {
		t.Fatalf("expected memory diff, got %v", diff)
	}
}

// 可移动介质（U 盘/移动硬盘）插拔属日常使用，不是硬件变更——
// 修复旧实现 len(disks) 直接比数量导致的误报
func TestCompareHardwareRemovablePlugIgnored(t *testing.T) {
	base := baseHardware()
	cur := baseHardware()
	cur.Disks = append(cur.Disks, protocol.Disk{Serial: "USB-001", Model: "Kingston", SizeGB: 64, Removable: true})
	if diff := compareHardware(base, cur); len(diff) != 0 {
		t.Fatalf("removable disk plug must not be reported, got %v", diff)
	}
}

func TestCompareHardwareDiskCount(t *testing.T) {
	base := baseHardware()
	cur := baseHardware()
	cur.Disks = cur.Disks[:1]
	diff := compareHardware(base, cur)
	if len(diff) != 1 || !strings.Contains(diff[0], "内置磁盘数量") {
		t.Fatalf("expected built-in disk count diff, got %v", diff)
	}
}

// 换盘检测：数量与容量都相同、仅序列号变化——旧实现完全检测不到的篡改场景
func TestCompareHardwareDiskSerialSwapped(t *testing.T) {
	base := baseHardware()
	cur := baseHardware()
	cur.Disks[0].Serial = "WD-CHANGED"
	diff := compareHardware(base, cur)
	if len(diff) != 1 {
		t.Fatalf("expected exactly 1 diff, got %v", diff)
	}
	if !strings.Contains(diff[0], "磁盘更换") || !strings.Contains(diff[0], "WD-AAAA") || !strings.Contains(diff[0], "WD-CHANGED") {
		t.Fatalf("serial swap diff must name old and new serial, got %v", diff)
	}
}

// 未取到序列号的盘（垃圾值/虚拟盘）不参与集合比对，防止噪声误报
func TestCompareHardwareEmptySerialIgnored(t *testing.T) {
	base := baseHardware()
	cur := baseHardware()
	cur.Disks[0].Serial = ""
	if diff := compareHardware(base, cur); len(diff) != 0 {
		t.Fatalf("empty serial must be ignored, got %v", diff)
	}
}

func TestCompareHardwareCPUCount(t *testing.T) {
	base := baseHardware()
	cur := baseHardware()
	cur.CPU = append(cur.CPU, protocol.CPU{Model: "Intel i5-1240P", Cores: 12, Threads: 16})
	diff := compareHardware(base, cur)
	if len(diff) != 1 || !strings.Contains(diff[0], "CPU数量") {
		t.Fatalf("expected cpu count diff, got %v", diff)
	}
}

func TestCompareHardwareCPUModelSwapped(t *testing.T) {
	base := baseHardware()
	cur := baseHardware()
	cur.CPU[0].Model = "Intel i7-1360P"
	diff := compareHardware(base, cur)
	if len(diff) != 1 {
		t.Fatalf("expected exactly 1 diff, got %v", diff)
	}
	if !strings.Contains(diff[0], "CPU型号") || !strings.Contains(diff[0], "i5-1240P") || !strings.Contains(diff[0], "i7-1360P") {
		t.Fatalf("cpu model diff must name old and new models, got %v", diff)
	}
}

// 多项同时变更时按固定顺序输出（内存→磁盘数量→磁盘SN→CPU），保证描述可读且测试可断言
func TestCompareHardwareMultipleChangesStableOrder(t *testing.T) {
	base := baseHardware()
	cur := baseHardware()
	cur.MemoryTotalMB = 8 * 1024
	cur.CPU[0].Model = "Intel i7-1360P"
	cur.Disks[1].Serial = "WD-CHANGED"
	diff := compareHardware(base, cur)
	if len(diff) != 3 {
		t.Fatalf("expected 3 diffs, got %v", diff)
	}
	if !strings.Contains(diff[0], "内存") || !strings.Contains(diff[1], "磁盘更换") || !strings.Contains(diff[2], "CPU型号") {
		t.Fatalf("unexpected diff order: %v", diff)
	}
}

func TestCompareHardwareNilSlices(t *testing.T) {
	base := protocol.Hardware{MemoryTotalMB: 16 * 1024}
	cur := protocol.Hardware{MemoryTotalMB: 16 * 1024, CPU: []protocol.CPU{{Model: "X"}}}
	if diff := compareHardware(base, cur); len(diff) != 1 || !strings.Contains(diff[0], "CPU数量") {
		t.Fatalf("nil base cpu slice must diff by count, got %v", diff)
	}
}
