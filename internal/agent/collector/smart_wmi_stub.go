//go:build !windows

package collector

import "itagent/internal/shared/protocol"

func collectSMARTWMI(disks []protocol.Disk) []protocol.SmartHealth {
	return nil
}
