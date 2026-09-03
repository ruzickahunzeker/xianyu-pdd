package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
)

func TestPDDNormalPriceCent(t *testing.T) {
	if got := pddNormalPriceCent(`{"normal_price":"39.9"}`); got != 3990 {
		t.Fatalf("normal price=%d, want 3990", got)
	}
	if got := pddNormalPriceCent(`{"group_price":"26.9"}`); got != 0 {
		t.Fatalf("missing normal price=%d, want 0", got)
	}
}

func TestSplitMaterialPreservesStableSourceIdentity(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	skus := `[{"material_sku_id":"stable-a","sku_type":"source","source_goods_id":"goods-1","source_sku_id":"pdd-a","price_cent":100,"quantity":8,"enabled":true,"properties":[{"name":"颜色","value":"红"}]},{"material_sku_id":"stable-b","sku_type":"source","source_goods_id":"goods-1","source_sku_id":"pdd-b","price_cent":200,"quantity":9,"enabled":true,"properties":[{"name":"颜色","value":"蓝"}]}]`
	result, err := store.DB.Exec(`INSERT INTO product_materials(user_id,source_type,source_id,title,description,images_json,category_json,skus_json,status,created_at,updated_at) VALUES(1,'pdd','goods-1','原素材','描述','["https://img/1.jpg"]','{}',?,'draft',1,1)`, skus)
	if err != nil {
		t.Fatal(err)
	}
	materialID, _ := result.LastInsertId()
	payload := `{"idempotency_key":"split-test-1","groups":[{"name":"红色组","title":"红色商品","material_sku_ids":["stable-a"]},{"name":"蓝色组","material_sku_ids":["stable-b"]}]}`
	body := strings.NewReader(payload)
	req := httptest.NewRequest(http.MethodPost, "/materials/1/split", body)
	route := chi.NewRouteContext()
	route.URLParams.Add("id", strconv.FormatInt(materialID, 10))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, route))
	req = req.WithContext(auth.WithSession(req.Context(), &db.Session{UserID: 1, Username: "admin", IsAdmin: true}))
	rec := httptest.NewRecorder()
	srv.splitMaterial(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("split status=%d body=%s", rec.Code, rec.Body.String())
	}
	var splitSource, childCount int
	if err = store.DB.QueryRow(`SELECT is_split_source FROM product_materials WHERE id=?`, materialID).Scan(&splitSource); err != nil || splitSource != 1 {
		t.Fatalf("split source=%d err=%v", splitSource, err)
	}
	if err = store.DB.QueryRow(`SELECT COUNT(*) FROM product_materials WHERE parent_material_id=? AND deleted_at IS NULL`, materialID).Scan(&childCount); err != nil || childCount != 2 {
		t.Fatalf("child count=%d err=%v", childCount, err)
	}
	var childRaw string
	if err = store.DB.QueryRow(`SELECT skus_json FROM product_materials WHERE parent_material_id=? AND split_group_name='红色组'`, materialID).Scan(&childRaw); err != nil {
		t.Fatal(err)
	}
	var childSKUs []materialSKU
	if json.Unmarshal([]byte(childRaw), &childSKUs) != nil || len(childSKUs) != 1 || childSKUs[0].MaterialSKUID != "stable-a" || childSKUs[0].SourceSKUID != "pdd-a" {
		t.Fatalf("child identity changed: %s", childRaw)
	}
	replayReq := httptest.NewRequest(http.MethodPost, "/materials/1/split", strings.NewReader(payload))
	replayRoute := chi.NewRouteContext()
	replayRoute.URLParams.Add("id", strconv.FormatInt(materialID, 10))
	replayReq = replayReq.WithContext(context.WithValue(replayReq.Context(), chi.RouteCtxKey, replayRoute))
	replayReq = replayReq.WithContext(auth.WithSession(replayReq.Context(), &db.Session{UserID: 1, Username: "admin", IsAdmin: true}))
	replayRec := httptest.NewRecorder()
	srv.splitMaterial(replayRec, replayReq)
	if replayRec.Code != http.StatusOK || !strings.Contains(replayRec.Body.String(), `"replayed":true`) {
		t.Fatalf("replay status=%d body=%s", replayRec.Code, replayRec.Body.String())
	}
	if err = store.DB.QueryRow(`SELECT COUNT(*) FROM product_materials WHERE parent_material_id=? AND deleted_at IS NULL`, materialID).Scan(&childCount); err != nil || childCount != 2 {
		t.Fatalf("idempotent child count=%d err=%v", childCount, err)
	}
	var redChildID int64
	if err = store.DB.QueryRow(`SELECT id FROM product_materials WHERE parent_material_id=? AND split_group_name='红色组'`, materialID).Scan(&redChildID); err != nil {
		t.Fatal(err)
	}
	deleteReq := httptest.NewRequest(http.MethodDelete, "/materials/child", nil)
	deleteRoute := chi.NewRouteContext()
	deleteRoute.URLParams.Add("id", strconv.FormatInt(redChildID, 10))
	deleteReq = deleteReq.WithContext(context.WithValue(deleteReq.Context(), chi.RouteCtxKey, deleteRoute))
	deleteReq = deleteReq.WithContext(auth.WithSession(deleteReq.Context(), &db.Session{UserID: 1, Username: "admin", IsAdmin: true}))
	deleteRec := httptest.NewRecorder()
	srv.deleteMaterial(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("delete child status=%d body=%s", deleteRec.Code, deleteRec.Body.String())
	}
	var assignmentCount int
	if err = store.DB.QueryRow(`SELECT COUNT(*) FROM material_split_assignments WHERE child_material_id=?`, redChildID).Scan(&assignmentCount); err != nil || assignmentCount != 0 {
		t.Fatalf("released assignment count=%d err=%v", assignmentCount, err)
	}
}

