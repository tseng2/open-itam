//go:build windows

package identity

import (
	"os/exec"
	"strings"
)

func runPS(script string) (string, error) {
	out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func probeBaseboardSerial() (string, error) {
	return runPS(`(Get-CimInstance Win32_BaseBoard).SerialNumber`)
}

func probeBIOSUUID() (string, error) {
	return runPS(`(Get-CimInstance Win32_ComputerSystemProduct).UUID`)
}

// 身份锚取全部物理网卡而非"第一张活动网卡"：网口切换、WiFi/有线互换、
// 网卡 Up/Down 状态变化都不影响集合内容
func probeAllMACs() ([]string, error) {
	out, err := runPS(`(Get-CimInstance Win32_NetworkAdapter | Where-Object { $_.PhysicalAdapter -and $_.MACAddress }).MacAddress`)
	if err != nil {
		return nil, err
	}
	var macs []string
	for _, line := range strings.Split(out, "\n") {
		if m := strings.TrimSpace(line); m != "" {
			macs = append(macs, m)
		}
	}
	return macs, nil
}

func PlatformProbe() ProbeFuncs {
	return ProbeFuncs{
		BaseboardSerial: probeBaseboardSerial,
		BIOSUUID:        probeBIOSUUID,
		AllMACs:         probeAllMACs,
	}
}
