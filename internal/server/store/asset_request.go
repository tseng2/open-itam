package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"itagent/internal/server/model"

	"gorm.io/gorm"
)

// validateAssetRequest 申请必填校验：申请人是台账绑定与履历 TargetPerson 的数据
// 依据；短期借用必须有归期（审批通过后写入履历 ReturnDate），长期领用归期
// 强制归一清空，防止误填
func validateAssetRequest(r model.AssetRequest) (model.AssetRequest, error) {
	if r.CompanyID <= 0 {
		return r, fmt.Errorf("company_id required")
	}
	if r.AssetID <= 0 {
		return r, fmt.Errorf("asset_id required")
	}
	if r.ApplicantID <= 0 {
		return r, fmt.Errorf("applicant_id required")
	}
	if r.ApplicantName == "" {
		return r, fmt.Errorf("applicant_name required")
	}
	if r.Reason == "" {
		return r, fmt.Errorf("reason required")
	}
	if !r.IsLongTerm && r.ExpectedReturnAt == nil {
		return r, fmt.Errorf("expected_return_at required for short-term borrow")
	}
	if r.IsLongTerm {
		r.ExpectedReturnAt = nil
	}
	return r, nil
}

// normalizeAssetRequestPage 分页兜底，防止零/负值与超大页拖垮查询
func normalizeAssetRequestPage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > maxDispatchPageSize {
		pageSize = maxDispatchPageSize
	}
	return page, pageSize
}

// CreateAssetRequest 提交申请：事务内校验"同一申请人对同一资产仅一条待审批"
// 再落库（不同申请人竞争同一资产放行，留给审批人裁决）；
// 状态由服务端强制为待审批
func (s *GormStore) CreateAssetRequest(ctx context.Context, r model.AssetRequest) (model.AssetRequest, error) {
	r, err := validateAssetRequest(r)
	if err != nil {
		return model.AssetRequest{}, err
	}
	r.Status = model.AssetRequestStatusPending

	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&model.AssetRequest{}).
			Where("company_id = ? AND asset_id = ? AND applicant_id = ? AND status = ?",
				r.CompanyID, r.AssetID, r.ApplicantID, model.AssetRequestStatusPending).
			Count(&count).Error; err != nil {
			return fmt.Errorf("count pending requests: %w", err)
		}
		if count > 0 {
			return ErrAlreadyExists
		}
		return tx.Create(&r).Error
	})
	if err != nil {
		return model.AssetRequest{}, fmt.Errorf("create asset request: %w", err)
	}
	return r, nil
}

func (s *GormStore) ListAssetRequests(ctx context.Context, f AssetRequestListFilter) ([]model.AssetRequest, int64, error) {
	page, size := normalizeAssetRequestPage(f.Page, f.PageSize)

	q := s.db.WithContext(ctx).Model(&model.AssetRequest{})
	if f.CompanyID > 0 {
		q = q.Where("company_id = ?", f.CompanyID)
	}
	if f.Status > 0 {
		q = q.Where("status = ?", f.Status)
	}
	if f.ApplicantID > 0 {
		q = q.Where("applicant_id = ?", f.ApplicantID)
	}
	if f.AssetID > 0 {
		q = q.Where("asset_id = ?", f.AssetID)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count asset requests: %w", err)
	}
	items := make([]model.AssetRequest, 0, size)
	if err := q.Preload("Asset").Order("id desc").
		Offset((page - 1) * size).Limit(size).Find(&items).Error; err != nil {
		return nil, 0, fmt.Errorf("list asset requests: %w", err)
	}
	return items, total, nil
}

func (s *GormStore) GetAssetRequest(ctx context.Context, companyID, id int64) (model.AssetRequest, error) {
	var r model.AssetRequest
	err := s.db.WithContext(ctx).Where("id = ? AND company_id = ?", id, companyID).First(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.AssetRequest{}, ErrNotFound
	}
	if err != nil {
		return model.AssetRequest{}, fmt.Errorf("get asset request: %w", err)
	}
	return r, nil
}

// transitionAssetRequest 原子条件更新：仅待审批可流转到驳回/取消；
// 未命中时二次区分"不存在或跨公司"与"状态不允许"，与 dispatch 语义一致
func (s *GormStore) transitionAssetRequest(ctx context.Context, companyID, id int64, toStatus int, extra map[string]any) (model.AssetRequest, error) {
	updates := map[string]any{"status": toStatus}
	for k, v := range extra {
		updates[k] = v
	}
	res := s.db.WithContext(ctx).Model(&model.AssetRequest{}).
		Where("id = ? AND company_id = ? AND status = ?", id, companyID, model.AssetRequestStatusPending).
		Updates(updates)
	if res.Error != nil {
		return model.AssetRequest{}, fmt.Errorf("update asset request status: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		var exists int64
		if err := s.db.WithContext(ctx).Model(&model.AssetRequest{}).
			Where("id = ? AND company_id = ?", id, companyID).
			Count(&exists).Error; err != nil {
			return model.AssetRequest{}, fmt.Errorf("check asset request exists: %w", err)
		}
		if exists == 0 {
			return model.AssetRequest{}, ErrNotFound
		}
		return model.AssetRequest{}, ErrInvalidState
	}

	var r model.AssetRequest
	if err := s.db.WithContext(ctx).Preload("Asset").First(&r, id).Error; err != nil {
		return model.AssetRequest{}, fmt.Errorf("reload asset request: %w", err)
	}
	return r, nil
}

// RejectAssetRequest 驳回：记录决策人与驳回原因（ApprovedBy/ApprovedAt 承载
// "决策人"语义，通过/驳回共用）
func (s *GormStore) RejectAssetRequest(ctx context.Context, companyID, id int64, approverID int64, remark string, now time.Time) (model.AssetRequest, error) {
	return s.transitionAssetRequest(ctx, companyID, id, model.AssetRequestStatusRejected, map[string]any{
		"approved_by":     approverID,
		"approved_at":     now,
		"decision_remark": remark,
		"updated_at":      now,
	})
}

// CancelAssetRequest 申请人撤回（仅待审批可撤）
func (s *GormStore) CancelAssetRequest(ctx context.Context, companyID, id int64) (model.AssetRequest, error) {
	return s.transitionAssetRequest(ctx, companyID, id, model.AssetRequestStatusCanceled, nil)
}
