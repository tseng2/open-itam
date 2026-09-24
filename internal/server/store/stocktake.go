package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"itagent/internal/server/model"

	"gorm.io/gorm"
)

const (
	maxStocktakePageSize  = 100
	maxStocktakeItemBatch = 500 // CreateInBatches 每批行数，防止超大 INSERT
	maxStocktakeChecks    = 200 // 单次批量核对上限（移动端整批提交与管理端修正共用）
	// scanTokenBytes 扫码令牌随机熵：16 字节 hex 后 32 字符（128-bit，不可枚举）
	scanTokenBytes = 16
)

// validateStocktake 校验任务与明细快照的完整性：
// 明细是范围快照，AssetTag 丢失会让扫码核对永远无法命中
func validateStocktake(st model.Stocktake, items []model.StocktakeItem) error {
	if st.CompanyID <= 0 {
		return fmt.Errorf("company_id required")
	}
	if st.Name == "" {
		return fmt.Errorf("name required")
	}
	if len(items) == 0 {
		return fmt.Errorf("stocktake scope is empty")
	}
	seen := make(map[int64]bool, len(items))
	for i, it := range items {
		if it.CompanyID != st.CompanyID {
			return fmt.Errorf("item %d: company mismatch", i)
		}
		if it.AssetID <= 0 {
			return fmt.Errorf("item %d: asset_id required", i)
		}
		if it.AssetTag == "" {
			return fmt.Errorf("item %d: asset_tag required", i)
		}
		if it.Result != 0 && it.Result != model.StocktakeItemPending {
			return fmt.Errorf("item %d: must start pending", i)
		}
		if seen[it.AssetID] {
			return fmt.Errorf("item %d: duplicate asset %d in scope", i, it.AssetID)
		}
		seen[it.AssetID] = true
	}
	return nil
}

// normalizeStocktakePage 分页兜底，防止零/负值与超大页拖垮查询
func normalizeStocktakePage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > maxStocktakePageSize {
		pageSize = maxStocktakePageSize
	}
	return page, pageSize
}

// newScanToken 生成扫码令牌明文（16 字节随机 → 32 hex 字符）
func newScanToken() (string, error) {
	b := make([]byte, scanTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// hashScanToken 令牌只落 SHA-256：库被拖走也无法反推明文伪造扫码请求
func hashScanToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// validateStocktakeChecks 核对指令前置校验：结果码四选一（pending 不是核对结果）、
// 定位方式二选一、批内目标不得重复（重复意味着指令自身有 bug）
func validateStocktakeChecks(checks []model.StocktakeCheck) error {
	if len(checks) == 0 {
		return fmt.Errorf("checks required")
	}
	if len(checks) > maxStocktakeChecks {
		return fmt.Errorf("too many checks (max %d)", maxStocktakeChecks)
	}
	seenID := make(map[int64]bool, len(checks))
	seenTag := make(map[string]bool, len(checks))
	for i, ck := range checks {
		if ck.Result != model.StocktakeItemNormal &&
			ck.Result != model.StocktakeItemLost &&
			ck.Result != model.StocktakeItemDamaged &&
			ck.Result != model.StocktakeItemScrapped {
			return fmt.Errorf("check %d: invalid result %d", i, ck.Result)
		}
		if ck.ItemID <= 0 && ck.AssetTag == "" {
			return fmt.Errorf("check %d: item_id or asset_tag required", i)
		}
		if ck.ItemID > 0 {
			if seenID[ck.ItemID] {
				return fmt.Errorf("check %d: duplicate item %d", i, ck.ItemID)
			}
			seenID[ck.ItemID] = true
		} else {
			if seenTag[ck.AssetTag] {
				return fmt.Errorf("check %d: duplicate asset_tag %s", i, ck.AssetTag)
			}
			seenTag[ck.AssetTag] = true
		}
	}
	return nil
}

// CreateStocktake 创建草稿任务并落明细快照：状态由服务端强制为草稿，
// 明细强制回到 pending 起点，事务保证任务与明细原子可见
func (s *GormStore) CreateStocktake(ctx context.Context, st model.Stocktake, items []model.StocktakeItem) (model.Stocktake, error) {
	if err := validateStocktake(st, items); err != nil {
		return model.Stocktake{}, err
	}
	st.Status = model.StocktakeStatusDraft

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&st).Error; err != nil {
			return fmt.Errorf("create stocktake: %w", err)
		}
		for i := range items {
			items[i].CompanyID = st.CompanyID
			items[i].StocktakeID = st.ID
			items[i].Result = model.StocktakeItemPending
		}
		if err := tx.CreateInBatches(&items, maxStocktakeItemBatch).Error; err != nil {
			return fmt.Errorf("create stocktake items: %w", err)
		}
		return nil
	})
	if err != nil {
		return model.Stocktake{}, err
	}
	return st, nil
}

