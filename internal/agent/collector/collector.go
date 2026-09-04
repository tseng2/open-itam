package collector

import (
	"time"

	"itagent/internal/shared/protocol"
)

type Collector struct {
	OS       func() (protocol.OSInfo, error)
	Logon    func() (protocol.LogonInfo, error)
	Identity func() (hostname, domain string, err error)
	Network  func() (protocol.Network, error)
	Boot     func() (bootTime time.Time, uptimeSec int64, err error)
	Hardware func() (protocol.Hardware, error)
	Software func() ([]protocol.Software, error)
}

func (c *Collector) Heartbeat() protocol.HeartbeatPayload {
	var hb protocol.HeartbeatPayload
	if c.Identity != nil {
		hb.Hostname, _, _ = c.Identity()
	}
	if c.OS != nil {
		hb.OS, _ = c.OS()
	}
	if c.Logon != nil {
		hb.Logon, _ = c.Logon()
	}
	if c.Boot != nil {
		hb.BootTime, hb.UptimeSec, _ = c.Boot()
	}
	if c.Network != nil {
		hb.Network, _ = c.Network()
	}
	return hb
}

func (c *Collector) Full() protocol.FullPayload {
	full := protocol.FullPayload{HeartbeatPayload: c.Heartbeat()}
	if c.Hardware != nil {
		full.Hardware, _ = c.Hardware()
	}
	if c.Software != nil {
		full.Software, _ = c.Software()
	}
	return full
}
