package alert

import (
	"fmt"

	"itagent/internal/shared/protocol"
)

const defaultTempThresholdC = 60
const defaultLifetimePercent = 80
const defaultTempStreak = 3

func CheckSMART(deviceID string, smart []protocol.SmartHealth) []Event {
	return CheckSMARTWithStreak(deviceID, smart, nil)
}

func CheckSMARTWithStreak(deviceID string, smart []protocol.SmartHealth, tempStreak map[string]int) []Event {
	var out []Event
	for _, s := range smart {
		key := s.DiskSerial
		if s.OverallHealth != "" && s.OverallHealth != "PASSED" {
			out = append(out, Event{
				Kind:     EventSmartHealthFailed,
				Severity: SeverityEmergency,
				Message:  fmt.Sprintf("磁盘整体健康失败: %s (%s)", s.Model, s.DiskSerial),
				Detail:   map[string]string{"disk_serial": key, "model": s.Model},
			})
		}
		if s.ReallocatedSectors > 0 {
			out = append(out, Event{
				Kind:     EventSmartReallocated,
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("磁盘发现 %d 个重映射扇区: %s", s.ReallocatedSectors, key),
				Detail:   map[string]string{"disk_serial": key, "model": s.Model, "count": fmt.Sprint(s.ReallocatedSectors)},
			})
		}
		if s.PendingSectors > 0 {
			out = append(out, Event{
				Kind:     EventSmartPending,
				Severity: SeverityCritical,
				Message:  fmt.Sprintf("磁盘存在 %d 个待决坏道(不稳定扇区): %s", s.PendingSectors, key),
				Detail:   map[string]string{"disk_serial": key, "model": s.Model, "count": fmt.Sprint(s.PendingSectors)},
			})
		}
		if s.PercentLifetimeUsed >= defaultLifetimePercent {
			out = append(out, Event{
				Kind:     EventSmartLifetime,
				Severity: SeverityInfo,
				Message:  fmt.Sprintf("磁盘寿命消耗 %d%% (≥%d%%): %s", s.PercentLifetimeUsed, defaultLifetimePercent, key),
				Detail:   map[string]string{"disk_serial": key, "model": s.Model},
			})
		}
		if s.TemperatureC >= defaultTempThresholdC {
			streak := 0
			if tempStreak != nil {
				streak = tempStreak[key]
			}
			if streak >= defaultTempStreak {
				out = append(out, Event{
					Kind:     EventSmartTemp,
					Severity: SeverityWarning,
					Message:  fmt.Sprintf("磁盘温度持续 %d°C 超阈值 %d 次: %s", s.TemperatureC, streak, key),
					Detail:   map[string]string{"disk_serial": key, "model": s.Model, "streak": fmt.Sprint(streak)},
				})
			}
		}
	}
	return out
}