func (s *GormStore) ListStocktakes(ctx context.Context, f StocktakeListFilter) ([]model.Stocktake, int64, error) {
	page, size := normalizeStocktakePage(f.Page, f.PageSize)

	q := s.db.WithContext(ctx).Model(&model.Stocktake{})
	if f.CompanyID > 0 {
		q = q.Where("company_id = ?", f.CompanyID)
	}
	if f.Status > 0 {
		q = q.Where("status = ?", f.Status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count stocktakes: %w", err)
	}
	items := make([]model.Stocktake, 0, size)
	if err := q.Order("id desc").Offset((page - 1) * size).Limit(size).Find(&items).Error; err != nil {
		return nil, 0, fmt.Errorf("list stocktakes: %w", err)
	}
	return items, total, nil
}

func (s *GormStore) GetStocktake(ctx context.Context, companyID, id int64) (model.Stocktake, error) {
	var st model.Stocktake
	err := s.db.WithContext(ctx).Where("id = ? AND company_id = ?", id, companyID).First(&st).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Stocktake{}, ErrNotFound
	}
	if err != nil {
		return model.Stocktake{}, fmt.Errorf("get stocktake: %w", err)
	}
	return st, nil
}

// transitionStocktake 条件更新驱动状态机：fromStatuses 内任一状态可流转到 toStatus；
// 未命中时二次区分"不存在或跨公司"与"状态不允许"，与 dispatch 语义一致。
// extra 承载流转附带的列（started_at / finished_at / scan_token_hash）
func (s *GormStore) transitionStocktake(ctx context.Context, companyID, id int64, fromStatuses []int, toStatus int, extra map[string]any) (model.Stocktake, error) {
	updates := map[string]any{"status": toStatus, "updated_at": time.Now().UTC()}
	for k, v := range extra {
		updates[k] = v
	}
	res := s.db.WithContext(ctx).Model(&model.Stocktake{}).
		Where("id = ? AND company_id = ? AND status IN ?", id, companyID, fromStatuses).
		Updates(updates)
	if res.Error != nil {
		return model.Stocktake{}, fmt.Errorf("update stocktake status: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		var exists int64
		if err := s.db.WithContext(ctx).Model(&model.Stocktake{}).
			Where("id = ? AND company_id = ?", id, companyID).
			Count(&exists).Error; err != nil {
			return model.Stocktake{}, fmt.Errorf("check stocktake exists: %w", err)
		}
		if exists == 0 {
			return model.Stocktake{}, ErrNotFound
		}
		return model.Stocktake{}, ErrInvalidState
	}

	var st model.Stocktake
	if err := s.db.WithContext(ctx).First(&st, id).Error; err != nil {
		return model.Stocktake{}, fmt.Errorf("reload stocktake: %w", err)
	}
	return st, nil
}

// StartStocktake 草稿 → 盘点中：生成扫码令牌并记录开始时间。
// 明文令牌仅此一次返回给调用方展示，库里只留哈希
func (s *GormStore) StartStocktake(ctx context.Context, companyID, id int64) (model.Stocktake, string, error) {
	token, err := newScanToken()
	if err != nil {
		return model.Stocktake{}, "", fmt.Errorf("generate scan token: %w", err)
	}
	st, err := s.transitionStocktake(ctx, companyID, id,
		[]int{model.StocktakeStatusDraft}, model.StocktakeStatusProcessing,
		map[string]any{"started_at": time.Now().UTC(), "scan_token_hash": hashScanToken(token)})
	return st, token, err
}

// RotateStocktakeToken 令牌轮换（疑似泄露时的止血动作）：仅盘点中可轮换，
// 旧令牌即刻失效；toStatus 与 from 相同，靠 hash 列变化保证命中行
func (s *GormStore) RotateStocktakeToken(ctx context.Context, companyID, id int64) (string, error) {
	token, err := newScanToken()
	if err != nil {
		return "", fmt.Errorf("generate scan token: %w", err)
	}
	_, err = s.transitionStocktake(ctx, companyID, id,
		[]int{model.StocktakeStatusProcessing}, model.StocktakeStatusProcessing,
		map[string]any{"scan_token_hash": hashScanToken(token)})
	if err != nil {
		return "", err
	}
	return token, nil
}

func (s *GormStore) FinishStocktake(ctx context.Context, companyID, id int64, finishedAt time.Time) (model.Stocktake, error) {
	return s.transitionStocktake(ctx, companyID, id,
		[]int{model.StocktakeStatusProcessing}, model.StocktakeStatusFinished,
		map[string]any{"finished_at": finishedAt})
}

func (s *GormStore) CancelStocktake(ctx context.Context, companyID, id int64) (model.Stocktake, error) {
	return s.transitionStocktake(ctx, companyID, id,
		[]int{model.StocktakeStatusDraft, model.StocktakeStatusProcessing}, model.StocktakeStatusCanceled, nil)
}

// GetStocktakeByToken 扫码令牌换任务：仅"盘点中"有效（finish/cancel 即失效），
// 任务不存在与非盘点中同样返回 ErrNotFound，不向扫码方泄露任务状态
func (s *GormStore) GetStocktakeByToken(ctx context.Context, token string) (model.Stocktake, error) {
	if token == "" {
		return model.Stocktake{}, ErrNotFound
	}
	var st model.Stocktake
	err := s.db.WithContext(ctx).
		Where("scan_token_hash = ? AND status = ?", hashScanToken(token), model.StocktakeStatusProcessing).
		First(&st).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Stocktake{}, ErrNotFound
	}
	if err != nil {
		return model.Stocktake{}, fmt.Errorf("get stocktake by token: %w", err)
	}
	return st, nil
}

