// Package hwfilter 提供硬件条目的过滤规则，Agent 采集端与服务端共用，
// 避免向日葵/ToDesk 等远控软件的虚拟设备混入台账与画像
package hwfilter

import "strings"

// 虚拟显示适配器名称特征（小写子串匹配）：
// 远程控制/投屏软件安装的虚拟显卡，不是物理硬件
var virtualDisplayPatterns = []string{
	"oray",            // 向日葵/花生壳（Oray IddDriver 等）
	"sunlogin",        // 向日葵英文标识
	"todesk",
	"anydesk",
	"teamviewer",
	"parsec",
	"iddcx",           // Windows 间接显示驱动框架（虚拟屏标志）
	"idd driver",
	"usb display",     // USB 外置显卡（DisplayLink 等扩展坞场景按需再放开）
	"virtual display",
	"rdp display",
	"indirect display",
}

// IsVirtualDisplay 判断显卡名称是否为虚拟显示适配器
func IsVirtualDisplay(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "" {
		return false
	}
	for _, p := range virtualDisplayPatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// 主板/整机序列号常见垃圾值：组装机厂商不写入，直接当无序列号处理
var garbageSerials = []string{
	"default string",
	"to be filled by o.e.m.",
	"system serial number",
	"none",
	"not specified",
	"unknown",
	"0",
}

// IsGarbageSerial 判断序列号是否为无意义的垃圾值
func IsGarbageSerial(s string) bool {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return true
	}
	lower := strings.ToLower(trimmed)
	for _, g := range garbageSerials {
		if lower == g {
			return true
		}
	}
	return false
}

// IsGarbageUUID 判断 BIOS UUID 是否为无意义的垃圾值（全 0 / 全 F 等占位）
func IsGarbageUUID(s string) bool {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return true
	}
	compact := strings.ReplaceAll(trimmed, "-", "")
	allZero, allF := true, true
	for _, c := range compact {
		if c != '0' {
			allZero = false
		}
		if c != 'f' && c != 'F' {
			allF = false
		}
	}
	return allZero || allF
}
