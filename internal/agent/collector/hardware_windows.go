//go:build windows

package collector

import (
	"itagent/internal/shared/protocol"
)

const hardwareScript = `
$cs = Get-CimInstance Win32_ComputerSystem
$csp = Get-CimInstance Win32_ComputerSystemProduct
$bios = Get-CimInstance Win32_BIOS
$cpus = @(Get-CimInstance Win32_Processor | ForEach-Object {
    @{ model = $_.Name.Trim(); cores = [int]$_.NumberOfCores; threads = [int]$_.NumberOfLogicalProcessors }
})
$mems = @(Get-CimInstance Win32_PhysicalMemory | ForEach-Object {
    @{ slot = $_.DeviceLocator; size_mb = [int64]($_.Capacity / 1MB); type = ('','','DDR2','DDR3','DDR4','','','','','','','','','','','','','','','','','','','','','DDR5')[[Math]::Min([int]$_.SMBIOSMemoryType,26)]; speed_mhz = [int]$_.Speed; serial = ('' + $_.SerialNumber).Trim() }
})
$disks = @(Get-CimInstance Win32_DiskDrive | ForEach-Object {
    $mt = $_.MediaType
    $t = 'HDD'
    if ($_.Model -match 'NVMe') { $t = 'NVMe' } elseif ($mt -match 'SSD') { $t = 'SSD' }
    @{ model = $_.Model; size_gb = [int64]($_.Size / 1GB); type = $t; serial = ('' + $_.SerialNumber).Trim(); removable = ($_.MediaType -match 'Removable') }
})
$gpus = @(Get-CimInstance Win32_VideoController | ForEach-Object {
    @{ model = $_.Name; vram_mb = [int64]($_.AdapterRAM / 1MB) }
})
$nics = @(Get-CimInstance Win32_NetworkAdapter | Where-Object { $_.PhysicalAdapter -and $_.MACAddress } | ForEach-Object {
    @{ name = $_.Name; mac = $_.MACAddress; speed_mbps = [int64]($_.Speed / 1MB) }
})
@{
    brand = $cs.Manufacturer
    model = $cs.Model
    serial = ('' + $csp.IdentifyingNumber).Trim()
    bios_serial = ('' + $bios.SerialNumber).Trim()
    cpu = $cpus
    memory_total_mb = [int64]($cs.TotalPhysicalMemory / 1MB)
    memory_modules = $mems
    disks = $disks
    gpus = $gpus
    nics = $nics
} | ConvertTo-Json -Compress -Depth 4
`

func collectHardwareWindows() (protocol.Hardware, error) {
	var hw protocol.Hardware
	if err := runPSJSON(hardwareScript, &hw); err != nil {
		return protocol.Hardware{}, err
	}
	hw.SMART = collectSMART(hw.Disks)
	return hw, nil
}
