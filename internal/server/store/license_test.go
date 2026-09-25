package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"itagent/internal/server/model"
)

// P2 软件许可管理：store 契约测试（SQLiteStore 实现）。
// 契约要点：名称必填 trim、席位非负、软删除记录不占名、
// 到期窗口过滤（ExpiringDays）只含未终止许可、跨公司一律 NotFound

func newLicense(companyID int64, name string) model.License {
	return model.License{
		CompanyID:  companyID,
		Name:       name,
		Vendor:     "Microsoft",
		Category:   "办公套件",
		LicenseKey: "XXXX-YYYY-ZZZZ",
		TotalSeats: 100,
	}
}

func TestLicenseCRUDLifecycle(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	created, err := s.CreateLicense(ctx, newLicense(1, "Microsoft 365 商业高级版"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID == 0 || created.TotalSeats != 100 || created.LicenseKey != "XXXX-YYYY-ZZZZ" {
		t.Fatalf("created mismatch: %+v", created)
	}

	// 列表 + 关键字过滤；名称 trim 后落库
	items, total, err := s.ListLicenses(ctx, LicenseListFilter{CompanyID: 1, Page: 1, PageSize: 10})
	if err != nil || total != 1 || len(items) != 1 || items[0].Name != "Microsoft 365 商业高级版" {
		t.Fatalf("list: total=%d items=%v err=%v", total, items, err)
	}
	if _, total, _ = s.ListLicenses(ctx, LicenseListFilter{CompanyID: 1, Keyword: "365", Page: 1, PageSize: 10}); total != 1 {
		t.Fatalf("keyword 365: total=%d", total)
	}
	if _, total, _ = s.ListLicenses(ctx, LicenseListFilter{CompanyID: 1, Keyword: "AutoCAD", Page: 1, PageSize: 10}); total != 0 {
		t.Fatalf("keyword autocad must be empty, got %d", total)
	}
	// 公司边界
	if _, total, _ = s.ListLicenses(ctx, LicenseListFilter{CompanyID: 2, Page: 1, PageSize: 10}); total != 0 {
		t.Fatalf("cross-company list must be empty, got %d", total)
	}

	// 详情：跨公司一律 NotFound
	got, err := s.GetLicense(ctx, 1, created.ID)
	if err != nil || got.ID != created.ID {
		t.Fatalf("get: %+v err=%v", got, err)
	}
	if _, err = s.GetLicense(ctx, 2, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-company get must be NotFound, got %v", err)
	}

	// 更新：改席位/密钥；跨公司 NotFound
	updated, err := s.UpdateLicense(ctx, model.License{
		BaseModel:       model.BaseModel{ID: created.ID},
		CompanyID:       1,
		Name:            "Microsoft 365 商业高级版",
		TotalSeats:      50,
		LicenseKey:      "NEW-KEY",
		ExpirationDate:  nil,
		TerminationDate: nil,
	})
	if err != nil || updated.TotalSeats != 50 || updated.LicenseKey != "NEW-KEY" {
		t.Fatalf("update: %+v err=%v", updated, err)
	}
	_, err = s.UpdateLicense(ctx, model.License{
		BaseModel: model.BaseModel{ID: created.ID},
		CompanyID: 2, Name: "x",
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-company update must be NotFound, got %v", err)
	}

	// 删除：不存在/跨公司 NotFound；删除后可重建同名（软删不占名）
	if err := s.DeleteLicense(ctx, 1, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := s.DeleteLicense(ctx, 1, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("re-delete must be NotFound, got %v", err)
	}
	if _, total, _ = s.ListLicenses(ctx, LicenseListFilter{CompanyID: 1, Page: 1, PageSize: 10}); total != 0 {
		t.Fatalf("deleted license must leave list, got %d", total)
	}
	if _, err := s.CreateLicense(ctx, newLicense(1, "Microsoft 365 商业高级版")); err != nil {
		t.Fatalf("recreate after delete: %v", err)
	}
}

func TestLicenseValidation(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	for name, bad := range map[string]model.License{
		"company required": {Name: "Office"},
		"name required":    {CompanyID: 1, Name: "   "},
		"negative seats":   {CompanyID: 1, Name: "Office", TotalSeats: -1},
	} {
		if _, err := s.CreateLicense(ctx, bad); err == nil {
			t.Fatalf("must reject %s: %+v", name, bad)
		}
	}

	// 名称 trim 后落库（治理口径与其他维表一致）
	created, err := s.CreateLicense(ctx, model.License{CompanyID: 1, Name: "  WPS Office  ", TotalSeats: 0})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Name != "WPS Office" {
		t.Fatalf("name must be trimmed, got %q", created.Name)
	}
}

func TestLicenseExpiringWindowFilter(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	seed := func(name string, expiration, termination *time.Time) {
		t.Helper()
		l := model.License{CompanyID: 7, Name: name, TotalSeats: 10, ExpirationDate: expiration, TerminationDate: termination}
		if _, err := s.CreateLicense(ctx, l); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	in30 := now.AddDate(0, 0, 30)
	in60 := now.AddDate(0, 0, 60)
	past := now.AddDate(0, 0, -5)
	seed("窗口内", &in30, nil)
	seed("窗口外", &in60, nil)
	seed("已过期", &past, nil)
	seed("永久授权", nil, nil)
	seed("已终止但在窗口内", &in30, &past)

	// ExpiringDays=45：到期日在 (now, now+45d] 且未终止
	//（永久授权无到期日不进窗口；到期日当天属于窗口内——闭区间）
	items, total, err := s.ListLicenses(ctx, LicenseListFilter{CompanyID: 7, ExpiringDays: 45, Page: 1, PageSize: 10})
	if err != nil || total != 1 || len(items) != 1 || items[0].Name != "窗口内" {
		t.Fatalf("expiring window: total=%d items=%+v err=%v", total, items, err)
	}
	// 不带窗口参数：全量（含过期/终止/永久）
	_, total, _ = s.ListLicenses(ctx, LicenseListFilter{CompanyID: 7, Page: 1, PageSize: 10})
	if total != 5 {
		t.Fatalf("no window must list all 5, got %d", total)
	}
}
