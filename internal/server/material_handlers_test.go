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
	material, err := scanMaterial(store.DB.QueryRow(`SELECT id,user_id,source_type,source_id,title,description,images_json,category_json,skus_json,postage_mode,postage_cent,status,created_at,updated_at,image_property_name,video_enabled,videos_json FROM product_materials WHERE id=?`, materialID))
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
