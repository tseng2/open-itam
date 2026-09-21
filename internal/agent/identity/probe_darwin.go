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

// macOS 以 IOPlatformUUID 作为整机身份锚点（对应 Windows 的 BIOS UUID）
func probePlatformUUID() (string, error) {
	return shell("sh", "-c", `ioreg -rd1 -c IOPlatformExpertDevice | awk -F'"' '/IOPlatformUUID/{print $4}'`)
}

// 取全部物理网卡 MAC 而非第一张活动网卡，网口/WiFi 切换不影响集合
func probeAllMACs() ([]string, error) {
	out, err := shell("sh", "-c", `ifconfig | awk '/ether /{print $2}'`)
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
		BIOSUUID: probePlatformUUID,
		AllMACs:  probeAllMACs,
	}
}
