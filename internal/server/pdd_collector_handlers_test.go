package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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

func TestPDDCollectorUploadAndIdempotency(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	token := "pddc_test_device_token"
	_, err := store.DB.Exec(`INSERT INTO pdd_collector_devices(id,name,token_hash,enabled,last_seen_at,last_collected_at,created_at) VALUES('dev1','测试设备',?,1,0,0,1)`, tokenDigest(token))
	if err != nil {
		t.Fatal(err)
	}
	body := `{"schema_version":1,"collection_id":"0d2858ce-43ad-4d1e-b953-01ff9492983a","collection_method":"page_raw_data","collected_at":"2026-08-13T12:00:00Z","final_url":"https://mobile.yangkeduo.com/goods.html?goods_id=972484695683","goods":{"goods_id":"972484695683","title":"测试商品","images":["https://img.example/a.jpg"]},"skus":[{"sku_id":"1928995096086","goods_id":"972484695683","thumb_url":"https://img.example/sku.jpg","stock":1000,"is_onsale":true,"prices":{"old_group_price":28500},"spec_value_ids":["30553766053"],"specs":[{"spec_key":"数量","spec_key_id":"1216","spec_value_id":"30553766053","raw_value":"整箱10罐【限时特价】"}]}]}`
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
