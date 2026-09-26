package model

// Company represents a subsidiary or tenant in the group company model.
// Region 是漫游判定的地理维基准（2026-09-26 市级升级）：格式「省|市」
// （如「广东省|东莞市」，与 ip2region v4 库名口径一致；只填省段则按
// 省级宽口径比对）；空 = 该公司资产跳过地理维判定（宁漏报不误报）
type Company struct {
	BaseModel
	Name   string `gorm:"type:varchar(128);not null;unique" json:"name"`
	Domain string `gorm:"type:varchar(128)" json:"domain"` // e.g. jg.com
	Code   string `gorm:"type:varchar(64)" json:"code"`
	Region string `gorm:"type:varchar(64)" json:"region"` // 所属区域「省|市」，漫游地理维基准
}
