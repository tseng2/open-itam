package alert

import (
	"fmt"

	"itagent/internal/shared/protocol"
)

const (
	SeverityInfo      = "info"
	SeverityWarning   = "warning"
	SeverityCritical  = "critical"
	SeverityEmergency = "emergency"
)

const (
	EventHostSerialChanged   = "host_serial_changed"
	EventHostModelChanged    = "host_model_changed"
	EventDiskCountChanged    = "disk_count_changed"
	EventDiskSerialSwapped   = "disk_serial_swapped"
	EventMemoryCountChanged  = "memory_count_changed"
	EventMemorySerialSwapped = "memory_serial_swapped"
	EventCPUChanged          = "cpu_changed"
	EventGPUCountChanged     = "gpu_count_changed"
	EventNICCountChanged     = "nic_count_changed"

	EventSmartHealthFailed = "smart_health_failed"
	EventSmartReallocated  = "smart_reallocated"
	EventSmartPending      = "smart_pending"
	EventSmartLifetime     = "smart_lifetime"
	EventSmartTemp         = "smart_temp"
)

type Event struct {
	Kind     string            `json:"kind"`
	Severity string            `json:"severity"`
	Message  string            `json:"message"`
	Detail   map[string]string `json:"detail,omitempty"`
}

func DiffHardware(deviceID string, before, after protocol.Hardware) []Event {
	if before.Serial == "" && before.Brand == "" && len(before.Disks) == 0 && len(before.CPU) == 0 {
		return nil
	}
	var out []Event

	if before.Serial != "" && after.Serial != "" && before.Serial != after.Serial {
		out = append(out, Event{
			Kind:     EventHostSerialChanged,
			Severity: SeverityCritical,
			Message:  fmt.Sprintf("主机序列号变更: %s → %s", before.Serial, after.Serial),
			Detail:   map[string]string{"before": before.Serial, "after": after.Serial},
		})
	}
	if before.Model != "" && after.Model != "" && before.Model != after.Model {
		out = append(out, Event{
			Kind:     EventHostModelChanged,
			Severity: SeverityWarning,
			Message:  fmt.Sprintf("主机型号变更: %s → %s", before.Model, after.Model),
			Detail:   map[string]string{"before": before.Model, "after": after.Model},
		})
	}

	out = append(out, diffCount(EventDiskCountChanged, len(before.Disks), len(after.Disks), "磁盘")...)
	if len(before.Disks) == len(after.Disks) {
		out = append(out, diffSerialSwap(EventDiskSerialSwapped, "磁盘", diskSerials(before.Disks), diskSerials(after.Disks))...)
	}

	out = append(out, diffCount(EventMemoryCountChanged, len(before.MemoryModules), len(after.MemoryModules), "内存条")...)
	if len(before.MemoryModules) == len(after.MemoryModules) {
		out = append(out, diffSerialSwap(EventMemorySerialSwapped, "内存", memSerials(before.MemoryModules), memSerials(after.MemoryModules))...)
	}

	out = append(out, diffCount(EventCPUChanged, len(before.CPU), len(after.CPU), "CPU")...)
	if len(before.CPU) > 0 && len(after.CPU) > 0 && before.CPU[0].Model != after.CPU[0].Model {
		out = append(out, Event{
			Kind:     EventCPUChanged,
			Severity: SeverityWarning,
			Message:  fmt.Sprintf("CPU 型号变更: %s → %s", before.CPU[0].Model, after.CPU[0].Model),
			Detail:   map[string]string{"before": before.CPU[0].Model, "after": after.CPU[0].Model},
		})
	}

	out = append(out, diffCount(EventGPUCountChanged, len(before.GPUs), len(after.GPUs), "GPU")...)
	out = append(out, diffCount(EventNICCountChanged, len(before.NICs), len(after.NICs), "网卡")...)

	return out
}

func diffCount(kind string, before, after int, label string) []Event {
	if before == after {
		return nil
	}
	return []Event{{
		Kind:     kind,
		Severity: SeverityWarning,
		Message:  fmt.Sprintf("%s数量变更: %d → %d", label, before, after),
		Detail:   map[string]string{"before": fmt.Sprint(before), "after": fmt.Sprint(after)},
	}}
}

func diffSerialSwap(kind, label string, before, after []string) []Event {
	beforeSet := toSet(before)
	afterSet := toSet(after)
	var removed, added []string
	for s := range beforeSet {
		if !afterSet[s] {
			removed = append(removed, s)
		}
	}
	for s := range afterSet {
		if !beforeSet[s] {
			added = append(added, s)
		}
	}
	if len(removed) == 0 && len(added) == 0 {
		return nil
	}
	if len(removed) == len(added) && len(removed) == 1 {
		return []Event{{
			Kind:     kind,
			Severity: SeverityWarning,
			Message:  fmt.Sprintf("%s序列号更换: %s → %s", label, removed[0], added[0]),
			Detail:   map[string]string{"old_serial": removed[0], "new_serial": added[0]},
		}}
	}
	msg := fmt.Sprintf("%s配置变更: %d 换 %d", label, len(removed), len(added))
	return []Event{{
		Kind:     kind + "_bulk",
		Severity: SeverityWarning,
		Message:  msg,
		Detail:   map[string]string{"removed": fmt.Sprint(removed), "added": fmt.Sprint(added)},
	}}
}

func toSet(xs []string) map[string]bool {
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		if x != "" {
			m[x] = true
		}
	}
	return m
}

func diskSerials(d []protocol.Disk) []string {
	out := make([]string, 0, len(d))
	for _, x := range d {
		out = append(out, x.Serial)
	}
	return out
}

func memSerials(d []protocol.MemoryModule) []string {
	out := make([]string, 0, len(d))
	for _, x := range d {
		out = append(out, x.Serial)
	}
	return out
}
