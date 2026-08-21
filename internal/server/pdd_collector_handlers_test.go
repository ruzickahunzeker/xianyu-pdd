package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
	"xianyu-go/internal/pddproduct"
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

func TestSetPDDNavigationHeadersDoesNotBindBrowserVersion(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://mobile.pinduoduo.com/goods.html?goods_id=123", nil)
	account := &db.PDDAccount{
		Cookie:    "api_uid=test; pdd_vds=fresh",
		UserAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/151.0.0.0 Safari/537.36",
	}
	setPDDNavigationHeaders(req, account)

	want := map[string]string{
		"Cookie":        account.Cookie,
		"User-Agent":    account.UserAgent,
		"Cache-Control": "no-cache",
	}
	for name, value := range want {
		if got := req.Header.Get(name); got != value {
			t.Errorf("%s=%q, want %q", name, got, value)
		}
	}
	for _, name := range []string{"Sec-CH-UA", "Sec-CH-UA-Mobile", "Sec-CH-UA-Platform"} {
		if got := req.Header.Get(name); got != "" {
			t.Fatalf("%s=%q, browser version hints must not be synthesized", name, got)
		}
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

func TestValidatePDDReviewMediaAllowsMultipleMediaAndRejectsUntrustedHosts(t *testing.T) {
	input := pddReviewMediaInput{SchemaVersion: 1, CollectionID: "0d2858ce-43ad-4d1e-b953-01ff9492983a", GoodsID: "977731220380", Media: []pddReviewMediaItemInput{
		{ReviewID: "767869329074058140", SKUID: "1932571528029", MediaKey: "image:abc", MediaType: "image", SourceType: "initial", RemoteURL: "https://review.pddpic.com/a.jpg", IsLivePhotoImage: true},
		{ReviewID: "767302508852573084", SKUID: "1932571528030", MediaKey: "video:def", MediaType: "video", SourceType: "additional", RemoteURL: "https://video2.pddpic.com/v.mp4"},
	}}
	if err := validatePDDReviewMedia(&input); err != nil {
		t.Fatal(err)
	}
	input.Media[1].RemoteURL = "https://example.com/v.mp4"
	if err := validatePDDReviewMedia(&input); err == nil {
		t.Fatal("expected an untrusted media host to be rejected")
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

func TestPDDProductURLPreservesCapturedContext(t *testing.T) {
	raw := "https://mobile.pinduoduo.com/goods.html?goods_id=936179834486&uin=user-token&page_from=31&_oak_rcto=fresh&refer_page_id=dynamic"
	if got := pddProductURL(raw, "936179834486"); got != raw {
		t.Fatalf("captured URL=%q", got)
	}
	if got := pddProductURL("chrome-extension://id/background.js", "936179834486"); got != "https://mobile.pinduoduo.com/goods.html?goods_id=936179834486" {
		t.Fatalf("invalid fallback URL=%q", got)
	}
}

func TestValidatePDDProductSnapshotRejectsDegradedZeroPricePage(t *testing.T) {
	err := validatePDDProductSnapshot(pddproduct.Snapshot{GoodsID: "936179834486", SKUs: []pddproduct.SKU{{SKUID: "1883590772174", GoodsID: "936179834486", Stock: 1000, IsOnsale: true}}})
	if err == nil || !strings.Contains(err.Error(), "价格为 0") {
		t.Fatalf("err=%v", err)
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
	materialSKUs := `[{"material_sku_id":"material-1","source_sku_id":"1928995096086","price_cent":39900,"quantity":3,"enabled":true,"properties":[{"name":"自定义规格","value":"保留文字"}]},{"material_sku_id":"manual","price_cent":100,"quantity":9,"enabled":true,"properties":[{"name":"规格","value":"手工"}]}]`
	_, err = store.DB.Exec(`INSERT INTO product_materials(user_id,source_type,source_id,title,description,images_json,category_json,skus_json,status,created_at,updated_at) VALUES(1,'pdd','972484695683','库存同步素材','','[]','{}',?,'draft',1,1)`, materialSKUs)
	if err != nil {
		t.Fatal(err)
	}
	bundleSKUs := `[{"material_sku_id":"bundle-1","source_goods_id":"972484695683","source_sku_id":"1928995096086","price_cent":49900,"quantity":4,"enabled":true,"properties":[{"name":"组合规格","value":"商品B"}]}]`
	_, err = store.DB.Exec(`INSERT INTO product_materials(user_id,source_type,source_id,title,description,images_json,category_json,skus_json,status,created_at,updated_at) VALUES(1,'pdd','111111111111','组合素材','','[]','{}',?,'draft',1,1)`, bundleSKUs)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"schema_version":1,"collection_id":"0d2858ce-43ad-4d1e-b953-01ff9492983a","collection_method":"page_raw_data","collected_at":"2026-08-13T12:00:00Z","final_url":"https://mobile.pinduoduo.com/goods.html?goods_id=972484695683&uin=test-uin&page_from=31&_oak_rcto=fresh","goods":{"goods_id":"972484695683","mall_sn":"CgI2W-test-token","title":"测试商品","images":["https://img.example/a.jpg"]},"skus":[{"sku_id":"1928995096086","goods_id":"972484695683","thumb_url":"https://img.example/sku.jpg","stock":0,"stock_exact":true,"is_onsale":true,"prices":{"old_group_price":28500},"spec_value_ids":["30553766053"],"specs":[{"spec_key":"数量","spec_key_id":"1216","spec_value_id":"30553766053","raw_value":"整箱10罐【限时特价】"}]}]}`
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
	var materialRaw string
	if err := store.DB.QueryRow(`SELECT skus_json FROM product_materials WHERE source_id='972484695683'`).Scan(&materialRaw); err != nil {
		t.Fatal(err)
	}
	var materialRows []materialSKU
	if err := json.Unmarshal([]byte(materialRaw), &materialRows); err != nil {
		t.Fatal(err)
	}
	if materialRows[0].Quantity != 0 || materialRows[0].PriceCents != 39900 || materialRows[0].Properties[0].Value != "保留文字" || materialRows[1].Quantity != 9 {
		t.Fatalf("collector stock sync changed unexpected fields: %+v", materialRows)
	}
	if err := store.DB.QueryRow(`SELECT skus_json FROM product_materials WHERE source_id='111111111111'`).Scan(&materialRaw); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(materialRaw), &materialRows); err != nil {
		t.Fatal(err)
	}
	if materialRows[0].Quantity != 0 || materialRows[0].PriceCents != 49900 || materialRows[0].Properties[0].Value != "商品B" {
		t.Fatalf("bundle stock sync=%+v", materialRows)
	}
	var firstResponse map[string]any
	_ = json.Unmarshal(first.Body.Bytes(), &firstResponse)
	if firstResponse["material_stock_updates"] != float64(2) {
		t.Fatalf("response=%v", firstResponse)
	}
	var skuID, raw string
	var price, stock int64
	if err := store.DB.QueryRow(`SELECT sku_id,raw_snapshot_json,price_cent,stock FROM pdd_skus WHERE goods_id='972484695683'`).Scan(&skuID, &raw, &price, &stock); err != nil {
		t.Fatal(err)
	}
	if skuID != "1928995096086" || price != 28500 || stock != 0 || !strings.Contains(raw, "整箱10罐") {
		t.Fatalf("sku=%s price=%d stock=%d raw=%s", skuID, price, stock, raw)
	}
	var mallSN string
	if err := store.DB.QueryRow(`SELECT mall_sn FROM pdd_products WHERE goods_id='972484695683'`).Scan(&mallSN); err != nil {
		t.Fatal(err)
	}
	if mallSN != "CgI2W-test-token" {
		t.Fatalf("mall_sn=%q", mallSN)
	}
	var finalURL string
	if err := store.DB.QueryRow(`SELECT final_url FROM pdd_products WHERE goods_id='972484695683'`).Scan(&finalURL); err != nil {
		t.Fatal(err)
	}
	if finalURL != "https://mobile.pinduoduo.com/goods.html?goods_id=972484695683&uin=test-uin&page_from=31&_oak_rcto=fresh" {
		t.Fatalf("default final_url=%q", finalURL)
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

func TestPDDRefreshProductKeepsMissingSKUAndOnlySyncsMaterialStock(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "pdd-refresh-handler-test-key")
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	admin, err := store.Users.GetByUsername(context.Background(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.PDDAccounts.SaveSingle(context.Background(), admin.ID, "主账号", "pinduoduo", "api_uid=1; token=x", "1", "60984097534", "test-agent", true); err != nil {
		t.Fatal(err)
	}
	_, err = store.DB.Exec(`INSERT INTO pdd_products(id,goods_id,mall_sn,final_url,title,images_json,first_collected_at,last_collected_at) VALUES(1,'123','','https://pdd/123','旧标题','[]',10,20)`)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		skuID string
		price int64
		stock int64
	}{
		{"456", 1000, 5},
		{"999", 3000, 7},
	} {
		_, err = store.DB.Exec(`INSERT INTO pdd_skus(product_id,goods_id,sku_id,specs_json,spec_value_ids_json,thumb_url,prices_json,price_cent,stock,is_onsale,raw_snapshot_json,last_collected_at) VALUES(1,'123',?,'[]','[]','','{}',?,?,1,'{}',20)`, row.skuID, row.price, row.stock)
		if err != nil {
			t.Fatal(err)
		}
	}
	materialSKUs := `[{"material_sku_id":"material-1","source_sku_id":"456","price_cent":8800,"quantity":2,"enabled":true,"properties":[{"name":"自定义规格","value":"人工名称"}]}]`
	_, err = store.DB.Exec(`INSERT INTO product_materials(user_id,source_type,source_id,title,description,images_json,category_json,skus_json,status,created_at,updated_at) VALUES(1,'pdd','123','素材','','[]','{}',?,'draft',1,1)`, materialSKUs)
	if err != nil {
		t.Fatal(err)
	}
	srv.PDDProductFetch = func(_ context.Context, account *db.PDDAccount, goodsID string) (pddproduct.Snapshot, error) {
		if account.Cookie == "" || goodsID != "123" {
			t.Fatalf("fetch account=%+v goodsID=%s", account, goodsID)
		}
		return pddproduct.Snapshot{GoodsID: "123", Title: "新标题", SKUs: []pddproduct.SKU{
			{SKUID: "456", GoodsID: "123", PriceCent: 1200, Stock: 11, StockExact: true, IsOnsale: true, Specs: []pddproduct.Spec{{SpecKey: "颜色", SpecValueID: "v1", RawValue: "黑色"}}},
			{SKUID: "789", GoodsID: "123", PriceCent: 2200, Stock: 9, StockExact: true, IsOnsale: true, Specs: []pddproduct.Spec{{SpecKey: "颜色", SpecValueID: "v2", RawValue: "白色"}}},
		}}, nil
	}
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("goodsID", "123")
	req := httptest.NewRequest(http.MethodPost, "/api/pdd-collector/catalog/123/refresh", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeContext))
	req = req.WithContext(auth.WithSession(req.Context(), &db.Session{UserID: admin.ID, Username: "admin", IsAdmin: true}))
	rec := httptest.NewRecorder()
	srv.pddRefreshProduct(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var result struct {
		Added                int      `json:"added"`
		PriceChanged         int      `json:"price_changed"`
		StockChanged         int      `json:"stock_changed"`
		MissingSuspected     []string `json:"missing_suspected"`
		MaterialStockUpdates int      `json:"material_stock_updates"`
	}
	if err = json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Added != 1 || result.PriceChanged != 1 || result.StockChanged != 1 || result.MaterialStockUpdates != 1 || len(result.MissingSuspected) != 1 || result.MissingSuspected[0] != "999" {
		t.Fatalf("result=%+v", result)
	}
	var missingStock, updatedPrice, updatedStock int64
	if err = store.DB.QueryRow(`SELECT stock FROM pdd_skus WHERE goods_id='123' AND sku_id='999'`).Scan(&missingStock); err != nil {
		t.Fatal(err)
	}
	if err = store.DB.QueryRow(`SELECT price_cent,stock FROM pdd_skus WHERE goods_id='123' AND sku_id='456'`).Scan(&updatedPrice, &updatedStock); err != nil {
		t.Fatal(err)
	}
	if missingStock != 7 || updatedPrice != 1200 || updatedStock != 11 {
		t.Fatalf("missingStock=%d updatedPrice=%d updatedStock=%d", missingStock, updatedPrice, updatedStock)
	}
	var materialRaw string
	if err = store.DB.QueryRow(`SELECT skus_json FROM product_materials WHERE source_id='123'`).Scan(&materialRaw); err != nil {
		t.Fatal(err)
	}
	var materialRows []materialSKU
	if err = json.Unmarshal([]byte(materialRaw), &materialRows); err != nil {
		t.Fatal(err)
	}
	if materialRows[0].Quantity != 11 || materialRows[0].PriceCents != 8800 || materialRows[0].Properties[0].Value != "人工名称" {
		t.Fatalf("material=%+v", materialRows[0])
	}
}