func (s *GormStore) ListStocktakeItems(ctx context.Context, f StocktakeItemListFilter) ([]model.StocktakeItem, int64, error) {
	page, size := normalizeStocktakePage(f.Page, f.PageSize)

	q := s.db.WithContext(ctx).Model(&model.StocktakeItem{})
	if f.CompanyID > 0 {
		q = q.Where("company_id = ?", f.CompanyID)
	}
	if f.StocktakeID > 0 {
		q = q.Where("stocktake_id = ?", f.StocktakeID)
	}
	if f.Result > 0 {
		q = q.Where("result = ?", f.Result)
	}
	if f.Keyword != "" {
		q = q.Where("asset_tag LIKE ?", "%"+f.Keyword+"%")
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count stocktake items: %w", err)
	}
	items := make([]model.StocktakeItem, 0, size)
	// id 正序即建账快照顺序，与管理端标签打印顺序一致；预载资产供表格展示
	//（SQLiteStore 测试库无 assets 表，不预载——与 dispatch 的既有不对称先例一致）
	if err := q.Preload("Asset").Order("id asc").
		Offset((page - 1) * size).Limit(size).Find(&items).Error; err != nil {
		return nil, 0, fmt.Errorf("list stocktake items: %w", err)
	}
	return items, total, nil
}

