//go:build windows

package collector

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"itagent/internal/shared/protocol"
)

func runPSJSON(script string, out any) error {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
		"[Console]::OutputEncoding=[Text.Encoding]::UTF8;"+script)
	data, err := cmd.Output()
	if err != nil {
		return err
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil
	}
	return json.Unmarshal([]byte(trimmed), out)
}

func osProvider() (protocol.OSInfo, error) {
	var r struct {
		Caption     string `json:"Caption"`
		Version     string `json:"Version"`
		BuildNumber string `json:"BuildNumber"`
	}
	err := runPSJSON(`Get-CimInstance Win32_OperatingSystem | Select-Object Caption,Version,BuildNumber | ConvertTo-Json -Compress`, &r)
	return protocol.OSInfo{Name: strings.TrimSpace(r.Caption), Version: r.Version, Build: r.BuildNumber}, err
}

func identityProvider() (string, string, error) {
	host, err := os.Hostname()
	return host, "", err
}

func logonProvider() (protocol.LogonInfo, error) {
	var cs struct {
		UserName     string `json:"UserName"`
		Domain       string `json:"Domain"`
		PartOfDomain bool   `json:"PartOfDomain"`
	}
	if err := runPSJSON(`Get-CimInstance Win32_ComputerSystem | Select-Object UserName,Domain,PartOfDomain | ConvertTo-Json -Compress`, &cs); err != nil {
		return protocol.LogonInfo{}, err
	}
	info := protocol.LogonInfo{LogonAt: time.Now()}
	if !cs.PartOfDomain {
		info.Type = protocol.LogonTypeLocal
	} else {
		info.Type = protocol.LogonTypeAD
	}
	if cs.UserName != "" {
		parts := strings.SplitN(cs.UserName, `\`, 2)
		if len(parts) == 2 {
			info.Domain = parts[0]
			info.User = parts[1]
		} else {
			info.User = cs.UserName
			info.Domain = cs.Domain
		}
	}
	var last struct {
		LastLoggedOnUser     string `json:"LastLoggedOnUser"`
		LastLoggedOnSAMUser  string `json:"LastLoggedOnSAMUser"`
		LastLoggedOnProvider string `json:"LastLoggedOnProvider"`
	}
	_ = runPSJSON(`Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Authentication\LogonUI' -ErrorAction SilentlyContinue | Select-Object LastLoggedOnUser,LastLoggedOnSAMUser,LastLoggedOnProvider | ConvertTo-Json -Compress`, &last)
	if info.User == "" {
		raw := last.LastLoggedOnSAMUser
		if raw == "" {
			raw = last.LastLoggedOnUser
		}
		if parts := strings.SplitN(raw, `\`, 2); len(parts) == 2 {
			info.Domain = parts[0]
			info.User = parts[1]
		} else {
			info.User = raw
		}
	}
	return info, nil
}

func bootProvider() (time.Time, int64, error) {
	var r struct {
		LastBootUpTime string `json:"LastBootUpTime"`
	}
	err := runPSJSON(`Get-CimInstance Win32_OperatingSystem | Select-Object @{n='LastBootUpTime';e={$_.LastBootUpTime.ToString('o')}} | ConvertTo-Json -Compress`, &r)
	if err != nil {
		return time.Time{}, 0, err
	}
	boot, err := time.Parse(time.RFC3339Nano, r.LastBootUpTime)
	if err != nil {
		return time.Time{}, 0, err
	}
	return boot, int64(time.Since(boot).Seconds()), nil
}

func networkProvider() (protocol.Network, error) {
	var nets []protocol.NetInterface
	ifaces, err := net.Interfaces()
	if err == nil {
		for _, i := range ifaces {
			if i.Flags&net.FlagLoopback != 0 {
				continue
			}
			ni := protocol.NetInterface{Name: i.Name, MAC: i.HardwareAddr.String()}
			ni.IsUp = i.Flags&net.FlagUp != 0
			addrs, _ := i.Addrs()
			for _, a := range addrs {
				ni.IPs = append(ni.IPs, a.String())
			}
			nets = append(nets, ni)
		}
	}
	pub := fetchPublicIP(3 * time.Second)
	return protocol.Network{Interfaces: nets, PublicIP: pub}, nil
}

func fetchPublicIP(timeout time.Duration) string {
	client := &http.Client{Timeout: timeout}
	for _, url := range []string{"https://api.ipify.org", "https://ifconfig.me/ip"} {
		resp, err := client.Get(url)
		if err != nil {
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 64))
		resp.Body.Close()
		if err != nil {
			continue
		}
		if ip := strings.TrimSpace(string(body)); net.ParseIP(ip) != nil {
			return ip
		}
	}
	return ""
}

func HardwareProvider() protocol.Hardware {
	hw, _ := collectHardwareWindows()
	return hw
}

func SoftwareProvider() []protocol.Software {
	sw, _ := collectSoftwareWindows()
	return sw
}

func New() *Collector {
	return &Collector{
		OS:       osProvider,
		Logon:    logonProvider,
		Identity: identityProvider,
		Network:  networkProvider,
		Boot:     bootProvider,
		Hardware: func() (protocol.Hardware, error) { return collectHardwareWindows() },
		Software: func() ([]protocol.Software, error) { return collectSoftwareWindows() },
	}
}
