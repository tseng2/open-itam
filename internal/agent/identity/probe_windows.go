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

func probeFirstMAC() (string, error) {
	return runPS(`(Get-NetAdapter -Physical | Where-Object Status -eq 'Up' | Select-Object -First 1).MacAddress`)
}

func PlatformProbe() ProbeFuncs {
	return ProbeFuncs{
		BaseboardSerial: probeBaseboardSerial,
		FirstMAC:        probeFirstMAC,
	}
}
