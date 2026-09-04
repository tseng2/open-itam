package collector

import (
	"errors"
	"testing"
	"time"

	"itagent/internal/shared/protocol"
)

func fakeCollector() *Collector {
	return &Collector{
		OS: func() (protocol.OSInfo, error) {
			return protocol.OSInfo{Name: "Windows 11 Pro", Version: "23H2", Build: "22631"}, nil
		},
		Logon: func() (protocol.LogonInfo, error) {
			return protocol.LogonInfo{User: "zs", Domain: "DG", Type: protocol.LogonTypeAD, LogonAt: time.Now()}, nil
		},
		Identity: func() (string, string, error) { return "HOST-01", "", nil },
		Network: func() (protocol.Network, error) {
			return protocol.Network{Interfaces: []protocol.NetInterface{{Name: "eth0", IsUp: true}}, PublicIP: "1.2.3.4"}, nil
		},
		Boot: func() (time.Time, int64, error) { return time.Now().Add(-time.Hour), 3600, nil },
		Hardware: func() (protocol.Hardware, error) {
			return protocol.Hardware{
				Brand: "Dell Inc.", Model: "OptiPlex 7010", Serial: "7XKQ1P3", BIOSSerial: "7XKQ1P3",
				MemoryTotalMB: 32768,
				Disks:         []protocol.Disk{{Serial: "SN1", Type: "NVMe"}},
			}, nil
		},
		Software: func() ([]protocol.Software, error) {
			return []protocol.Software{{Name: "Office", Version: "16.0"}}, nil
		},
	}
}

func TestHeartbeatAssemblesAllFields(t *testing.T) {
	c := fakeCollector()
	hb := c.Heartbeat()
	if hb.Hostname != "HOST-01" {
		t.Fatalf("hostname missing: %q", hb.Hostname)
	}
	if hb.OS.Name == "" || hb.Logon.User != "zs" || hb.UptimeSec != 3600 {
		t.Fatalf("heartbeat incomplete: %+v", hb)
	}
	if hb.Network.PublicIP != "1.2.3.4" {
		t.Fatalf("public ip missing: %+v", hb.Network)
	}
}

func TestHeartbeatToleratesPartialFailure(t *testing.T) {
	c := fakeCollector()
	c.Logon = func() (protocol.LogonInfo, error) { return protocol.LogonInfo{}, errors.New("no session") }
	c.Network = func() (protocol.Network, error) { return protocol.Network{}, errors.New("offline") }
	hb := c.Heartbeat()
	if hb.Hostname != "HOST-01" || hb.OS.Name == "" {
		t.Fatal("available fields must survive partial failure")
	}
	if hb.Logon.User != "" {
		t.Fatal("failed provider field must stay zero")
	}
}

func TestFullIncludesHardwareAndSoftware(t *testing.T) {
	c := fakeCollector()
	full := c.Full()
	if full.Hardware.Serial != "7XKQ1P3" || full.Hardware.BIOSSerial != "7XKQ1P3" {
		t.Fatalf("serial fields missing: %+v", full.Hardware)
	}
	if len(full.Hardware.Disks) != 1 || full.Software[0].Name != "Office" {
		t.Fatalf("full payload incomplete: %+v", full)
	}
	if full.Hostname != "HOST-01" {
		t.Fatal("full payload must embed heartbeat fields")
	}
}
