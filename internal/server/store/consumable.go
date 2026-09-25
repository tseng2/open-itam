package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"itagent/internal/server/model"

	"gorm.io/gorm"
)

// 耗材管理（P2 体验运营）：GormStore 生产实现。
// SQLiteStore 同步实现见 sqlite.go（契约测试 consumable_test.go）。
// 库存数量只经流水变更：CreateConsumableTxn 用条件更新
//（stock + delta >= 0）原子扣减，出库击穿零库存返回 ErrInsufficient

// validateConsumable 耗材元数据必填校验：公司边界 + 名称非空（trim 落库，
// 与维表治理同口径）。库存不在此校验——建账恒 0，只经流水变更
func validateConsumable(c model.Consumable) (model.Consumable, error) {
	if c.CompanyID <= 0 {
		return c, fmt.Errorf("company_id required")
	}
	name := strings.TrimSpace(c.Name)
	if name == "" {
		return c, fmt.Errorf("name required")
	}
	c.Name = name
	if c.MinQuantity < 0 {
		return c, fmt.Errorf("min_quantity must not be negative")
	}
	return c, nil
}

// consumableNameTaken 名称占用检查（软删除记录不占名，可重建）；
// id > 0 时排除自身——更新保留原名不算冲突（维表同口径）
func consumableNameTaken(ctx context.Context, db *gorm.DB, companyID, id int64, name string) (bool, error) {
	var cnt int64
	q := db.WithContext(ctx).Model(&model.Consumable{}).
		Where("company_id = ? AND name = ?", companyID, name)
	if id > 0 {
		q = q.Where("id <> ?", id)
	}
	if err := q.Count(&cnt).Error; err != nil {
		return false, err
	}
	return cnt > 0, nil
}

func (s *GormStore) CreateConsumable(ctx context.Context, c model.Consumable) (model.Consumable, error) {
	c, err := validateConsumable(c)
	if err != nil {
		return model.Consumable{}, err
	}
	taken, err := consumableNameTaken(ctx, s.db, c.CompanyID, 0, c.Name)
	if err != nil {
		return model.Consumable{}, fmt.Errorf("check consumable name: %w", err)
	}
	if taken {
		return model.Consumable{}, ErrAlreadyExists
	}
	c.Stock = 0 // 建账库存恒 0：期初库存走第一笔入库流水，账实可追溯
	if err := s.db.WithContext(ctx).Create(&c).Error; err != nil {
		return model.Consumable{}, fmt.Errorf("create consumable: %w", err)
	}
	return c, nil
}

func (s *GormStore) ListConsumables(ctx context.Context, f ConsumableListFilter) ([]model.Consumable, int64, error) {
	page, size := normalizeDispatchPage(f.Page, f.PageSize)
	q := s.db.WithContext(ctx).Model(&model.Consumable{}).Where("company_id = ?", f.CompanyID)
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		q = q.Where("name LIKE ? OR spec LIKE ?", "%"+kw+"%", "%"+kw+"%")
	}
	if f.LowStock != nil {
		if *f.LowStock {
			q = q.Where("min_quantity > 0 AND stock <= min_quantity")
		} else {
			q = q.Where("NOT (min_quantity > 0 AND stock <= min_quantity)")
		}
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count consumables: %w", err)
	}
	items := make([]model.Consumable, 0, size)
	if err := q.Order("id desc").
		Offset((page - 1) * size).Limit(size).Find(&items).Error; err != nil {
		return nil, 0, fmt.Errorf("list consumables: %w", err)
	}
	return items, total, nil
}

