package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"itagent/internal/server/model"

	"gorm.io/gorm"
)

// 软件许可（P2 体验运营）：授权池的 GormStore 生产实现。
// SQLiteStore 同步实现见 sqlite.go（契约测试 license_test.go）。
// 状态与已用席位不落库：状态在 API 层经 ResolveLicenseStatus 派生，
// 席位由资产挂接计数富化——store 只管许可表本身的持久化

// validateLicense 许可最小必填校验：公司边界 + 名称非空（trim 落库，
// 与维表治理同口径）+ 席位非负（0 = 不限席位）
func validateLicense(l model.License) (model.License, error) {
	if l.CompanyID <= 0 {
		return l, fmt.Errorf("company_id required")
	}
	name := strings.TrimSpace(l.Name)
	if name == "" {
		return l, fmt.Errorf("name required")
	}
	l.Name = name
	if l.TotalSeats < 0 {
		return l, fmt.Errorf("total_seats must not be negative")
	}
	return l, nil
}

func (s *GormStore) CreateLicense(ctx context.Context, l model.License) (model.License, error) {
	l, err := validateLicense(l)
	if err != nil {
		return model.License{}, err
	}
	if err := s.db.WithContext(ctx).Create(&l).Error; err != nil {
		return model.License{}, fmt.Errorf("create license: %w", err)
	}
	return l, nil
}

func (s *GormStore) ListLicenses(ctx context.Context, f LicenseListFilter) ([]model.License, int64, error) {
	page, size := normalizeDispatchPage(f.Page, f.PageSize)
	q := s.db.WithContext(ctx).Model(&model.License{}).Where("company_id = ?", f.CompanyID)
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		q = q.Where("name LIKE ? OR vendor LIKE ?", "%"+kw+"%", "%"+kw+"%")
	}
	// 到期提醒窗口：到期日在 (now, now+N] 且未终止；永久授权（无到期日）不进窗口。
	// 时间参数统一 UTC 归一（glebarez SQLite 文本比较坑，见 P2-1 经验）
	if f.ExpiringDays > 0 {
		now := time.Now().UTC()
		q = q.Where("termination_date IS NULL AND expiration_date IS NOT NULL AND expiration_date > ? AND expiration_date <= ?",
			now, now.AddDate(0, 0, f.ExpiringDays))
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count licenses: %w", err)
	}
	items := make([]model.License, 0, size)
	if err := q.Order("id desc").
		Offset((page - 1) * size).Limit(size).Find(&items).Error; err != nil {
		return nil, 0, fmt.Errorf("list licenses: %w", err)
	}
	return items, total, nil
}

func (s *GormStore) GetLicense(ctx context.Context, companyID, id int64) (model.License, error) {
	var l model.License
	err := s.db.WithContext(ctx).Where("id = ? AND company_id = ?", id, companyID).First(&l).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.License{}, ErrNotFound
	}
	if err != nil {
		return model.License{}, fmt.Errorf("get license: %w", err)
	}
	return l, nil
}

func (s *GormStore) UpdateLicense(ctx context.Context, l model.License) (model.License, error) {
	l, err := validateLicense(l)
	if err != nil {
		return model.License{}, err
	}
	if l.ID == 0 {
		return model.License{}, fmt.Errorf("id required")
	}
	l.UpdatedAt = time.Now().UTC()
	res := s.db.WithContext(ctx).Model(&model.License{}).
		Where("id = ? AND company_id = ?", l.ID, l.CompanyID).
		Select("name", "vendor", "category", "license_key", "total_seats",
			"purchase_date", "expiration_date", "termination_date", "remark", "updated_at").
		Updates(l)
	if res.Error != nil {
		return model.License{}, fmt.Errorf("update license: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return model.License{}, ErrNotFound
	}
	var out model.License
	if err := s.db.WithContext(ctx).Where("id = ? AND company_id = ?", l.ID, l.CompanyID).First(&out).Error; err != nil {
		return model.License{}, fmt.Errorf("reload license: %w", err)
	}
	return out, nil
}

func (s *GormStore) DeleteLicense(ctx context.Context, companyID, id int64) error {
	res := s.db.WithContext(ctx).Delete(&model.License{}, "id = ? AND company_id = ?", id, companyID)
	if res.Error != nil {
		return fmt.Errorf("delete license: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
