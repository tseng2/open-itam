//go:build windows

package collector

import (
	"golang.org/x/sys/windows/registry"

	"itagent/internal/shared/protocol"
)

func collectSoftwareWindows() ([]protocol.Software, error) {
	roots := []struct {
		key  registry.Key
		path string
	}{
		{registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`},
		{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`},
		{registry.CURRENT_USER, `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`},
	}
	seen := map[string]bool{}
	var out []protocol.Software
	for _, r := range roots {
		k, err := registry.OpenKey(r.key, r.path, registry.READ|registry.ENUMERATE_SUB_KEYS)
		if err != nil {
			continue
		}
		names, _ := k.ReadSubKeyNames(-1)
		for _, name := range names {
			sub, err := registry.OpenKey(k, name, registry.READ)
			if err != nil {
				continue
			}
			display, _, _ := sub.GetStringValue("DisplayName")
			if display == "" {
				sub.Close()
				continue
			}
			version, _, _ := sub.GetStringValue("DisplayVersion")
			path, _, _ := sub.GetStringValue("InstallLocation")
			sub.Close()
			key := display + "|" + version
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, protocol.Software{Name: display, Version: version, InstallPath: path})
		}
		k.Close()
	}
	return out, nil
}
