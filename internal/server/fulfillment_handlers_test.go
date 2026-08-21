package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xianyu-go/internal/pddaddress"
)

type fakePDDAddressUpdater struct {
	calls  int
	result pddaddress.UpdateResult
	err    error
}

func (f *fakePDDAddressUpdater) Update(_ context.Context, _ pddaddress.UpdateRequest) (pddaddress.UpdateResult, error) {
	f.calls++
	return f.result, f.err
}

func TestApplyPDDAddressIsIdempotentAndStatusGuarded(t *testing.T) {
	server, store, cleanup := newTestServer(t)
	defer cleanup()
	admin, err := store.Users.GetByUsername(t.Context(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	updater := &fakePDDAddressUpdater{result: pddaddress.UpdateResult{Status: "applied", HTTPStatus: 200, ResponseBody: `{"success":true}`}}
	server.PDDAddressUpdater = updater
	if err := store.Settings.Set(t.Context(), "pdd_phone_change_enabled", "true"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB.Exec(`INSERT INTO orders(order_id,item_id,cookie_id,order_status,receiver_name,receiver_phone,receiver_address,receiver_city) VALUES('address-order','item-a','acc1','pending_ship','张三','13216514040','北京市海淀区中关村大街1号','北京市')`); err != nil {
		t.Fatal(err)
	}
	request := func(key string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/fulfillment/orders/address-order/pdd-address/apply", nil)
		req.AddCookie(loginHelper(t, server.Router()))
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		recorder := httptest.NewRecorder()
		server.Router().ServeHTTP(recorder, req)
		return recorder
	}
	if response := request(""); response.Code != http.StatusBadRequest {
		t.Fatalf("missing key: %d %s", response.Code, response.Body.String())
	}
	first := request("order-address-1")
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), `"status":"applied"`) {
		t.Fatalf("first: %d %s", first.Code, first.Body.String())
	}
	second := request("order-address-1")
	if second.Code != http.StatusOK || !strings.Contains(second.Body.String(), `"replayed":true`) || updater.calls != 1 {
		t.Fatalf("replay: %d calls=%d %s", second.Code, updater.calls, second.Body.String())
	}
	var status, phone string
	if err := store.DB.QueryRow(`SELECT address_match_status,temporary_phone FROM order_fulfillments WHERE order_id='address-order' AND user_id=?`, admin.ID).Scan(&status, &phone); err != nil || status != "applied" || phone != "13217514040" {
		t.Fatalf("status=%s phone=%s err=%v", status, phone, err)
	}
	if _, err := store.DB.Exec(`UPDATE orders SET order_status='refunding' WHERE order_id='address-order'`); err != nil {
		t.Fatal(err)
	}
	if response := request("order-address-2"); response.Code != http.StatusConflict || updater.calls != 1 {
		t.Fatalf("status guard: %d calls=%d", response.Code, updater.calls)
	}
}

func TestFulfillmentPhoneUsesOriginalUnlessSwitchEnabled(t *testing.T) {
	server, store, cleanup := newTestServer(t)
	defer cleanup()
	mobile, temporary, err := server.fulfillmentPhone(t.Context(), "13216514040")
	if err != nil || mobile != "13216514040" || temporary != "" {
		t.Fatalf("default mobile=%q temporary=%q err=%v", mobile, temporary, err)
	}
	if err := store.Settings.Set(t.Context(), "pdd_phone_change_enabled", "true"); err != nil {
		t.Fatal(err)
	}
	mobile, temporary, err = server.fulfillmentPhone(t.Context(), "13216514040")
	if err != nil || mobile != "13217514040" || temporary != mobile {
		t.Fatalf("enabled mobile=%q temporary=%q err=%v", mobile, temporary, err)
	}
}

func TestFulfillmentRoutesRequireDedicatedAPIKey(t *testing.T) {
	server, store, cleanup := newTestServer(t)
	defer cleanup()
	admin, err := store.Users.GetByUsername(t.Context(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	token := "xyf_test_fulfillment_key"
	if _, err := store.DB.Exec(`INSERT INTO fulfillment_api_keys(id,user_id,name,token_hash,enabled,last_used_at,created_at) VALUES('key1',?,'测试脚本',?,1,0,1)`, admin.ID, tokenDigest(token)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB.Exec(`INSERT INTO cookies(id,value,user_id) VALUES('fulfillment-account','unb=456; _m_h5_tk=tk2_1;',?)`, admin.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB.Exec(`INSERT INTO orders(order_id,item_id,cookie_id,order_status) VALUES('fulfillment-order','item-1','fulfillment-account','pending_ship')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB.Exec(`INSERT INTO orders(order_id,item_id,cookie_id,order_status) VALUES('cancelled-order','item-2','fulfillment-account','cancelled')`); err != nil {
		t.Fatal(err)
	}
	var active int
	if err := store.DB.QueryRow(`SELECT COUNT(*) FROM orders o JOIN cookies c ON c.id=o.cookie_id WHERE c.user_id=? AND o.deleted_at IS NULL AND o.order_status<>'cancelled'`, admin.ID).Scan(&active); err != nil || active != 1 {
		t.Fatalf("active owned orders=%d err=%v", active, err)
	}

	request := func(value string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/fulfillment/orders?pdd_ordered=false", nil)
		if value != "" {
			req.Header.Set("Authorization", "Bearer "+value)
		}
		recorder := httptest.NewRecorder()
		server.Router().ServeHTTP(recorder, req)
		return recorder
	}
	if response := request(""); response.Code != http.StatusUnauthorized {
		t.Fatalf("without key status=%d body=%s", response.Code, response.Body.String())
	}
	if response := request("xyf_wrong"); response.Code != http.StatusUnauthorized {
		t.Fatalf("wrong key status=%d body=%s", response.Code, response.Body.String())
	}
	if response := request(token); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"order_id":"fulfillment-order"`) || !strings.Contains(response.Body.String(), `"pdd_ordered":false`) || strings.Contains(response.Body.String(), `"order_id":"cancelled-order"`) {
		t.Fatalf("valid key should reconcile active orders and exclude cancelled orders: status=%d body=%s", response.Code, response.Body.String())
	}
	if _, err := store.DB.Exec(`INSERT INTO pdd_purchase_tasks(id,user_id,order_id,attempt,status,pdd_order_id,pdd_order_json,created_at,updated_at) VALUES('purchase-info',?,'fulfillment-order',1,'completed','pdd-100','{"order_id":"pdd-100","sku_id":"sku-9","amount_cent":219,"quantity":1}',1,1)`, admin.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB.Exec(`UPDATE order_fulfillments SET pdd_order_id='pdd-100' WHERE order_id='fulfillment-order'`); err != nil {
		t.Fatal(err)
	}
	if response := request(token); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"pdd_order":{"order_id":"pdd-100","sku_id":"sku-9","amount_cent":219,"quantity":1}`) {
		t.Fatalf("fulfillment response should include captured PDD order detail: status=%d body=%s", response.Code, response.Body.String())
	}

	sessionRequest := httptest.NewRequest(http.MethodGet, "/api/fulfillment/orders?pdd_ordered=false", nil)
	sessionRequest.AddCookie(loginHelper(t, server.Router()))
	sessionResponse := httptest.NewRecorder()
	server.Router().ServeHTTP(sessionResponse, sessionRequest)
	if sessionResponse.Code != http.StatusOK || !strings.Contains(sessionResponse.Body.String(), `"order_id":"fulfillment-order"`) {
		t.Fatalf("admin session should use unified fulfillment API: status=%d body=%s", sessionResponse.Code, sessionResponse.Body.String())
	}
}

func TestShippingPrecheckNormalizesNumericStatusAndShipmentDoesNotRegress(t *testing.T) {
	server, store, cleanup := newTestServer(t)
	defer cleanup()
	admin, err := store.Users.GetByUsername(t.Context(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB.Exec(`INSERT INTO cookies(id,value,user_id) VALUES('shipping-account','unb=1; _m_h5_tk=t_1;',?)`, admin.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB.Exec(`INSERT INTO orders(order_id,item_id,cookie_id,order_status,system_shipped) VALUES('shipping-order','item-1','shipping-account','2',0)`); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB.Exec(`INSERT INTO order_fulfillments(order_id,user_id,cookie_id,item_id,pdd_order_id,pdd_shipped,logistics_company,logistics_company_code,tracking_number,xianyu_shipped,created_at,updated_at) VALUES('shipping-order',?,'shipping-account','item-1','pdd-1',1,'申通快递','STO','TRACK-1',0,1,1)`, admin.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB.Exec(`INSERT INTO xianyu_shipping_accounts(cookie_id,user_id,address_id,address_summary,verified_at,created_at,updated_at) VALUES('shipping-account',?,123,'test',1,1,1)`, admin.ID); err != nil {
		t.Fatal(err)
	}
	server.MTop = withMTopTransport(roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"ret":["SUCCESS::调用成功"],"data":{"addressId":"25152147714"}}`))}, nil
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/fulfillment/orders/shipping-order/shipping-precheck", strings.NewReader(`{}`))
	req.AddCookie(loginHelper(t, server.Router()))
	rec := httptest.NewRecorder()
	server.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ready":true`) {
		t.Fatalf("numeric pending-ship status should pass: %d %s", rec.Code, rec.Body.String())
	}

	if _, err = store.DB.Exec(`UPDATE order_fulfillments SET xianyu_shipped=1,xianyu_shipped_at=10 WHERE order_id='shipping-order'`); err != nil {
		t.Fatal(err)
	}
	order, err := store.Orders.Get(t.Context(), "shipping-order")
	if err != nil {
		t.Fatal(err)
	}
	if err = server.ensureFulfillment(httptest.NewRequest(http.MethodGet, "/", nil), *order, admin.ID); err != nil {
		t.Fatal(err)
	}
	var shipped int
	if err = store.DB.QueryRow(`SELECT xianyu_shipped FROM order_fulfillments WHERE order_id='shipping-order'`).Scan(&shipped); err != nil || shipped != 1 {
		t.Fatalf("successful shipment regressed: shipped=%d err=%v", shipped, err)
	}
}

func TestFulfillmentBoolFilter(t *testing.T) {
	tests := []struct {
		raw         string
		wantValue   int
		wantPresent bool
		wantError   bool
	}{
		{raw: "false", wantValue: 0, wantPresent: true},
		{raw: "FALSE", wantValue: 0, wantPresent: true},
		{raw: "0", wantValue: 0, wantPresent: true},
		{raw: "true", wantValue: 1, wantPresent: true},
		{raw: "1", wantValue: 1, wantPresent: true},
		{raw: "", wantPresent: false},
		{raw: "no", wantError: true},
	}
	for _, test := range tests {
		value, present, err := fulfillmentBoolFilter(test.raw)
		if value != test.wantValue || present != test.wantPresent || (err != nil) != test.wantError {
			t.Fatalf("raw=%q value=%d present=%v err=%v", test.raw, value, present, err)
		}
	}
}

func TestFulfillmentPropertiesMatch(t *testing.T) {
	properties := []materialProperty{{Name: "款式", Value: "USB 转 Type-C"}, {Name: "长度", Value: "1米"}}
	if !fulfillmentPropertiesMatch(properties, "款式", "USB 转 Type-C") {
		t.Fatal("expected exact published property to match")
	}
	if fulfillmentPropertiesMatch(properties, "款式", "Type-C 转 Type-C") {
		t.Fatal("must not bind a different SKU property")
	}
	if fulfillmentPropertiesMatch(properties, "", "") {
		t.Fatal("empty order specification must not guess a mapping")
	}
}

func TestFulfillmentHistoryRepairOnlyTouchesUntouchedTerminalOrders(t *testing.T) {
	server, store, cleanup := newTestServer(t)
	defer cleanup()
	admin, err := store.Users.GetByUsername(t.Context(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct{ id, status string }{
		{"history-clean", "completed"},
		{"history-pdd", "completed"},
		{"history-manual", "completed"},
		{"history-pending", "pending_ship"},
		{"history-refunding", "refunding"},
		{"history-shipped", "shipped"},
	} {
		if _, err := store.DB.Exec(`INSERT INTO orders(order_id,item_id,cookie_id,order_status) VALUES(?,?,'acc1',?)`, row.id, "item-"+row.id, row.status); err != nil {
			t.Fatal(err)
		}
	}
	now := int64(10)
	if _, err := store.DB.Exec(`INSERT INTO order_fulfillments(order_id,user_id,cookie_id,item_id,pdd_order_id,created_at,updated_at) VALUES('history-pdd',?,'acc1','item-history-pdd','pdd-1',?,?)`, admin.ID, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB.Exec(`INSERT INTO order_fulfillments(order_id,user_id,cookie_id,item_id,manual_modified_at,manual_modified_fields,created_at,updated_at) VALUES('history-manual',?,'acc1','item-history-manual',20,'reminded',?,?)`, admin.ID, now, now); err != nil {
		t.Fatal(err)
	}

	h := server.Router()
	cookie := loginHelper(t, h)
	previewReq := httptest.NewRequest(http.MethodGet, "/api/fulfillment/history-repair/preview", nil)
	previewReq.AddCookie(cookie)
	previewRec := httptest.NewRecorder()
	h.ServeHTTP(previewRec, previewReq)
	if previewRec.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", previewRec.Code, previewRec.Body.String())
	}
	var preview fulfillmentHistoryRepairPreview
	if err := json.Unmarshal(previewRec.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.Eligible != 1 || preview.ActiveExcluded != 3 || preview.ManualExcluded != 1 || preview.PDDExcluded != 1 {
		t.Fatalf("preview=%+v", preview)
	}

	repairReq := httptest.NewRequest(http.MethodPost, "/api/fulfillment/history-repair", strings.NewReader(`{}`))
	repairReq.AddCookie(cookie)
	repairRec := httptest.NewRecorder()
	h.ServeHTTP(repairRec, repairReq)
	if repairRec.Code != http.StatusOK || !strings.Contains(repairRec.Body.String(), `"updated":1`) {
		t.Fatalf("repair status=%d body=%s", repairRec.Code, repairRec.Body.String())
	}
	var exempt, reminderExempt int
	if err := store.DB.QueryRow(`SELECT fulfillment_exempt,reminder_exempt FROM order_fulfillments WHERE order_id='history-clean'`).Scan(&exempt, &reminderExempt); err != nil || exempt != 1 || reminderExempt != 1 {
		t.Fatalf("clean order flags=%d/%d err=%v", exempt, reminderExempt, err)
	}
	for _, id := range []string{"history-pdd", "history-manual", "history-pending", "history-refunding", "history-shipped"} {
		if err := store.DB.QueryRow(`SELECT fulfillment_exempt FROM order_fulfillments WHERE order_id=?`, id).Scan(&exempt); err != nil || exempt != 0 {
			t.Fatalf("order %s should remain untouched: exempt=%d err=%v", id, exempt, err)
		}
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/fulfillment/orders?pdd_ordered=false", nil)
	listReq.AddCookie(cookie)
	listRec := httptest.NewRecorder()
	h.ServeHTTP(listRec, listReq)
	if strings.Contains(listRec.Body.String(), `"order_id":"history-clean"`) {
		t.Fatalf("repaired order must not remain in pending list: %s", listRec.Body.String())
	}
}
