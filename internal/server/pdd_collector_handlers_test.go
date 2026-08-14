package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestPDDCollectorRejectsMissingToken(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	req := httptest.NewRequest(http.MethodPost, "/api/pdd-collector/products", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPDDPriceCentPrefersGroupPrice(t *testing.T) {
	prices := map[string]any{
		"group_price":     "26.9",
		"sku_price":       float64(2690),
		"normal_price":    "39.9",
		"old_group_price": float64(3750),
	}
	if got := pddPriceCent(prices); got != 2690 {
		t.Fatalf("pddPriceCent()=%d, want 2690", got)
	}
}

func TestValidatePDDCollectionFiltersBrandAndShippingOrigin(t *testing.T) {
	var input pddCollectionInput
	if err := json.Unmarshal([]byte(`{"schema_version":1,"collection_id":"0d2858ce-43ad-4d1e-b953-01ff9492983a","goods":{"goods_id":"123","goods_property":[{"key":"品牌","values":["测试品牌"]},{"key":"发货地","values":["辽宁省"]},{"key":"材质","values":[" 合金钢 "]}]},"skus":[{"sku_id":"456","goods_id":"123","stock":1}]}`), &input); err != nil {
		t.Fatal(err)
	}
	if err := validatePDDCollection(&input); err != nil {
		t.Fatal(err)
	}
	if len(input.Goods.GoodsProperty) != 1 || input.Goods.GoodsProperty[0].Key != "材质" || input.Goods.GoodsProperty[0].Values[0] != "合金钢" {
		t.Fatalf("goods properties=%+v", input.Goods.GoodsProperty)
	}
}

func TestPDDPriceCentFallbackOrder(t *testing.T) {
	tests := []struct {
		name   string
		prices map[string]any
		want   int64
	}{
		{"sku price", map[string]any{"sku_price": float64(1680), "normal_price": "28.8", "old_group_price": float64(2680)}, 1680},
		{"normal price", map[string]any{"normal_price": "28.8", "old_group_price": float64(2680)}, 2880},
		{"old group price", map[string]any{"old_group_price": float64(2680)}, 2680},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pddPriceCent(tt.prices); got != tt.want {
				t.Fatalf("pddPriceCent()=%d, want %d", got, tt.want)
			}
		})
	}
}

func TestPDDCollectorUploadAndIdempotency(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	token := "pddc_test_device_token"
	_, err := store.DB.Exec(`INSERT INTO pdd_collector_devices(id,name,token_hash,enabled,last_seen_at,last_collected_at,created_at) VALUES('dev1','测试设备',?,1,0,0,1)`, tokenDigest(token))
	if err != nil {
		t.Fatal(err)
	}
	body := `{"schema_version":1,"collection_id":"0d2858ce-43ad-4d1e-b953-01ff9492983a","collection_method":"page_raw_data","collected_at":"2026-08-13T12:00:00Z","final_url":"https://mobile.yangkeduo.com/goods.html?goods_id=972484695683","goods":{"goods_id":"972484695683","mall_sn":"CgI2W-test-token","title":"测试商品","images":["https://img.example/a.jpg"]},"skus":[{"sku_id":"1928995096086","goods_id":"972484695683","thumb_url":"https://img.example/sku.jpg","stock":1000,"is_onsale":true,"prices":{"old_group_price":28500},"spec_value_ids":["30553766053"],"specs":[{"spec_key":"数量","spec_key_id":"1216","spec_value_id":"30553766053","raw_value":"整箱10罐【限时特价】"}]}]}`
	upload := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/pdd-collector/products", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		srv.Router().ServeHTTP(rec, req)
		return rec
	}
	first := upload()
	if first.Code != http.StatusCreated {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	var skuID, raw string
	var price, stock int64
	if err := store.DB.QueryRow(`SELECT sku_id,raw_snapshot_json,price_cent,stock FROM pdd_skus WHERE goods_id='972484695683'`).Scan(&skuID, &raw, &price, &stock); err != nil {
		t.Fatal(err)
	}
	if skuID != "1928995096086" || price != 28500 || stock != 1000 || !strings.Contains(raw, "整箱10罐") {
		t.Fatalf("sku=%s price=%d stock=%d raw=%s", skuID, price, stock, raw)
	}
	var mallSN string
	if err := store.DB.QueryRow(`SELECT mall_sn FROM pdd_products WHERE goods_id='972484695683'`).Scan(&mallSN); err != nil {
		t.Fatal(err)
	}
	if mallSN != "CgI2W-test-token" {
		t.Fatalf("mall_sn=%q", mallSN)
	}
	second := upload()
	if second.Code != http.StatusOK {
		t.Fatalf("second status=%d body=%s", second.Code, second.Body.String())
	}
	var response map[string]any
	_ = json.Unmarshal(second.Body.Bytes(), &response)
	if response["status"] != "already_collected" {
		t.Fatalf("response=%v", response)
	}
	var count int
	if err := store.DB.QueryRow(`SELECT COUNT(*) FROM pdd_sku_snapshots`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("snapshot count=%d err=%v", count, err)
	}
}

func TestPDDCollectorCatalog(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	_, err := store.DB.Exec(`INSERT INTO pdd_products(id,goods_id,mall_sn,final_url,title,images_json,first_collected_at,last_collected_at) VALUES(1,'123','mall-token','https://pdd/123','测试商品','["https://img/a.jpg"]',10,20)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.DB.Exec(`INSERT INTO pdd_skus(product_id,goods_id,sku_id,specs_json,spec_value_ids_json,thumb_url,prices_json,price_cent,stock,is_onsale,raw_snapshot_json,last_collected_at) VALUES(1,'123','456','[{"spec_key":"规格","raw_value":"整箱10罐【限时】"}]','["789"]','https://img/sku.jpg','{}',2990,8,1,'{}',20)`)
	if err != nil {
		t.Fatal(err)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/pdd-collector/catalog", nil)
	listRec := httptest.NewRecorder()
	srv.pddListProducts(listRec, listReq)
	if listRec.Code != http.StatusOK || !strings.Contains(listRec.Body.String(), `"sku_count":1`) || !strings.Contains(listRec.Body.String(), `"mall_sn":"mall-token"`) {
		t.Fatalf("list status=%d body=%s", listRec.Code, listRec.Body.String())
	}

	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("goodsID", "123")
	detailReq := httptest.NewRequest(http.MethodGet, "/api/pdd-collector/catalog/123", nil)
	detailReq = detailReq.WithContext(context.WithValue(detailReq.Context(), chi.RouteCtxKey, routeContext))
	detailRec := httptest.NewRecorder()
	srv.pddGetProduct(detailRec, detailReq)
	if detailRec.Code != http.StatusOK || !strings.Contains(detailRec.Body.String(), "整箱10罐") || !strings.Contains(detailRec.Body.String(), `"mall_sn":"mall-token"`) {
		t.Fatalf("detail status=%d body=%s", detailRec.Code, detailRec.Body.String())
	}
}
