package store

import (
	"context"
	"errors"
	"testing"

	"itagent/internal/server/model"
)

// P2 耗材管理：store 契约测试（SQLiteStore 实现）。
// 契约要点：名称同公司唯一（软删不占名）；库存只经流水变更且建账恒 0；
// 流水带符号（入库正/出库负/调整非零）与库存不足哨兵；出库不得击穿
// 零库存（条件更新原子防并发超卖）；流水追加式无修改删除面

func newConsumable(companyID int64, name string) model.Consumable {
	return model.Consumable{
		CompanyID: companyID, Name: name, Spec: "70g/500张/包", Unit: "包", MinQuantity: 10,
	}
}

func stockIn(t *testing.T, s Store, ctx context.Context, companyID, id int64, qty int) model.Consumable {
	t.Helper()
	txn, c, err := s.CreateConsumableTxn(ctx, model.ConsumableTxn{
		CompanyID: companyID, ConsumableID: id, Type: model.ConsumableTxnStockIn, Delta: qty,
		OperatorID: 7, OperatorName: "库管员",
	})
	if err != nil {
		t.Fatalf("stock-in %d: %v", qty, err)
	}
	if txn.ID == 0 || txn.Delta != qty {
		t.Fatalf("stock-in txn mismatch: %+v", txn)
	}
	return c
}

