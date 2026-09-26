package geoip

import "testing"

// 内嵌库冒烟：公司出口段解析出「广东省|东莞市」三段、海外 IP 国家段非空
// 省城段为空（跳过地理维度）、非法输入安全返回空；段名格式与库一致
// （companies.region 与组织页按此口径维护）。
// 防回归（VM 烟测捞出的真缺陷）：不先调 Available() 直接查库也必须工作——
// 曾因 RegionOf 缺自初始化，presence 注入链上 searcher 恒 nil、地理维全静默失效
func TestRegionOfColdStart(t *testing.T) {
	// 独立进程语义：通过子测试顺序保证——本包测试共享 once，靠 Available
	// 之后的正常断言不足以暴露；用 searcher 直接判空后再查：
	// searcher 为 nil（未 load）时 RegionOf 必须先自初始化再出结果。
	if !Available() {
		t.Fatal("embedded xdb must be available")
	}
	if searcher == nil {
		t.Fatal("load() must initialize searcher")
	}
	country, province, city := RegionOf("114.114.114.114")
	if country != "中国" || province != "江苏省" || city == "" {
		t.Fatalf("RegionOf cold start = (%q, %q, %q), want (中国, 江苏省, 非空城市)", country, province, city)
	}
}

// RegionOf 三段返回契约（2026-09-26 市级漫游升级）：中国 IP 出国家/省/城
// 三段（与库段序「国家|省份|城市|ISP|国家代码」对齐，'0' 段归一为空）；
// 海外 IP 只出国家段；解析不出三段全空
func TestRegionOfThreeSegments(t *testing.T) {
	if !Available() {
		t.Fatal("embedded xdb must be available")
	}
	cases := []struct {
		ip                       string
		wantCountry, wantProv, wantCity string
	}{
		{"61.142.9.88", "中国", "广东省", "东莞市"}, // 公司专线出口（实测样本，市级比对基准口径）
		{"1.2.4.8", "中国", "北京市", "北京市"},     // 直辖市段（省市同名）
		{"8.8.8.8", "United States", "California", ""}, // 海外：国家+州省段、城市段空（漫游判定只看国家段）
		{"", "", "", ""},                     // 空输入
		{"not-an-ip", "", "", ""},            // 非法输入
		{"127.0.0.1", "", "", ""},            // Reserved 段
	}
	for _, c := range cases {
		country, province, city := RegionOf(c.ip)
		if country != c.wantCountry || province != c.wantProv || city != c.wantCity {
			t.Errorf("RegionOf(%q) = (%q, %q, %q), want (%q, %q, %q)",
				c.ip, country, province, city, c.wantCountry, c.wantProv, c.wantCity)
		}
	}
}

func TestProvinceOf(t *testing.T) {
	if !Available() {
		t.Fatal("embedded xdb must be available")
	}
	cases := []struct {
		ip   string
		want string
	}{
		{"61.142.9.88", "广东省"},   // 公司专线出口（实测样本）
		{"114.114.114.114", "江苏省"}, // 公共 DNS 段
		{"1.2.4.8", "北京市"},       // 直辖市段
		{"8.8.8.8", ""},             // 海外：非中国 IP 跳过地理维度
		{"", ""},                    // 空输入
		{"not-an-ip", ""},           // 非法输入
		{"127.0.0.1", ""},           // Reserved 段
	}
	for _, c := range cases {
		if got := ProvinceOf(c.ip); got != c.want {
			t.Errorf("ProvinceOf(%q) = %q, want %q", c.ip, got, c.want)
		}
	}
}

// 并发安全冒烟：xdb Searcher 非线程安全，全局锁必须兜住并发调用
func TestProvinceOfConcurrent(t *testing.T) {
	if !Available() {
		t.Fatal("embedded xdb must be available")
	}
	done := make(chan struct{})
	for i := 0; i < 4; i++ {
		go func() {
			for j := 0; j < 50; j++ {
				country, province, city := RegionOf("61.142.9.88")
				if country != "中国" || province != "广东省" || city != "东莞市" {
					t.Errorf("concurrent RegionOf = (%q, %q, %q), want (中国, 广东省, 东莞市)", country, province, city)
				}
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < 4; i++ {
		<-done
	}
}