// UpdateConsumable 编辑只动元数据（名称/规格/单位/预警线/备注），
// 库存列不在 Select 列表——库存只经流水变更
func (s *GormStore) UpdateConsumable(ctx context.Context, c model.Consumable) (model.Consumable, error) {
	c, err := validateConsumable(c)
	if err != nil {
		return model.Consumable{}, err
	}
	if c.ID == 0 {
		return model.Consumable{}, fmt.Errorf("id required")
	}
	taken, err := consumableNameTaken(ctx, s.db, c.CompanyID, c.ID, c.Name)
	if err != nil {
		return model.Consumable{}, fmt.Errorf("check consumable name: %w", err)
	}
	if taken {
		return model.Consumable{}, ErrAlreadyExists
	}
	c.UpdatedAt = time.Now().UTC()
	res := s.db.WithContext(ctx).Model(&model.Consumable{}).
		Where("id = ? AND company_id = ?", c.ID, c.CompanyID).
		Select("name", "spec", "unit", "min_quantity", "remark", "updated_at").Updates(c)
	if res.Error != nil {
		return model.Consumable{}, fmt.Errorf("update consumable: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return model.Consumable{}, ErrNotFound
	}
	var out model.Consumable
	if err := s.db.WithContext(ctx).Where("id = ? AND company_id = ?", c.ID, c.CompanyID).First(&out).Error; err != nil {
		return model.Consumable{}, fmt.Errorf("reload consumable: %w", err)
	}
	return out, nil
}

func (s *GormStore) DeleteConsumable(ctx context.Context, companyID, id int64) error {
	res := s.db.WithContext(ctx).Delete(&model.Consumable{}, "id = ? AND company_id = ?", id, companyID)
	if res.Error != nil {
		return fmt.Errorf("delete consumable: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// validateConsumableTxn 流水校验：类型合法、符号与类型匹配（入库恒正/
// 出库恒负/调整任意非零）、操作人必填（操作人快照是审计依据）
func validateConsumableTxn(txn model.ConsumableTxn) error {
	if !model.ConsumableTxnTypes[txn.Type] {
		return fmt.Errorf("invalid txn type %q", txn.Type)
	}
	switch txn.Type {
	case model.ConsumableTxnStockIn:
		if txn.Delta <= 0 {
			return fmt.Errorf("stock-in delta must be positive")
		}
	case model.ConsumableTxnStockOut:
		if txn.Delta >= 0 {
			return fmt.Errorf("stock-out delta must be negative")
		}
	case model.ConsumableTxnAdjust:
		if txn.Delta == 0 {
			return fmt.Errorf("adjust delta must not be zero")
		}
	}
	if txn.OperatorID <= 0 {
		return fmt.Errorf("operator_id required")
	}
	if strings.TrimSpace(txn.OperatorName) == "" {
		return fmt.Errorf("operator_name required")
	}
	return nil
}

// CreateConsumableTxn 记一笔流水并原子变更库存：单事务内
//「条件更新 stock + delta >= 0」+「插流水」。条件更新零命中时区分
// 耗材不存在（含跨公司 → NotFound）与库存不足（ErrInsufficient），
// 并发超卖由条件更新天然拦截，流水不会落半截
func (s *GormStore) CreateConsumableTxn(ctx context.Context, txn model.ConsumableTxn) (model.ConsumableTxn, model.Consumable, error) {
	if err := validateConsumableTxn(txn); err != nil {
		return model.ConsumableTxn{}, model.Consumable{}, err
	}
	now := time.Now().UTC()
	txn.CreatedAt = now

	var out model.Consumable
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&model.Consumable{}).
			Where("id = ? AND company_id = ? AND stock + ? >= 0", txn.ConsumableID, txn.CompanyID, txn.Delta).
			Updates(map[string]any{"stock": gorm.Expr("stock + ?", txn.Delta), "updated_at": now})
		if res.Error != nil {
			return fmt.Errorf("apply stock: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			var exists int64
			if err := tx.Model(&model.Consumable{}).
				Where("id = ? AND company_id = ?", txn.ConsumableID, txn.CompanyID).
				Count(&exists).Error; err != nil {
				return fmt.Errorf("check consumable: %w", err)
			}
			if exists == 0 {
				return ErrNotFound
			}
			return ErrInsufficient
		}
		if err := tx.Create(&txn).Error; err != nil {
			return fmt.Errorf("insert txn: %w", err)
		}
		if err := tx.Where("id = ? AND company_id = ?", txn.ConsumableID, txn.CompanyID).First(&out).Error; err != nil {
			return fmt.Errorf("reload consumable: %w", err)
		}
		return nil
	})
	if err != nil {
		return model.ConsumableTxn{}, model.Consumable{}, err
	}
	return txn, out, nil
}

func (s *GormStore) ListConsumableTxns(ctx context.Context, f ConsumableTxnListFilter) ([]model.ConsumableTxn, int64, error) {
	page, size := normalizeDispatchPage(f.Page, f.PageSize)
	q := s.db.WithContext(ctx).Model(&model.ConsumableTxn{}).
		Where("company_id = ?", f.CompanyID)
	if f.ConsumableID > 0 {
		q = q.Where("consumable_id = ?", f.ConsumableID)
	}
	if f.Type != "" {
		q = q.Where("type = ?", f.Type)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count consumable txns: %w", err)
	}
	items := make([]model.ConsumableTxn, 0, size)
	if err := q.Order("id desc").
		Offset((page - 1) * size).Limit(size).Find(&items).Error; err != nil {
		return nil, 0, fmt.Errorf("list consumable txns: %w", err)
	}
	return items, total, nil
}
