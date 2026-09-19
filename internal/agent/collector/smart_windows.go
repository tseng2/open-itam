//go:build windows

package collector

import (
	"strings"

	"itagent/internal/shared/protocol"
)

type smartWMIResult struct {
	Instance       string `json:"instance"`
	PredictFailure bool   `json:"predict_failure"`
	Realloc        int64  `json:"realloc"`
	Pending        int64  `json:"pending"`
	PowerOnHours   int64  `json:"power_on_hours"`
	TemperatureC   int    `json:"temperature_c"`
	Wear231        int64  `json:"wear231"`
	Wear233        int64  `json:"wear233"`
}

const smartWMIScript = `
$status = @{}
Get-CimInstance -Namespace root\wmi -ClassName MSStorageDriver_FailurePredictStatus -ErrorAction SilentlyContinue | ForEach-Object {
    $status[$_.InstanceName] = [bool]$_.PredictFailure
}
$out = @()
Get-CimInstance -Namespace root\wmi -ClassName MSStorageDriver_FailurePredictData -ErrorAction SilentlyContinue | ForEach-Object {
    $vs = $_.VendorSpecific
    if (-not $vs -or $vs.Length -lt 362) { return }
    $attrs = @{}
    for ($i = 2; $i -le 361; $i += 12) {
        $id = [int]$vs[$i]
        if ($id -eq 0) { continue }
        $raw = [int64][BitConverter]::ToUInt32($vs, $i + 5)
        $attrs[$id] = $raw
    }
    $out += @{
        instance = $_.InstanceName
        predict_failure = [bool]$status[$_.InstanceName]
        realloc = [int64]$attrs[5]
        pending = [int64]$attrs[197]
        power_on_hours = [int64]$attrs[9]
        temperature_c = [int]$attrs[194]
        wear231 = [int64]$attrs[231]
        wear233 = [int64]$attrs[233]
    }
}
if ($out.Count -eq 1) { $out = @($out) }
ConvertTo-Json -Compress -InputObject @($out)
`

func collectSMARTWMI(disks []protocol.Disk) []protocol.SmartHealth {
	var rows []smartWMIResult
	_ = runPSJSON(smartWMIScript, &rows)
	var out []protocol.SmartHealth
	for _, r := range rows {
		h := protocol.SmartHealth{
			OverallHealth:       "PASSED",
			ReallocatedSectors:  r.Realloc,
			PendingSectors:      r.Pending,
			PowerOnHours:        r.PowerOnHours,
			TemperatureC:        r.TemperatureC,
			PercentLifetimeUsed: lifetimeUsed(r.Wear231, r.Wear233),
		}
		if r.PredictFailure {
			h.OverallHealth = "FAILED"
		}
		h.Model, h.DiskSerial = matchDisk(r.Instance, disks)
		out = append(out, h)
	}
	if len(out) == 0 && len(disks) > 0 {
		for _, d := range disks {
			out = append(out, protocol.SmartHealth{
				Model:         d.Model,
				DiskSerial:    d.Serial,
				OverallHealth: "PASSED",
			})
		}
	}
	return out
}

// 231/233 多数厂商表示剩余寿命百分比（normalized raw），转换成已用百分比
func lifetimeUsed(a, b int64) int {
	for _, v := range []int64{a, b} {
		if v >= 1 && v <= 100 {
			return 100 - int(v)
		}
	}
	return 0
}

func matchDisk(instance string, disks []protocol.Disk) (model, serial string) {
	norm := strings.ToUpper(strings.NewReplacer(" ", "", "_", "", ".", "", "-", "").Replace(instance))
	for _, d := range disks {
		m := strings.ToUpper(strings.NewReplacer(" ", "", "_", "", ".", "", "-", "").Replace(d.Model))
		if m != "" && strings.Contains(norm, m) {
			return d.Model, d.Serial
		}
	}
	return "", ""
}
