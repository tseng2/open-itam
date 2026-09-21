package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"itagent/internal/shared/hwfilter"
)

// Bundle 终端身份特征包：指纹的完整输入，也随注册请求上报服务端用于归属调和
type Bundle struct {
	BoardSerial string   // 主板序列号（垃圾值已过滤为空）
	BIOSUUID    string   // BIOS/整机 UUID（垃圾值已过滤为空）
	MACs        []string // 全部物理网卡 MAC，大写冒号格式，已排序
}

// Fingerprint 由特征包计算终端指纹：任一单点硬件变化（换网卡/换主板）在
// 配置丢失重算时仍有其他锚点可调和，日常运行只依赖本地持久化的结果
func (b Bundle) Fingerprint() string {
	raw := b.BoardSerial + "|" + b.BIOSUUID + "|" + strings.Join(b.MACs, ",")
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])[:16]
}

// Usable 判断特征包是否足以生成稳定身份
func (b Bundle) Usable() bool {
	return b.BoardSerial != "" || b.BIOSUUID != "" || len(b.MACs) > 0
}

// ProbeFuncs 各平台探针。BIOSUUID/AllMACs 为可选：缺省时退化为旧的两锚点行为
type ProbeFuncs struct {
	BaseboardSerial func() (string, error)
	FirstMAC        func() (string, error) // 兼容字段：AllMACs 缺省时使用
	BIOSUUID        func() (string, error)
	AllMACs         func() ([]string, error)
}

// CollectBundle 采集并规范化身份特征包
func CollectBundle(p ProbeFuncs) Bundle {
	var b Bundle
	if p.BaseboardSerial != nil {
		serial, _ := p.BaseboardSerial()
		serial = strings.TrimSpace(serial)
		if !hwfilter.IsGarbageSerial(serial) {
			b.BoardSerial = serial
		}
	}
	if p.BIOSUUID != nil {
		uuid, _ := p.BIOSUUID()
		uuid = strings.TrimSpace(uuid)
		if !hwfilter.IsGarbageUUID(uuid) {
			b.BIOSUUID = strings.ToUpper(uuid)
		}
	}
	if p.AllMACs != nil {
		macs, _ := p.AllMACs()
		for _, m := range macs {
			if n := normalizeMAC(m); n != "" {
				b.MACs = append(b.MACs, n)
			}
		}
	} else if p.FirstMAC != nil {
		mac, _ := p.FirstMAC()
		if n := normalizeMAC(mac); n != "" {
			b.MACs = []string{n}
		}
	}
	sort.Strings(b.MACs)
	b.MACs = dedupeStrings(b.MACs)
	return b
}

// DeviceID 采集特征包并计算指纹。指纹只在首次安装时计算一次，
// 之后由调用方持久化（配置文件 + 系统注册表），日常启动不重算
func DeviceID(p ProbeFuncs) (string, error) {
	b := CollectBundle(p)
	if !b.Usable() {
		return "", fmt.Errorf("cannot derive device id: no usable hardware identifier")
	}
	return b.Fingerprint(), nil
}

func normalizeMAC(mac string) string {
	mac = strings.TrimSpace(mac)
	if mac == "" {
		return ""
	}
	return strings.ToUpper(strings.ReplaceAll(mac, "-", ":"))
}

func dedupeStrings(in []string) []string {
	out := in[:0]
	var prev string
	for i, v := range in {
		if i == 0 || v != prev {
			out = append(out, v)
			prev = v
		}
	}
	return out
}
