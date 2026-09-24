package store

import (
	"context"
	"errors"
	"testing"

	"itagent/internal/server/model"
)

// P1 维度治理：四张维表的 store 契约测试（SQLiteStore 实现）。
// 契约要点：名称同公司唯一（软删除不占名可重建）、名称 trim 落库、
// 公司边界（跨公司一律 NotFound）、keyword 模糊 + 分页、更新需主键

func TestManufacturerLifecycle(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	created, err := s.CreateManufacturer(ctx, model.Manufacturer{CompanyID: 1, Name: "  联想  ", Remark: "Lenovo"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID == 0 || created.Name != "联想" {
		t.Fatalf("create should trim and persist name, got %+v", created)
	}

	// 同名同公司 → 冲突；同名跨公司 → 放行
	if _, err := s.CreateManufacturer(ctx, model.Manufacturer{CompanyID: 1, Name: "联想"}); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("duplicate name must be ErrAlreadyExists, got %v", err)
	}
	if _, err := s.CreateManufacturer(ctx, model.Manufacturer{CompanyID: 2, Name: "联想"}); err != nil {
		t.Fatalf("same name in other company must pass: %v", err)
	}
	if _, err := s.CreateManufacturer(ctx, model.Manufacturer{CompanyID: 1, Name: "DELL"}); err != nil {
		t.Fatalf("create DELL: %v", err)
	}

	// 必填校验
	if _, err := s.CreateManufacturer(ctx, model.Manufacturer{CompanyID: 0, Name: "x"}); err == nil {
		t.Fatal("company_id required")
	}
	if _, err := s.CreateManufacturer(ctx, model.Manufacturer{CompanyID: 1, Name: "  "}); err == nil {
		t.Fatal("blank name required")
	}

	// 列表：keyword + 分页 + 公司边界
	items, total, err := s.ListManufacturers(ctx, DimensionListFilter{CompanyID: 1, Keyword: "联想", Page: 1, PageSize: 10})
	if err != nil || total != 1 || len(items) != 1 || items[0].ID != created.ID {
		t.Fatalf("list keyword: total=%d items=%v err=%v", total, items, err)
	}
	items, total, err = s.ListManufacturers(ctx, DimensionListFilter{CompanyID: 1, Page: 1, PageSize: 1})
	if err != nil || total != 2 || len(items) != 1 {
		t.Fatalf("list pagination: total=%d len=%d err=%v", total, len(items), err)
	}
	if _, total, err = s.ListManufacturers(ctx, DimensionListFilter{CompanyID: 2, Page: 1, PageSize: 10}); err != nil || total != 1 {
		t.Fatalf("company boundary: total=%d err=%v", total, err)
	}

	// 更新：改名成功；改成他人已占名 → 冲突；保留自身名 → 放行；跨公司 → NotFound；无主键 → 报错
	created.Name = "Lenovo 联想"
	updated, err := s.UpdateManufacturer(ctx, created)
	if err != nil || updated.Name != "Lenovo 联想" {
		t.Fatalf("update: %v %+v", err, updated)
	}
	if _, err := s.UpdateManufacturer(ctx, model.Manufacturer{BaseModel: model.BaseModel{ID: created.ID}, CompanyID: 1, Name: "DELL"}); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("rename to occupied name must conflict, got %v", err)
	}
	if _, err := s.UpdateManufacturer(ctx, created); err != nil {
		t.Fatalf("keep own name must pass: %v", err)
	}
	wrong := created
	wrong.CompanyID = 2
	if _, err := s.UpdateManufacturer(ctx, wrong); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-company update must be NotFound, got %v", err)
	}
	noID := created
	noID.ID = 0
	if _, err := s.UpdateManufacturer(ctx, noID); err == nil {
		t.Fatal("update without id must fail")
	}

	// 删除：跨公司 NotFound → 删除 → 幂等 NotFound → 软删后名字可重建
	if err := s.DeleteManufacturer(ctx, 2, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-company delete must be NotFound, got %v", err)
	}
	if err := s.DeleteManufacturer(ctx, 1, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := s.DeleteManufacturer(ctx, 1, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete must be NotFound, got %v", err)
	}
	if _, err := s.CreateManufacturer(ctx, model.Manufacturer{CompanyID: 1, Name: "联想"}); err != nil {
		t.Fatalf("name freed after soft delete: %v", err)
	}
}

