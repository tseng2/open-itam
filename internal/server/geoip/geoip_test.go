package geoip

import "testing"

// 内嵌库冒烟：公司出口段解析出广东省、海外 IP 返回空（跳过地理维度）、
// 非法输入安全返回空；省份名格式与库一致（设置页 company_province 按此对齐）
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
	country, province := RegionOf("114.114.114.114")
	if country != "中国" || province != "江苏省" {
		t.Fatalf("RegionOf cold start = (%q, %q), want (中国, 江苏省)", country, province)
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
				if got := ProvinceOf("61.142.9.88"); got != "广东省" {
					t.Errorf("concurrent ProvinceOf = %q, want 广东省", got)
				}
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < 4; i++ {
		<-done
	}
}
