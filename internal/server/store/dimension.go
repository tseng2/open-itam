package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"itagent/internal/server/model"

	"gorm.io/gorm"
)

// validateDimension 维表共通必填校验：公司边界 + 名称非空。
// 名称 trim 后落库（治理口径：不允许首尾空白差异绕过唯一性）。
// 实体特有校验（如位置父节点、型号类别）在 API 层完成
func validateDimension(companyID int64, name string) (string, error) {
	if companyID <= 0 {
		return "", fmt.Errorf("company_id required")
	}
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", fmt.Errorf("name required")
	}
	return trimmed, nil
}

// dimensionNameTaken 维表名称占用检查（软删除记录不占名，可重建）。
// id > 0 时排除自身——更新保留原名不算冲突
func dimensionNameTaken[T any](ctx context.Context, db *gorm.DB, companyID, id int64, name string) (bool, error) {
	var cnt int64
	q := db.WithContext(ctx).Model(new(T)).
		Where("company_id = ? AND name = ?", companyID, name)
	if id > 0 {
		q = q.Where("id <> ?", id)
	}
	if err := q.Count(&cnt).Error; err != nil {
		return false, err
	}
	return cnt > 0, nil
}

// ==================== 厂商 ====================

func (s *GormStore) CreateManufacturer(ctx context.Context, m model.Manufacturer) (model.Manufacturer, error) {
	name, err := validateDimension(m.CompanyID, m.Name)
	if err != nil {
		return model.Manufacturer{}, err
	}
	m.Name = name
	taken, err := dimensionNameTaken[model.Manufacturer](ctx, s.db, m.CompanyID, 0, m.Name)
	if err != nil {
		return model.Manufacturer{}, fmt.Errorf("check manufacturer name: %w", err)
	}
	if taken {
		return model.Manufacturer{}, ErrAlreadyExists
	}
	if err := s.db.WithContext(ctx).Create(&m).Error; err != nil {
		return model.Manufacturer{}, fmt.Errorf("create manufacturer: %w", err)
	}
	return m, nil
}

func (s *GormStore) ListManufacturers(ctx context.Context, f DimensionListFilter) ([]model.Manufacturer, int64, error) {
	page, size := normalizeDispatchPage(f.Page, f.PageSize)
	q := s.db.WithContext(ctx).Model(&model.Manufacturer{}).Where("company_id = ?", f.CompanyID)
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		q = q.Where("name LIKE ?", "%"+kw+"%")
	}
	return listDimensions[model.Manufacturer](ctx, q, page, size)
}

func (s *GormStore) UpdateManufacturer(ctx context.Context, m model.Manufacturer) (model.Manufacturer, error) {
	name, err := validateDimension(m.CompanyID, m.Name)
	if err != nil {
		return model.Manufacturer{}, err
	}
	if m.ID == 0 {
		return model.Manufacturer{}, fmt.Errorf("id required")
	}
	m.Name = name
	taken, err := dimensionNameTaken[model.Manufacturer](ctx, s.db, m.CompanyID, m.ID, m.Name)
	if err != nil {
		return model.Manufacturer{}, fmt.Errorf("check manufacturer name: %w", err)
	}
	if taken {
		return model.Manufacturer{}, ErrAlreadyExists
	}
	m.UpdatedAt = time.Now().UTC()
	res := s.db.WithContext(ctx).Model(&model.Manufacturer{}).
		Where("id = ? AND company_id = ?", m.ID, m.CompanyID).
		Select("name", "remark", "updated_at").Updates(m)
	if res.Error != nil {
		return model.Manufacturer{}, fmt.Errorf("update manufacturer: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return model.Manufacturer{}, ErrNotFound
	}
	var out model.Manufacturer
	if err := s.db.WithContext(ctx).Where("id = ? AND company_id = ?", m.ID, m.CompanyID).First(&out).Error; err != nil {
		return model.Manufacturer{}, fmt.Errorf("reload manufacturer: %w", err)
	}
	return out, nil
}

func (s *GormStore) DeleteManufacturer(ctx context.Context, companyID, id int64) error {
	return deleteDimension[model.Manufacturer](ctx, s.db, "manufacturer", companyID, id)
}

// ==================== 供应商 ====================

func (s *GormStore) CreateSupplier(ctx context.Context, sup model.Supplier) (model.Supplier, error) {
	name, err := validateDimension(sup.CompanyID, sup.Name)
	if err != nil {
		return model.Supplier{}, err
	}
	sup.Name = name
	taken, err := dimensionNameTaken[model.Supplier](ctx, s.db, sup.CompanyID, 0, sup.Name)
	if err != nil {
		return model.Supplier{}, fmt.Errorf("check supplier name: %w", err)
	}
	if taken {
		return model.Supplier{}, ErrAlreadyExists
	}
	if err := s.db.WithContext(ctx).Create(&sup).Error; err != nil {
		return model.Supplier{}, fmt.Errorf("create supplier: %w", err)
	}
	return sup, nil
}

func (s *GormStore) ListSuppliers(ctx context.Context, f DimensionListFilter) ([]model.Supplier, int64, error) {
	page, size := normalizeDispatchPage(f.Page, f.PageSize)
	q := s.db.WithContext(ctx).Model(&model.Supplier{}).Where("company_id = ?", f.CompanyID)
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		q = q.Where("name LIKE ?", "%"+kw+"%")
	}
	return listDimensions[model.Supplier](ctx, q, page, size)
}

