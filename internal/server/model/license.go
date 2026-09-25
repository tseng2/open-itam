package model

import "time"

// P2 软件许可管理：商业正版授权池（席位总数 / 授权密钥 / 到期日 / 终止日）。
// 席位使用数与许可状态均不落库——使用数由资产挂接（assets.license_id）
// 实时计数，状态由日期派生（与外派"超期是计算属性"同口径），口径只此一处

// 许可状态段位：由 ResolveLicenseStatus 从日期实时派生，不落库。
// 优先级：终止日非空（合同终止盖过一切）→ 到期日已过 → 在用
const (
	LicenseStatusActive     = 10 // 在用（未到期且未终止）
	LicenseStatusExpired    = 20 // 已到期（到期日已过且未终止）
	LicenseStatusTerminated = 30 // 已终止（合同终止日非空）
)

type License struct {
	BaseModel
	CompanyID       int64      `gorm:"index;not null" json:"company_id"`
	Name            string     `gorm:"type:varchar(128);not null" json:"name"` // 软件名称（如 Microsoft 365 商业高级版）
	Vendor          string     `gorm:"type:varchar(128)" json:"vendor"`       // 软件厂商（Microsoft / Autodesk）
	Category        string     `gorm:"type:varchar(64)" json:"category"`      // 分类（办公套件 / 工程设计 / 开发工具）
	LicenseKey      string     `gorm:"type:varchar(255)" json:"license_key"`  // 授权密钥
	TotalSeats      int        `gorm:"not null;default:0" json:"total_seats"` // 席位总数（0 = 不限席位）
	PurchaseDate    *time.Time `json:"purchase_date"`                        // 采购日期
	ExpirationDate  *time.Time `gorm:"index" json:"expiration_date"`         // 到期日（NULL = 永久授权）
	TerminationDate *time.Time `json:"termination_date"`                     // 合同终止日（非空即已终止）
	Remark          string     `gorm:"type:text" json:"remark"`

	// 服务端派生字段（不落库）：状态由日期驱动；已用席位由资产挂接计数。
	// 与 Asset.Presence 同模式，展示层只渲染不计算
	Status    int    `gorm:"-" json:"status,omitempty"`
	UsedSeats int64  `gorm:"-" json:"used_seats"`
}

func (License) TableName() string { return "licenses" }

// ResolveLicenseStatus 许可状态派生：终止日非空优先（合同终止盖过一切），
// 到期日已过（严格小于当前时刻）次之，其余在用。到期日当天仍算在用
//（闭区间语义，与"归期当天未超期"的口径一致）
func ResolveLicenseStatus(now time.Time, expiration, termination *time.Time) int {
	if termination != nil {
		return LicenseStatusTerminated
	}
	if expiration != nil && expiration.Before(now) {
		return LicenseStatusExpired
	}
	return LicenseStatusActive
}
