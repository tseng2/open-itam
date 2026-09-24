package api

import (
	"fmt"
	"sort"
	"strings"

	"itagent/internal/shared/protocol"
)

// compareHardware 比对基线与当前上报的硬件画像（阶段五 A3）：
// 内存总量 / 内置磁盘数量 / 磁盘序列号集合 / CPU 数量与型号集合。
// 返回人类可读的变更描述列表（固定顺序），空切片表示无变化。
// U 盘等可移动介质不参与比对（日常插拔不是硬件变更）；
// 空序列号（垃圾值/未采集）不参与集合比对，防止噪声误报。
func compareHardware(base, cur protocol.Hardware) []string {
	var diff []string

	baseMemGB := base.MemoryTotalMB / 1024
	curMemGB := cur.MemoryTotalMB / 1024
	if baseMemGB != curMemGB {
		diff = append(diff, fmt.Sprintf("内存: %dGB -> %dGB", baseMemGB, curMemGB))
	}

	baseDisks := fixedDisks(base.Disks)
	curDisks := fixedDisks(cur.Disks)
	if len(baseDisks) != len(curDisks) {
		diff = append(diff, fmt.Sprintf("内置磁盘数量: %d -> %d", len(baseDisks), len(curDisks)))
	} else if removed, added := diffSerialSets(baseDisks, curDisks); len(removed) > 0 || len(added) > 0 {
		// 换盘检测：数量相同仅序列号变化（数量变化时 SN 集合差必然随之发生，报数量即可）
		diff = append(diff, fmt.Sprintf("磁盘更换: 移除[%s], 新增[%s]",
			strings.Join(removed, ", "), strings.Join(added, ", ")))
	}

	if len(base.CPU) != len(cur.CPU) {
		diff = append(diff, fmt.Sprintf("CPU数量: %d -> %d", len(base.CPU), len(cur.CPU)))
	} else if removed, added := diffCPUModels(base.CPU, cur.CPU); len(removed) > 0 || len(added) > 0 {
		diff = append(diff, fmt.Sprintf("CPU型号变更: [%s] -> [%s]",
			strings.Join(removed, ", "), strings.Join(added, ", ")))
	}

	return diff
}

// fixedDisks 过滤可移动介质，仅保留内置磁盘参与变更比对
func fixedDisks(disks []protocol.Disk) []protocol.Disk {
	out := make([]protocol.Disk, 0, len(disks))
	for _, d := range disks {
		if !d.Removable {
			out = append(out, d)
		}
	}
	return out
}

// diffSerialSets 内置磁盘序列号集合差：数量/容量相同仅 SN 变化的换盘检测。
// 任一侧存在空序列号即视为采集不完整，跳过本轮 SN 比对——
// 宁漏报不误报，漏报场景由下次完整采集补上
func diffSerialSets(base, cur []protocol.Disk) (removed, added []string) {
	baseSet := make(map[string]bool, len(base))
	for _, d := range base {
		if d.Serial == "" {
			return nil, nil
		}
		baseSet[d.Serial] = true
	}
	curSet := make(map[string]bool, len(cur))
	for _, d := range cur {
		if d.Serial == "" {
			return nil, nil
		}
		curSet[d.Serial] = true
	}
	for sn := range baseSet {
		if !curSet[sn] {
			removed = append(removed, sn)
		}
	}
	for sn := range curSet {
		if !baseSet[sn] {
			added = append(added, sn)
		}
	}
	sort.Strings(removed)
	sort.Strings(added)
	return removed, added
}

// diffCPUModels CPU 型号集合差（更换 CPU / 超线程重识别等），空型号不参与
func diffCPUModels(base, cur []protocol.CPU) (removed, added []string) {
	baseSet := make(map[string]bool, len(base))
	for _, c := range base {
		if c.Model != "" {
			baseSet[c.Model] = true
		}
	}
	curSet := make(map[string]bool, len(cur))
	for _, c := range cur {
		if c.Model != "" {
			curSet[c.Model] = true
		}
	}
	for m := range baseSet {
		if !curSet[m] {
			removed = append(removed, m)
		}
	}
	for m := range curSet {
		if !baseSet[m] {
			added = append(added, m)
		}
	}
	sort.Strings(removed)
	sort.Strings(added)
	return removed, added
}