func (s *GormStore) UpdateSupplier(ctx context.Context, sup model.Supplier) (model.Supplier, error) {
	name, err := validateDimension(sup.CompanyID, sup.Name)
	if err != nil {
		return model.Supplier{}, err
	}
	if sup.ID == 0 {
		return model.Supplier{}, fmt.Errorf("id required")
	}
	sup.Name = name
	taken, err := dimensionNameTaken[model.Supplier](ctx, s.db, sup.CompanyID, sup.ID, sup.Name)
	if err != nil {
		return model.Supplier{}, fmt.Errorf("check supplier name: %w", err)
	}
	if taken {
		return model.Supplier{}, ErrAlreadyExists
	}
	sup.UpdatedAt = time.Now().UTC()
	res := s.db.WithContext(ctx).Model(&model.Supplier{}).
		Where("id = ? AND company_id = ?", sup.ID, sup.CompanyID).
		Select("name", "contact_name", "phone", "remark", "updated_at").Updates(sup)
	if res.Error != nil {
		return model.Supplier{}, fmt.Errorf("update supplier: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return model.Supplier{}, ErrNotFound
	}
	var out model.Supplier
	if err := s.db.WithContext(ctx).Where("id = ? AND company_id = ?", sup.ID, sup.CompanyID).First(&out).Error; err != nil {
		return model.Supplier{}, fmt.Errorf("reload supplier: %w", err)
	}
	return out, nil
}

func (s *GormStore) DeleteSupplier(ctx context.Context, companyID, id int64) error {
	return deleteDimension[model.Supplier](ctx, s.db, "supplier", companyID, id)
}

// ==================== 位置库 ====================

func (s *GormStore) CreateLocation(ctx context.Context, l model.Location) (model.Location, error) {
	name, err := validateDimension(l.CompanyID, l.Name)
	if err != nil {
		return model.Location{}, err
	}
	l.Name = name
	taken, err := dimensionNameTaken[model.Location](ctx, s.db, l.CompanyID, 0, l.Name)
	if err != nil {
		return model.Location{}, fmt.Errorf("check location name: %w", err)
	}
	if taken {
		return model.Location{}, ErrAlreadyExists
	}
	if err := s.db.WithContext(ctx).Create(&l).Error; err != nil {
		return model.Location{}, fmt.Errorf("create location: %w", err)
	}
	return l, nil
}

func (s *GormStore) ListLocations(ctx context.Context, f DimensionListFilter) ([]model.Location, int64, error) {
	page, size := normalizeDispatchPage(f.Page, f.PageSize)
	q := s.db.WithContext(ctx).Model(&model.Location{}).Where("company_id = ?", f.CompanyID)
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		q = q.Where("name LIKE ?", "%"+kw+"%")
	}
	return listDimensions[model.Location](ctx, q, page, size)
}

