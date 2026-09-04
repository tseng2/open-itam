//go:build !windows

package identity

import (
	"os/exec"
	"strings"
)

func shell(cmd string, args ...string) (string, error) {
	out, err := exec.Command(cmd, args...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func probePlatformUUID() (string, error) {
	return shell("sh", "-c", `ioreg -rd1 -c IOPlatformExpertDevice | awk -F'"' '/IOPlatformUUID/{print $4}'`)
}

func probeFirstMAC() (string, error) {
	return shell("sh", "-c", `ifconfig | awk '/flags=.*UP/ && !/LOOPBACK/{iface=$1} /ether /{print $2; exit}'`)
}

func PlatformProbe() ProbeFuncs {
	return ProbeFuncs{
		BaseboardSerial: probePlatformUUID,
		FirstMAC:        probeFirstMAC,
	}
}
