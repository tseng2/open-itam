// Package geoip 提供出口 IP → 省份的离线解析（漫游判定的地理维度）。
// 数据源 ip2region v4 离线库（单文件 xdb，纯 Go 读取零 CGO，go:embed
// 内嵌与 smartctl 先例一致——单二进制自包含，部署零外部依赖）；
// 库更新走 repo 换文件重编译，不做运行时加载。
//
// 设计边界（内外网判定契约）：GeoIP 只做「省份归属」一个维度的输出，
// 不判断在线/失联（那是心跳阈值的事）；解析失败/非中国 IP 一律返回
// 空串，由调用方降级为「跳过地理维度」（宁漏报不误报）。
// GeoIP 颗粒度只到省级：同城家宽与公司无法区分（内网判定交给
// 本机 IP 公私网维度），4G 基站跨省漂移靠省级宽口径容错。
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

// RegionOf 解析公网 IP 的国家与省份（如「中国」「广东省」，与 ip2region 库名一致）。
// 非中国 IP 国家段非空（如「United States」）；解析失败 / 库未初始化 /
// 保留段返回两个空串——调用方据此跳过地理维度
func RegionOf(ip string) (country, province string) {
	ip = strings.TrimSpace(ip)
	if ip == "" || searcher == nil {
		return "", ""
	}
	mu.Lock()
	defer mu.Unlock()
	region, err := searcher.Search(ip)
	if err != nil {
		return "", ""
	}
	// 新版 v4 xdb region 段序：国家|省份|城市|ISP|国家代码。
	// 保留段（Reserved 等）国家与省份同段名，视为解析不出
	parts := strings.Split(region, "|")
	if len(parts) < 2 || parts[0] == "" || parts[0] == "0" {
		return "", ""
	}
	country = parts[0]
	if len(parts) >= 2 {
		province = parts[1]
	}
	if province == "0" {
		province = ""
	}
	return country, province
}

// ProvinceOf 解析公网 IP 的中国省份名（如「广东省」，与 ip2region 库名一致）。
// 非中国 IP / 解析失败 / 库未初始化返回空串——调用方据此跳过地理维度；
// 漫游判定需要区分「海外出口」时用 RegionOf 取国家段
func ProvinceOf(ip string) string {
	country, province := RegionOf(ip)
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