func TestConsumableLifecycle(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	// 建账：库存恒 0（期初库存走第一笔入库流水，账实可追溯）
	created, err := s.CreateConsumable(ctx, newConsumable(1, "A4 打印纸"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID == 0 || created.Stock != 0 || created.MinQuantity != 10 {
		t.Fatalf("created mismatch: %+v", created)
	}
	// 名称同公司唯一；软删除记录不占名（维表同口径）
	if _, err := s.CreateConsumable(ctx, newConsumable(1, "A4 打印纸")); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("duplicate name must be ErrAlreadyExists, got %v", err)
	}
	if _, err := s.CreateConsumable(ctx, newConsumable(2, "A4 打印纸")); err != nil {
		t.Fatalf("other company same name must pass: %v", err)
	}
	// 名称 trim 落库；必填校验
	if _, err := s.CreateConsumable(ctx, model.Consumable{CompanyID: 1, Name: "  硒鼓  "}); err != nil {
		t.Fatalf("create trim: %v", err)
	}
	for _, bad := range []model.Consumable{
		{CompanyID: 0, Name: "x"},
		{CompanyID: 1, Name: "  "},
	} {
		if _, err := s.CreateConsumable(ctx, bad); err == nil {
			t.Fatalf("must reject %+v", bad)
		}
	}

	// 列表：公司边界 + 关键字
	items, total, err := s.ListConsumables(ctx, ConsumableListFilter{CompanyID: 1, Page: 1, PageSize: 10})
	if err != nil || total != 2 || len(items) != 2 {
		t.Fatalf("list: total=%d err=%v", total, err)
	}
	if _, total, _ = s.ListConsumables(ctx, ConsumableListFilter{CompanyID: 1, Keyword: "硒鼓", Page: 1, PageSize: 10}); total != 1 {
		t.Fatalf("keyword: total=%d", total)
	}
	if _, total, _ = s.ListConsumables(ctx, ConsumableListFilter{CompanyID: 2, Page: 1, PageSize: 10}); total != 1 {
		t.Fatalf("cross-company list: total=%d", total)
	}

	// 编辑：只动元数据（名称/规格/单位/预警线/备注），库存不经编辑面；
	// 重名 409；跨公司 NotFound
	updated, err := s.UpdateConsumable(ctx, model.Consumable{
		BaseModel: model.BaseModel{ID: created.ID},
		CompanyID: 1, Name: "A4 打印纸", Spec: "80g/500张/包", Unit: "包", MinQuantity: 20,
	})
	if err != nil || updated.MinQuantity != 20 || updated.Spec != "80g/500张/包" {
		t.Fatalf("update: %+v err=%v", updated, err)
	}
	if updated.Stock != 0 {
		t.Fatalf("update must not touch stock, got %d", updated.Stock)
	}
	if _, err := s.UpdateConsumable(ctx, model.Consumable{
		BaseModel: model.BaseModel{ID: created.ID}, CompanyID: 1, Name: "硒鼓",
	}); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("rename to taken name must 409, got %v", err)
	}
	if _, err := s.UpdateConsumable(ctx, model.Consumable{
		BaseModel: model.BaseModel{ID: created.ID}, CompanyID: 2, Name: "别家的",
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-company update must be NotFound, got %v", err)
	}

	// 删除：软删；删除后同名可重建（流水历史仍在，台账面按删除收敛）
	if err := s.DeleteConsumable(ctx, 1, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := s.DeleteConsumable(ctx, 1, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("re-delete must be NotFound, got %v", err)
	}
	if _, err := s.CreateConsumable(ctx, newConsumable(1, "A4 打印纸")); err != nil {
		t.Fatalf("recreate after delete: %v", err)
	}
}

func TestConsumableStockLedger(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	c, err := s.CreateConsumable(ctx, newConsumable(1, "无线鼠标"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// 入库 +50 → 出库 -3（输入正数量、落库负增量）→ 调整 -2（盘亏）
	afterIn := stockIn(t, s, ctx, 1, c.ID, 50)
	if afterIn.Stock != 50 {
		t.Fatalf("stock after in: %d", afterIn.Stock)
	}
	txn, afterOut, err := s.CreateConsumableTxn(ctx, model.ConsumableTxn{
		CompanyID: 1, ConsumableID: c.ID, Type: model.ConsumableTxnStockOut,
		Delta: -3, Recipient: "张三", OperatorID: 7, OperatorName: "库管员",
	})
	if err != nil || afterOut.Stock != 47 || txn.Delta != -3 || txn.Recipient != "张三" {
		t.Fatalf("stock-out: c=%+v txn=%+v err=%v", afterOut, txn, err)
	}
	if _, afterAdjust, err := s.CreateConsumableTxn(ctx, model.ConsumableTxn{
		CompanyID: 1, ConsumableID: c.ID, Type: model.ConsumableTxnAdjust,
		Delta: -2, OperatorID: 7, OperatorName: "库管员",
	}); err != nil || afterAdjust.Stock != 45 {
		t.Fatalf("adjust: %+v err=%v", afterAdjust, err)
	}

	// 出库击穿零库存 → ErrInsufficient（条件更新原子拦截，库存不动）
	if _, _, err := s.CreateConsumableTxn(ctx, model.ConsumableTxn{
		CompanyID: 1, ConsumableID: c.ID, Type: model.ConsumableTxnStockOut,
		Delta: -100, OperatorID: 7, OperatorName: "库管员",
	}); !errors.Is(err, ErrInsufficient) {
		t.Fatalf("oversell must be ErrInsufficient, got %v", err)
	}
	reloaded, _, _ := s.ListConsumables(ctx, ConsumableListFilter{CompanyID: 1, Page: 1, PageSize: 10})
	if reloaded[0].Stock != 45 {
		t.Fatalf("stock must stay 45 after rejected out, got %d", reloaded[0].Stock)
	}

	// 流水校验：类型非法 / 符号违规 / 零调整 / 缺操作人 / 跨公司耗材
	for _, bad := range []model.ConsumableTxn{
		{CompanyID: 1, ConsumableID: c.ID, Type: "bogus", Delta: 1, OperatorID: 1, OperatorName: "x"},
		{CompanyID: 1, ConsumableID: c.ID, Type: model.ConsumableTxnStockIn, Delta: -1, OperatorID: 1, OperatorName: "x"},
		{CompanyID: 1, ConsumableID: c.ID, Type: model.ConsumableTxnStockIn, Delta: 0, OperatorID: 1, OperatorName: "x"},
		{CompanyID: 1, ConsumableID: c.ID, Type: model.ConsumableTxnStockOut, Delta: 3, OperatorID: 1, OperatorName: "x"},
		{CompanyID: 1, ConsumableID: c.ID, Type: model.ConsumableTxnAdjust, Delta: 0, OperatorID: 1, OperatorName: "x"},
		{CompanyID: 1, ConsumableID: c.ID, Type: model.ConsumableTxnStockIn, Delta: 1, OperatorID: 0, OperatorName: "x"},
		{CompanyID: 1, ConsumableID: c.ID, Type: model.ConsumableTxnStockIn, Delta: 1, OperatorID: 1, OperatorName: " "},
		{CompanyID: 1, ConsumableID: c.ID + 999, Type: model.ConsumableTxnStockIn, Delta: 1, OperatorID: 1, OperatorName: "x"},
		{CompanyID: 2, ConsumableID: c.ID, Type: model.ConsumableTxnStockIn, Delta: 1, OperatorID: 1, OperatorName: "x"},
	} {
		if _, _, err := s.CreateConsumableTxn(ctx, bad); err == nil {
			t.Fatalf("must reject txn %+v", bad)
		}
	}

	// 流水台账：类型过滤 + 耗材过滤 + 倒序（最新在前）+ 分页 total
	txns, total, err := s.ListConsumableTxns(ctx, ConsumableTxnListFilter{CompanyID: 1, Page: 1, PageSize: 10})
	if err != nil || total != 3 || len(txns) != 3 {
		t.Fatalf("ledger: total=%d err=%v", total, err)
	}
	if txns[0].Type != model.ConsumableTxnAdjust || txns[2].Type != model.ConsumableTxnStockIn {
		t.Fatalf("ledger order: %+v", txns)
	}
	if _, total, _ = s.ListConsumableTxns(ctx, ConsumableTxnListFilter{
		CompanyID: 1, Type: model.ConsumableTxnStockOut, Page: 1, PageSize: 10,
	}); total != 1 {
		t.Fatalf("type filter: total=%d", total)
	}
	if _, total, _ = s.ListConsumableTxns(ctx, ConsumableTxnListFilter{CompanyID: 2, Page: 1, PageSize: 10}); total != 0 {
		t.Fatalf("cross-company ledger must be empty, got %d", total)
	}
}

func TestConsumableLowStockFilter(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	low, _ := s.CreateConsumable(ctx, model.Consumable{CompanyID: 1, Name: "键鼠套装", MinQuantity: 10})
	_ = stockIn(t, s, ctx, 1, low.ID, 8) // 8 ≤ 10 → 触线
	healthy, _ := s.CreateConsumable(ctx, model.Consumable{CompanyID: 1, Name: "显示器支架", MinQuantity: 5})
	_ = stockIn(t, s, ctx, 1, healthy.ID, 20) // 20 > 5 → 健康
	noLine, _ := s.CreateConsumable(ctx, model.Consumable{CompanyID: 1, Name: "网线", MinQuantity: 0})
	_ = noLine // 未配预警线不进预警口径（库存恒 0 也不算预警）

	// 预警过滤：min_quantity > 0 且 stock <= min_quantity
	items, total, err := s.ListConsumables(ctx, ConsumableListFilter{CompanyID: 1, LowStock: boolPtr(true), Page: 1, PageSize: 10})
	if err != nil || total != 1 || len(items) != 1 || items[0].ID != low.ID {
		t.Fatalf("low-stock filter: total=%d items=%v err=%v", total, items, err)
	}
	// 健康过滤
	if _, total, _ = s.ListConsumables(ctx, ConsumableListFilter{CompanyID: 1, LowStock: boolPtr(false), Page: 1, PageSize: 10}); total != 2 {
		t.Fatalf("healthy filter: total=%d", total)
	}
}

func boolPtr(b bool) *bool { return &b }
