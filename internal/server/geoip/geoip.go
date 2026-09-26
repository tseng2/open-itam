// Package geoip 提供出口 IP → 国家/省份/城市的离线解析（漫游判定的地理维度）。
// 数据源 ip2region v4 离线库（单文件 xdb，纯 Go 读取零 CGO，go:embed
// 内嵌与 smartctl 先例一致——单二进制自包含，部署零外部依赖）；
// 库更新走 repo 换文件重编译，不做运行时加载。
//
// 设计边界（内外网判定契约）：GeoIP 只做「地理归属」维度的输出，
// 不判断在线/失联（那是心跳阈值的事）；解析失败/非中国 IP 的省城段
// 一律返回空串，由调用方降级为「跳过地理维度」（宁漏报不误报）。
// 市级颗粒度是库的能力上限：城市段缺失（库中记 '0'）时按省级宽口径
// 降级比对；同城家宽与公司内网无法区分（内网判定交与本机 IP 公私网维度）。
package geoip

import (
	"embed"
	"strings"
	"sync"

	"github.com/lionsoul2014/ip2region/binding/golang/xdb"
)

//go:embed ip2region_v4.xdb
var xdbFS embed.FS

// xdbPath 库文件放包根而非 data/ 目录：仓库根 .gitignore 的 `data/`
// 规则会误伤任意层级同名目录，embed 数据必须入库（否则 clone 编译不过）
const xdbPath = "ip2region_v4.xdb"

var (
	once     sync.Once
	searcher *xdb.Searcher
	mu       sync.Mutex // xdb Searcher 非线程安全，全局串行化（低频调用无竞争压力）
)

// load 包级单例初始化：embed 内容进内存，失败只发生一次且永久降级（全部返回空）
func load() {
	once.Do(func() {
		content, err := xdb.LoadContentFromFS(xdbFS, xdbPath)
		if err != nil {
			return
		}
		header, err := xdb.LoadHeaderFromBuff(content)
		if err != nil {
			return
		}
		version, err := xdb.VersionFromHeader(header)
		if err != nil {
			return
		}
		s, err := xdb.NewWithBuffer(version, content)
		if err != nil {
			return
		}
		searcher = s
	})
}

// RegionOf 解析公网 IP 的国家、省份与城市（如「中国」「广东省」「东莞市」，
// 与 ip2region 库名一致——漫游市级比对按公司基准的口径来源）。
// 非中国 IP 国家段非空（如「United States」）而省城段为空；
// 城市段未知（库中记 '0'）归一为空串，调用方按省级基准降级；
// 解析失败 / 库未初始化 / 保留段返回三个空串——调用方据此跳过地理维度
func RegionOf(ip string) (country, province, city string) {
	load() // 独立入口必须自初始化：曾有调用方从未触 Available() 导致 searcher 恒 nil、地理维全静默失效（VM 烟测捞出）
	ip = strings.TrimSpace(ip)
	if ip == "" || searcher == nil {
		return "", "", ""
	}
	mu.Lock()
	defer mu.Unlock()
	region, err := searcher.Search(ip)
	if err != nil {
		return "", "", ""
	}
	// 新版 v4 xdb region 段序：国家|省份|城市|ISP|国家代码。
	// 保留段（Reserved 等）国家与省份同段名，视为解析不出——
	// 国家与省份同名的组合在真实归属里不存在（中国/广东省、
	// United States/California），ProvinceOf 的中国过滤曾掩盖这一缺口
	parts := strings.Split(region, "|")
	if len(parts) < 2 || parts[0] == "" || parts[0] == "0" || parts[0] == parts[1] {
		return "", "", ""
	}
	country = parts[0]
	if len(parts) >= 2 && parts[1] != "0" {
		province = parts[1]
	}
	if len(parts) >= 3 && parts[2] != "0" {
		city = parts[2]
	}
	return country, province, city
}

// ProvinceOf 解析公网 IP 的中国省份名（如「广东省」，与 ip2region 库名一致）。
// 非中国 IP / 解析失败 / 库未初始化返回空串——调用方据此跳过地理维度；
// 漫游判定需要区分「海外出口」时用 RegionOf 取国家段
func ProvinceOf(ip string) string {
	country, province, _ := RegionOf(ip)
	if country != "中国" {
		return ""
	}
	return province
}

// Available 库是否可用（初始化成功），供诊断与测试
func Available() bool {
	load()
	return searcher != nil
}
