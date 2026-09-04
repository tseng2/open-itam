//go:build !windows

package collector

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"time"

	"itagent/internal/shared/protocol"
)

func shellJSON(out any, cmd string, args ...string) error {
	data, err := exec.Command(cmd, args...).Output()
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func New() *Collector {
	return &Collector{
		OS: func() (protocol.OSInfo, error) {
			var v struct {
				ProductVersion string `json:"ProductVersion"`
			}
			_ = shellJSON(&v, "sw_vers", "-json") // older macOS lacks -json; fallback parse below
			if v.ProductVersion == "" {
				data, err := exec.Command("sw_vers", "-productVersion").Output()
				if err != nil {
					return protocol.OSInfo{}, err
				}
				v.ProductVersion = strings.TrimSpace(string(data))
			}
			return protocol.OSInfo{Name: "macOS", Version: v.ProductVersion}, nil
		},
		Identity: func() (string, string, error) {
			h, err := os.Hostname()
			return strings.TrimSuffix(h, ".local"), "", err
		},
		Logon: func() (protocol.LogonInfo, error) {
			out, err := exec.Command("stat", "-f", "%Su", "/dev/console").Output()
			if err != nil {
				return protocol.LogonInfo{}, err
			}
			user := strings.TrimSpace(string(out))
			if user == "root" {
				user = ""
			}
			return protocol.LogonInfo{User: user, Type: protocol.LogonTypeLocal, LogonAt: time.Now()}, nil
		},
		Boot: func() (time.Time, int64, error) {
			out, err := exec.Command("sysctl", "-n", "kern.boottime").Output()
			if err != nil {
				return time.Time{}, 0, err
			}
			line := string(out)
			idx := strings.Index(line, "sec = ")
			if idx < 0 {
				return time.Time{}, 0, err
			}
			var sec int64
			rest := line[idx+6:]
			for i, c := range rest {
				if c < '0' || c > '9' {
					rest = rest[:i]
					break
				}
			}
			for _, c := range rest {
				sec = sec*10 + int64(c-'0')
			}
			boot := time.Unix(sec, 0)
			return boot, int64(time.Since(boot).Seconds()), nil
		},
	}
}