func TestSupplierLifecycle(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	created, err := s.CreateSupplier(ctx, model.Supplier{
		CompanyID: 3, Name: "京东采购", ContactName: "王经理", Phone: "13800000000", Remark: "电商渠道",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ContactName != "王经理" || created.Phone != "13800000000" {
		t.Fatalf("fields not persisted: %+v", created)
	}

	if _, err := s.CreateSupplier(ctx, model.Supplier{CompanyID: 3, Name: "京东采购"}); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("duplicate must conflict, got %v", err)
	}

	items, total, err := s.ListSuppliers(ctx, DimensionListFilter{CompanyID: 3, Keyword: "京东", Page: 1, PageSize: 10})
	if err != nil || total != 1 || len(items) != 1 || items[0].ContactName != "王经理" {
		t.Fatalf("list: total=%d err=%v", total, err)
	}

	created.Phone = "13900000000"
	updated, err := s.UpdateSupplier(ctx, created)
	if err != nil || updated.Phone != "13900000000" {
		t.Fatalf("update: %v %+v", err, updated)
	}

	if err := s.DeleteSupplier(ctx, 3, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, total, err = s.ListSuppliers(ctx, DimensionListFilter{CompanyID: 3, Page: 1, PageSize: 10}); err != nil || total != 0 {
		t.Fatalf("deleted supplier must not list: total=%d err=%v", total, err)
	}
}

func TestLocationLifecycle(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	hq, err := s.CreateLocation(ctx, model.Location{CompanyID: 5, Name: "总部大楼"})
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}
	if hq.ParentID != nil {
		t.Fatal("top-level location must have nil parent")
	}

	floor, err := s.CreateLocation(ctx, model.Location{CompanyID: 5, Name: "3F 办公区", ParentID: &hq.ID})
	if err != nil {
		t.Fatalf("create child: %v", err)
	}
	if floor.ParentID == nil || *floor.ParentID != hq.ID {
		t.Fatalf("parent not persisted: %+v", floor)
	}

	if _, err := s.CreateLocation(ctx, model.Location{CompanyID: 5, Name: "总部大楼"}); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("duplicate must conflict, got %v", err)
	}

	items, total, err := s.ListLocations(ctx, DimensionListFilter{CompanyID: 5, Page: 1, PageSize: 10})
	if err != nil || total != 2 || len(items) != 2 {
		t.Fatalf("list: total=%d err=%v", total, err)
	}

	// 改挂父节点 + 清回顶级
	other, err := s.CreateLocation(ctx, model.Location{CompanyID: 5, Name: "仓库区"})
	if err != nil {
		t.Fatalf("create other: %v", err)
	}
	other.ParentID = &hq.ID
	if _, err := s.UpdateLocation(ctx, other); err != nil {
		t.Fatalf("re-parent: %v", err)
	}
	other.ParentID = nil
	if _, err := s.UpdateLocation(ctx, other); err != nil {
		t.Fatalf("clear parent: %v", err)
	}

	if err := s.DeleteLocation(ctx, 5, floor.ID); err != nil {
		t.Fatalf("delete child: %v", err)
	}
	// 子位置被删后父位置不受影响；父位置删除由 API 层做引用拦截
	if err := s.DeleteLocation(ctx, 5, hq.ID); err != nil {
		t.Fatalf("delete parent: %v", err)
	}
}

func TestAssetModelLifecycle(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	mfr, err := s.CreateManufacturer(ctx, model.Manufacturer{CompanyID: 9, Name: "联想"})
	if err != nil {
		t.Fatalf("seed manufacturer: %v", err)
	}
	rule, err := s.CreateDepreciationRule(ctx, newRule(9, "3年折旧", 36, "percent", 0.05, ""))
	if err != nil {
		t.Fatalf("seed rule: %v", err)
	}

	created, err := s.CreateAssetModel(ctx, model.AssetModel{
		CompanyID: 9, Name: "ThinkPad X1 Carbon Gen 11", CategoryID: model.AssetCategoryNotebook,
		ManufacturerID: &mfr.ID, DepreciationID: &rule.ID, EOLMonths: 60,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.CategoryID != model.AssetCategoryNotebook || created.EOLMonths != 60 ||
		created.ManufacturerID == nil || *created.ManufacturerID != mfr.ID ||
		created.DepreciationID == nil || *created.DepreciationID != rule.ID {
		t.Fatalf("fields not persisted: %+v", created)
	}

	if _, err := s.CreateAssetModel(ctx, model.AssetModel{CompanyID: 9, Name: "ThinkPad X1 Carbon Gen 11"}); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("duplicate must conflict, got %v", err)
	}

	// 类别过滤 + keyword
	if _, err := s.CreateAssetModel(ctx, model.AssetModel{CompanyID: 9, Name: "P27h-30 显示器", CategoryID: model.AssetCategoryMonitor}); err != nil {
		t.Fatalf("create monitor model: %v", err)
	}
	items, total, err := s.ListAssetModels(ctx, AssetModelListFilter{CompanyID: 9, CategoryID: model.AssetCategoryMonitor, Page: 1, PageSize: 10})
	if err != nil || total != 1 || len(items) != 1 || items[0].Name != "P27h-30 显示器" {
		t.Fatalf("category filter: total=%d err=%v", total, err)
	}
	items, total, err = s.ListAssetModels(ctx, AssetModelListFilter{CompanyID: 9, Keyword: "X1", Page: 1, PageSize: 10})
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("keyword filter: total=%d err=%v", total, err)
	}
	if _, total, err = s.ListAssetModels(ctx, AssetModelListFilter{CompanyID: 9, Page: 1, PageSize: 10}); err != nil || total != 2 {
		t.Fatalf("list all: total=%d err=%v", total, err)
	}

	// 更新：清空厂商/折旧挂接 + 改 EOL
	created.ManufacturerID = nil
	created.DepreciationID = nil
	created.EOLMonths = 48
	updated, err := s.UpdateAssetModel(ctx, created)
	if err != nil || updated.EOLMonths != 48 || updated.ManufacturerID != nil || updated.DepreciationID != nil {
		t.Fatalf("update clear FKs: %v %+v", err, updated)
	}

	// 跨公司/无主键
	wrong := created
	wrong.CompanyID = 8
	if _, err := s.UpdateAssetModel(ctx, wrong); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-company must be NotFound, got %v", err)
	}
	noID := created
	noID.ID = 0
	if _, err := s.UpdateAssetModel(ctx, noID); err == nil {
		t.Fatal("update without id must fail")
	}

	if err := s.DeleteAssetModel(ctx, 9, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := s.DeleteAssetModel(ctx, 9, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete must be NotFound, got %v", err)
	}
}
