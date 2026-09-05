package collector

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"itagent/internal/shared/protocol"
)

type smartctlOutput struct {
	ModelName    string `json:"model_name"`
	SerialNumber string `json:"serial_number"`
	SmartStatus  struct {
		Passed *bool `json:"passed"`
	} `json:"smart_status"`
	PowerOnTime struct {
		Hours int64 `json:"hours"`
	} `json:"power_on_time"`
	Temperature struct {
		Current int `json:"current"`
	} `json:"temperature"`
	NVMeSmartHealthInformationLog struct {
		AvailableSpare  int   `json:"available_spare"`
		PercentageUsed  int   `json:"percentage_used"`
		MediaErrors     int64 `json:"media_errors"`
		CriticalWarning int   `json:"critical_warning"`
		UnsafeShutdowns int64 `json:"unsafe_shutdowns"`
	} `json:"nvme_smart_health_information_log"`
	ATASmartAttributes struct {
		Table []struct {
			ID    int    `json:"id"`
			Name  string `json:"name"`
			Value int    `json:"value"`
			When  string `json:"when_failed"`
			Raw   struct {
				Value  int64  `json:"value"`
				String string `json:"string"`
			} `json:"raw"`
		} `json:"table"`
	} `json:"ata_smart_attributes"`
	Device struct {
		Type string `json:"type"`
	} `json:"device"`
}

// 优先用打包附带的 smartctl.exe（tools 目录），其次 PATH，都没有则 WMI 兜底
func findSmartctl() string {
	self, err := os.Executable()
	if err == nil {
		p := filepath.Join(filepath.Dir(self), "..", "tools", "smartctl.exe")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath("smartctl"); err == nil {
		return p
	}
	return ""
}

func collectSMART(disks []protocol.Disk) []protocol.SmartHealth {
	bin := findSmartctl()
	if bin == "" {
		return collectSMARTWMI(disks)
	}
	var out []protocol.SmartHealth
	devices := smartScanDevices(bin)
	if len(devices) == 0 {
		return collectSMARTWMI(disks)
	}
	seen := map[string]bool{}
	for _, dev := range devices {
		if h, ok := smartQuery(bin, dev); ok {
			key := h.DiskSerial + "|" + h.Model
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, h)
		}
	}
	return out
}

func smartScanDevices(bin string) []string {
	out, err := exec.Command(bin, "--scan", "-j").Output()
	if err != nil {
		return nil
	}
	var scan struct {
		Devices []struct {
			Name string `json:"name"`
		} `json:"devices"`
	}
	if json.Unmarshal(out, &scan) != nil {
		return nil
	}
	var devs []string
	for _, d := range scan.Devices {
		devs = append(devs, d.Name)
	}
	return devs
}

func smartQuery(bin, dev string) (protocol.SmartHealth, bool) {
	out, err := exec.Command(bin, "-a", "-j", dev).Output()
	if err != nil && len(out) == 0 {
		return protocol.SmartHealth{}, false
	}
	var s smartctlOutput
	if json.Unmarshal(out, &s) != nil {
		return protocol.SmartHealth{}, false
	}
	h := protocol.SmartHealth{
		DiskSerial:          s.SerialNumber,
		Model:               s.ModelName,
		OverallHealth:       healthString(s),
		PowerOnHours:        s.PowerOnTime.Hours,
		TemperatureC:        s.Temperature.Current,
		PercentLifetimeUsed: s.NVMeSmartHealthInformationLog.PercentageUsed,
	}
	for _, attr := range s.ATASmartAttributes.Table {
		switch attr.ID {
		case 5:
			h.ReallocatedSectors = attr.Raw.Value
		case 197:
			h.PendingSectors = attr.Raw.Value
		}
	}
	h.DiskSerial = strings.TrimSpace(h.DiskSerial)
	return h, true
}

func healthString(s smartctlOutput) string {
	if s.SmartStatus.Passed == nil {
		if s.NVMeSmartHealthInformationLog.CriticalWarning != 0 {
			return "FAILED"
		}
		return "PASSED"
	}
	if *s.SmartStatus.Passed {
		return "PASSED"
	}
	return "FAILED"
}