// CheckStocktakeItems 批量核对明细：先整体校验（结果码、定位、批内去重、任务状态），
// 再在事务内逐条条件更新，任一明细未命中即整批失败（原子性，防止半批落库）
func (s *GormStore) CheckStocktakeItems(ctx context.Context, companyID, stocktakeID int64, checks []model.StocktakeCheck, scannedBy string) ([]model.StocktakeItem, error) {
	if err := validateStocktakeChecks(checks); err != nil {
		return nil, err
	}
	if scannedBy == "" {
		return nil, fmt.Errorf("scanned_by required")
	}
	var st model.Stocktake
	if err := s.db.WithContext(ctx).Where("id = ? AND company_id = ?", stocktakeID, companyID).First(&st).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("load stocktake: %w", err)
	}
	if st.Status != model.StocktakeStatusProcessing {
		return nil, ErrInvalidState
	}

	now := time.Now().UTC()
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for i, ck := range checks {
			// 改判重核语义：空串不覆盖既有值，避免只改结果时误清实盘位置/备注
			updates := map[string]any{
				"result":      ck.Result,
				"scanned_by":  scannedBy,
				"scanned_at":  now,
				"updated_at":  now,
			}
			if ck.ActualLocation != "" {
				updates["actual_location"] = ck.ActualLocation
			}
			if ck.Remark != "" {
				updates["remark"] = ck.Remark
			}
			cond := tx.Model(&model.StocktakeItem{}).
				Where("stocktake_id = ? AND company_id = ?", stocktakeID, companyID)
			if ck.ItemID > 0 {
				cond = cond.Where("id = ?", ck.ItemID)
			} else {
				cond = cond.Where("asset_tag = ?", ck.AssetTag)
			}
			res := cond.Updates(updates)
			if res.Error != nil {
				return fmt.Errorf("update item %d: %w", i, res.Error)
			}
			if res.RowsAffected == 0 {
				return ErrNotFound
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("check stocktake items: %w", err)
	}
	return s.reloadStocktakeItems(ctx, companyID, stocktakeID, checks)
}

// reloadStocktakeItems 一次查询取回整批更新后的明细，按提交顺序排列
func (s *GormStore) reloadStocktakeItems(ctx context.Context, companyID, stocktakeID int64, checks []model.StocktakeCheck) ([]model.StocktakeItem, error) {
	idSet := make([]int64, 0, len(checks))
	tagSet := make([]string, 0, len(checks))
	for _, ck := range checks {
		if ck.ItemID > 0 {
			idSet = append(idSet, ck.ItemID)
		} else {
			tagSet = append(tagSet, ck.AssetTag)
		}
	}
	q := s.db.WithContext(ctx).Where("stocktake_id = ? AND company_id = ?", stocktakeID, companyID)
	switch {
	case len(idSet) > 0 && len(tagSet) > 0:
		q = q.Where("id IN ? OR asset_tag IN ?", idSet, tagSet)
	case len(idSet) > 0:
		q = q.Where("id IN ?", idSet)
	default:
		q = q.Where("asset_tag IN ?", tagSet)
	}
	var found []model.StocktakeItem
	if err := q.Find(&found).Error; err != nil {
		return nil, fmt.Errorf("reload stocktake items: %w", err)
	}
	byID := make(map[int64]model.StocktakeItem, len(found))
	byTag := make(map[string]model.StocktakeItem, len(found))
	for _, it := range found {
		byID[it.ID] = it
		byTag[it.AssetTag] = it
	}
	out := make([]model.StocktakeItem, 0, len(checks))
	for _, ck := range checks {
		if ck.ItemID > 0 {
			out = append(out, byID[ck.ItemID])
		} else {
			out = append(out, byTag[ck.AssetTag])
		}
	}
	return out, nil
}

func (s *GormStore) CountStocktakeResults(ctx context.Context, companyID, stocktakeID int64) (map[int]int64, error) {
	type resultCount struct {
		Result int
		N      int64
	}
	var rows []resultCount
	if err := s.db.WithContext(ctx).Model(&model.StocktakeItem{}).
		Select("result, COUNT(*) AS n").
		Where("company_id = ? AND stocktake_id = ?", companyID, stocktakeID).
		Group("result").Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("count stocktake results: %w", err)
	}
	// 五个结果段位全部占位，缺省 0——调用方无需处理缺键
	counts := map[int]int64{
		model.StocktakeItemPending:  0,
		model.StocktakeItemNormal:   0,
		model.StocktakeItemLost:     0,
		model.StocktakeItemDamaged:  0,
		model.StocktakeItemScrapped: 0,
	}
	for _, r := range rows {
		counts[r.Result] = r.N
	}
	return counts, nil
}