func TestMaterialSourcePriceBackfillAndSyncPreserveSalePrice(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	_, err := store.DB.Exec(`INSERT INTO pdd_products(id,goods_id,mall_sn,final_url,title,images_json,first_collected_at,last_collected_at) VALUES(1,'123','','https://pdd/123','商品','[]',10,20)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.DB.Exec(`INSERT INTO pdd_skus(product_id,goods_id,sku_id,specs_json,spec_value_ids_json,thumb_url,prices_json,price_cent,stock,is_onsale,raw_snapshot_json,last_collected_at) VALUES(1,'123','456','[{"spec_key":"颜色","raw_value":"黑色"}]','[]','','{"normal_price":"19.9"}',1000,8,1,'{}',20)`)
	if err != nil {
		t.Fatal(err)
	}
	oldSKUs := `[{"material_sku_id":"material-1","source_sku_id":"456","price_cent":8800,"quantity":2,"enabled":true,"properties":[{"name":"颜色","value":"黑色"}]}]`
	result, err := store.DB.Exec(`INSERT INTO product_materials(user_id,source_type,source_id,title,description,images_json,category_json,skus_json,status,created_at,updated_at) VALUES(1,'pdd','123','素材','描述','[]','{}',?,'draft',1,1)`, oldSKUs)
	if err != nil {
		t.Fatal(err)
	}
	materialID, _ := result.LastInsertId()
	material, err := scanMaterial(store.DB.QueryRow(`SELECT id,user_id,source_type,source_id,title,description,images_json,category_json,skus_json,postage_mode,postage_cent,status,created_at,updated_at,image_property_name,video_enabled,videos_json,parent_material_id,split_batch_id,split_group_name,is_split_source FROM product_materials WHERE id=?`, materialID))
	if err != nil {
		t.Fatal(err)
	}
	if !srv.backfillMaterialSourcePrices(context.Background(), material) {
		t.Fatal("expected old material source price to be backfilled")
	}
	var backfilled []materialSKU
	raw, _ := json.Marshal(material["skus"])
	_ = json.Unmarshal(raw, &backfilled)
	if backfilled[0].PriceCents != 8800 || backfilled[0].SourcePriceCents != 1000 || backfilled[0].SourceNormalPriceCents != 1990 || backfilled[0].SourcePriceOrigin != "backfilled_current" {
		t.Fatalf("backfilled sku=%+v", backfilled[0])
	}
	_, err = store.DB.Exec(`UPDATE pdd_skus SET prices_json='{"normal_price":"22.9"}',price_cent=1200,stock=9,last_collected_at=30 WHERE goods_id='123' AND sku_id='456'`)
	if err != nil {
		t.Fatal(err)
	}
	body := strings.NewReader(`{"prices":true,"stock":true,"images":false,"add_new":false,"disable_removed":true}`)
	req := httptest.NewRequest(http.MethodPost, "/materials/1/sync-source", body)
	route := chi.NewRouteContext()
	route.URLParams.Add("id", strconv.FormatInt(materialID, 10))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, route))
	req = req.WithContext(auth.WithSession(req.Context(), &db.Session{UserID: 1, Username: "admin", IsAdmin: true}))
	rec := httptest.NewRecorder()
	srv.syncMaterialSource(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("sync status=%d body=%s", rec.Code, rec.Body.String())
	}
	var savedRaw string
	if err = store.DB.QueryRow(`SELECT skus_json FROM product_materials WHERE id=?`, materialID).Scan(&savedRaw); err != nil {
		t.Fatal(err)
	}
	var saved []materialSKU
	_ = json.Unmarshal([]byte(savedRaw), &saved)
	if saved[0].PriceCents != 8800 || saved[0].SourcePriceCents != 1200 || saved[0].SourceNormalPriceCents != 2290 || saved[0].Quantity != 9 || saved[0].SourcePriceOrigin != "synced" {
		t.Fatalf("synced sku=%+v", saved[0])
	}
}

func TestNormalizePublishedMaterialSKUZerosUnmappedPDDCombination(t *testing.T) {
	unmapped := map[string]any{"source_sku_id": "", "quantity": int64(1000)}
	normalizePublishedMaterialSKU("pdd", unmapped)
	if got := jsonInt64(unmapped["quantity"]); got != 0 {
		t.Fatalf("unmapped quantity=%d, want 0", got)
	}

	mapped := map[string]any{"source_sku_id": "1883590772174", "quantity": int64(1000)}
	normalizePublishedMaterialSKU("pdd", mapped)
	if got := jsonInt64(mapped["quantity"]); got != 1000 {
		t.Fatalf("mapped quantity=%d, want 1000", got)
	}

	manual := map[string]any{"source_sku_id": "", "quantity": int64(12)}
	normalizePublishedMaterialSKU("manual", manual)
	if got := jsonInt64(manual["quantity"]); got != 12 {
		t.Fatalf("manual quantity=%d, want 12", got)
	}

	pddManual := map[string]any{"sku_type": "manual", "source_sku_id": "", "quantity": int64(12)}
	normalizePublishedMaterialSKU("pdd", pddManual)
	if got := jsonInt64(pddManual["quantity"]); got != 12 {
		t.Fatalf("pdd manual quantity=%d, want 12", got)
	}
}

func TestProtectMaterialSKUIdentitiesPreservesSourceBindingDuringRename(t *testing.T) {
	old := []materialSKU{{MaterialSKUID: "stable-1", SKUType: materialSKUTypeSource, SourceGoodsID: "609274612506", SourceSKUID: "pdd-1", SourceProperties: []materialProperty{{Name: "原规格", Value: "A"}}, Quantity: 9, PriceCents: 100, Properties: []materialProperty{{Name: "款式", Value: "A"}}}}
	incoming := []materialSKU{{MaterialSKUID: "stable-1", Quantity: 7, PriceCents: 200, Enabled: true, Properties: []materialProperty{{Name: "接口类型", Value: "BC车USB-C"}}}}
	if err := protectMaterialSKUIdentities("pdd", "609274612506", old, incoming); err != nil {
		t.Fatal(err)
	}
	got := incoming[0]
	if got.MaterialSKUID != "stable-1" || got.SKUType != materialSKUTypeSource || got.SourceGoodsID != "609274612506" || got.SourceSKUID != "pdd-1" || got.Properties[0].Name != "接口类型" || got.PriceCents != 200 || got.Quantity != 7 {
		t.Fatalf("protected sku=%+v", got)
	}
}

func TestProtectMaterialSKUIdentitiesRejectsRemovedSourceSKU(t *testing.T) {
	old := []materialSKU{{MaterialSKUID: "stable-1", SKUType: materialSKUTypeSource, SourceGoodsID: "goods-1", SourceSKUID: "pdd-1"}}
	incoming := []materialSKU{{MaterialSKUID: "replacement", SKUType: materialSKUTypePlaceholder, PriceCents: 100, Properties: []materialProperty{{Name: "款式", Value: "改名"}}}}
	if err := protectMaterialSKUIdentities("pdd", "goods-1", old, incoming); err == nil || !strings.Contains(err.Error(), "不能通过普通素材编辑删除或重建") {
		t.Fatalf("expected removed source SKU rejection, got %v", err)
	}
}

func TestProtectMaterialSKUIdentitiesAllowsRemovingPlaceholder(t *testing.T) {
	old := []materialSKU{{MaterialSKUID: "placeholder-1", SKUType: materialSKUTypePlaceholder, PriceCents: 100, Properties: []materialProperty{{Name: "款式", Value: "无效组合"}}}}
	if err := protectMaterialSKUIdentities("pdd", "goods-1", old, nil); err != nil {
		t.Fatalf("removing placeholder: %v", err)
	}
}

func TestProtectMaterialSKUIdentitiesDistinguishesManualAndPlaceholder(t *testing.T) {
	incoming := []materialSKU{
		{MaterialSKUID: "manual", SKUType: materialSKUTypeManual, Quantity: 8, PriceCents: 100, Properties: []materialProperty{{Name: "规格", Value: "手工"}}},
		{MaterialSKUID: "placeholder", SKUType: materialSKUTypePlaceholder, Quantity: 8, PriceCents: 100, Properties: []materialProperty{{Name: "规格", Value: "补位"}}},
	}
	if err := protectMaterialSKUIdentities("pdd", "609274612506", nil, incoming); err != nil {
		t.Fatal(err)
	}
	if incoming[0].Quantity != 8 || incoming[1].Quantity != 0 {
		t.Fatalf("manual=%d placeholder=%d", incoming[0].Quantity, incoming[1].Quantity)
	}
}

func TestProtectMaterialSKUIdentitiesRejectsDuplicateStableID(t *testing.T) {
	incoming := []materialSKU{
		{MaterialSKUID: "duplicate", SKUType: materialSKUTypeManual, PriceCents: 100, Properties: []materialProperty{{Name: "规格", Value: "A"}}},
		{MaterialSKUID: "duplicate", SKUType: materialSKUTypeManual, PriceCents: 100, Properties: []materialProperty{{Name: "规格", Value: "B"}}},
	}
	if err := protectMaterialSKUIdentities("pdd", "", nil, incoming); err == nil {
		t.Fatal("duplicate material_sku_id must be rejected")
	}
}

func TestProtectMaterialSKUIdentitiesRejectsDuplicateSourceBinding(t *testing.T) {
	incoming := []materialSKU{
		{MaterialSKUID: "stable-1", SKUType: materialSKUTypeSource, SourceGoodsID: "goods-1", SourceSKUID: "sku-1"},
		{MaterialSKUID: "stable-2", SKUType: materialSKUTypeSource, SourceGoodsID: "goods-1", SourceSKUID: "sku-1"},
	}
	if err := protectMaterialSKUIdentities("pdd", "goods-1", nil, incoming); err == nil {
		t.Fatal("expected duplicate source binding to be rejected")
	}
}

func TestNormalizeCollectedMaterialSpecificationsHidesFixedDimension(t *testing.T) {
	skus := []materialSKU{
		{Enabled: true, Properties: []materialProperty{{Name: "口味", Value: "牛肉"}, {Name: "包装", Value: "90支"}}},
		{Enabled: true, Properties: []materialProperty{{Name: "口味", Value: "鸡肉"}, {Name: "包装", Value: "90支"}}},
	}
	for index := range skus {
		skus[index].SourceProperties = append([]materialProperty(nil), skus[index].Properties...)
	}
	normalizeCollectedMaterialSpecifications(skus)
	for index, sku := range skus {
		if len(sku.Properties) != 1 || sku.Properties[0].Name != "口味" {
			t.Fatalf("sku %d properties=%v, want only 口味", index, sku.Properties)
		}
		if len(sku.SourceProperties) != 2 {
			t.Fatalf("sku %d source properties were changed: %v", index, sku.SourceProperties)
		}
	}
}