func (s *GormStore) UpdateLocation(ctx context.Context, l model.Location) (model.Location, error) {
	name, err := validateDimension(l.CompanyID, l.Name)
	if err != nil {
		return model.Location{}, err
	}
	if l.ID == 0 {
		return model.Location{}, fmt.Errorf("id required")
	}
	l.Name = name
	taken, err := dimensionNameTaken[model.Location](ctx, s.db, l.CompanyID, l.ID, l.Name)
	if err != nil {
		return model.Location{}, fmt.Errorf("check location name: %w", err)
	}
	if taken {
		return model.Location{}, ErrAlreadyExists
	}
	l.UpdatedAt = time.Now().UTC()
	res := s.db.WithContext(ctx).Model(&model.Location{}).
		Where("id = ? AND company_id = ?", l.ID, l.CompanyID).
		Select("name", "parent_id", "remark", "updated_at").Updates(l)
	if res.Error != nil {
		return model.Location{}, fmt.Errorf("update location: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return model.Location{}, ErrNotFound
	}
	var out model.Location
	if err := s.db.WithContext(ctx).Where("id = ? AND company_id = ?", l.ID, l.CompanyID).First(&out).Error; err != nil {
		return model.Location{}, fmt.Errorf("reload location: %w", err)
	}
	return out, nil
}

func (s *GormStore) DeleteLocation(ctx context.Context, companyID, id int64) error {
	return deleteDimension[model.Location](ctx, s.db, "location", companyID, id)
}

// ==================== 型号库 ====================

func (s *GormStore) CreateAssetModel(ctx context.Context, m model.AssetModel) (model.AssetModel, error) {
	name, err := validateDimension(m.CompanyID, m.Name)
	if err != nil {
		return model.AssetModel{}, err
	}
	m.Name = name
	taken, err := dimensionNameTaken[model.AssetModel](ctx, s.db, m.CompanyID, 0, m.Name)
	if err != nil {
		return model.AssetModel{}, fmt.Errorf("check asset model name: %w", err)
	}
	if taken {
		return model.AssetModel{}, ErrAlreadyExists
	}
	if err := s.db.WithContext(ctx).Create(&m).Error; err != nil {
		return model.AssetModel{}, fmt.Errorf("create asset model: %w", err)
	}
	return m, nil
}

func (s *GormStore) ListAssetModels(ctx context.Context, f AssetModelListFilter) ([]model.AssetModel, int64, error) {
	page, size := normalizeDispatchPage(f.Page, f.PageSize)
	q := s.db.WithContext(ctx).Model(&model.AssetModel{}).Where("company_id = ?", f.CompanyID)
	if f.CategoryID > 0 {
		q = q.Where("category_id = ?", f.CategoryID)
	}
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		q = q.Where("name LIKE ?", "%"+kw+"%")
	}
	return listDimensions[model.AssetModel](ctx, q, page, size)
}

func (s *GormStore) UpdateAssetModel(ctx context.Context, m model.AssetModel) (model.AssetModel, error) {
	name, err := validateDimension(m.CompanyID, m.Name)
	if err != nil {
		return model.AssetModel{}, err
	}
	if m.ID == 0 {
		return model.AssetModel{}, fmt.Errorf("id required")
	}
	m.Name = name
	taken, err := dimensionNameTaken[model.AssetModel](ctx, s.db, m.CompanyID, m.ID, m.Name)
	if err != nil {
		return model.AssetModel{}, fmt.Errorf("check asset model name: %w", err)
	}
	if taken {
		return model.AssetModel{}, ErrAlreadyExists
	}
	m.UpdatedAt = time.Now().UTC()
	res := s.db.WithContext(ctx).Model(&model.AssetModel{}).
		Where("id = ? AND company_id = ?", m.ID, m.CompanyID).
		Select("name", "category_id", "manufacturer_id", "depreciation_id", "eol_months", "remark", "updated_at").Updates(m)
	if res.Error != nil {
		return model.AssetModel{}, fmt.Errorf("update asset model: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return model.AssetModel{}, ErrNotFound
	}
	var out model.AssetModel
	if err := s.db.WithContext(ctx).Where("id = ? AND company_id = ?", m.ID, m.CompanyID).First(&out).Error; err != nil {
		return model.AssetModel{}, fmt.Errorf("reload asset model: %w", err)
	}
	return out, nil
}

func (s *GormStore) DeleteAssetModel(ctx context.Context, companyID, id int64) error {
	return deleteDimension[model.AssetModel](ctx, s.db, "asset model", companyID, id)
}

// ==================== 维表共通工具 ====================

// listDimensions 维表分页查询共通装配：计数 + 倒序分页（与其余列表口径一致）
func listDimensions[T any](ctx context.Context, q *gorm.DB, page, size int) ([]T, int64, error) {
	var total int64
	if err := q.WithContext(ctx).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	items := make([]T, 0, size)
	if err := q.WithContext(ctx).Order("id desc").
		Offset((page - 1) * size).Limit(size).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// deleteDimension 维表软删除；幂等语义：不存在/跨公司一律 NotFound
func deleteDimension[T any](ctx context.Context, db *gorm.DB, what string, companyID, id int64) error {
	res := db.WithContext(ctx).Delete(new(T), "id = ? AND company_id = ?", id, companyID)
	if res.Error != nil {
		return fmt.Errorf("delete %s: %w", what, res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
