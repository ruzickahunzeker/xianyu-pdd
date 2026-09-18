package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func seedLogisticsSyncOrder(t *testing.T, srv *Server, orderID, pddOrderID string) {
	t.Helper()
	admin, err := srv.Store.Users.GetByUsername(t.Context(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = srv.Store.DB.Exec(`INSERT INTO cookies(id,value,user_id) VALUES(?,?,?)`, "cookie-"+orderID, "unb=1", admin.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = srv.Store.DB.Exec(`INSERT INTO orders(order_id,item_id,cookie_id,order_status) VALUES(?,?,?,'pending_ship')`, orderID, "item-"+orderID, "cookie-"+orderID); err != nil {
		t.Fatal(err)
	}
	if _, err = srv.Store.DB.Exec(`INSERT INTO order_fulfillments(order_id,user_id,cookie_id,item_id,pdd_ordered,pdd_order_id,pdd_shipped,xianyu_shipped,fulfillment_exempt,created_at,updated_at) VALUES(?,?,?,?,1,?,0,0,0,1,1)`, orderID, admin.ID, "cookie-"+orderID, "item-"+orderID, pddOrderID); err != nil {
		t.Fatal(err)
	}
}

func saveLogisticsSyncPDDAccount(t *testing.T, srv *Server) {
	t.Helper()
	admin, err := srv.Store.Users.GetByUsername(t.Context(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = srv.Store.PDDAccounts.SaveSingle(t.Context(), admin.ID, "主账号", "pinduoduo", "pdd_user_id=1", "1", "609", "test-agent", true); err != nil {
		t.Fatal(err)
	}
}

func logisticsRequest(t *testing.T, router http.Handler, session *http.Cookie, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(session)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestQueueLogisticsSyncIsIdempotent(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "logistics-queue-test-key")
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	saveLogisticsSyncPDDAccount(t, srv)
	seedLogisticsSyncOrder(t, srv, "xy-single", "pdd-single")
	router, session := srv.Router(), loginHelper(t, srv.Router())
	first := logisticsRequest(t, router, session, http.MethodPost, "/api/fulfillment/orders/xy-single/logistics-sync", `{}`)
	if first.Code != http.StatusCreated || !strings.Contains(first.Body.String(), `"status":"queued"`) {
		t.Fatalf("first=%d %s", first.Code, first.Body.String())
	}
	second := logisticsRequest(t, router, session, http.MethodPost, "/api/fulfillment/orders/xy-single/logistics-sync", `{}`)
	if second.Code != http.StatusOK || !strings.Contains(second.Body.String(), `"status":"already_queued"`) {
		t.Fatalf("second=%d %s", second.Code, second.Body.String())
	}
	var count int
	if err := srv.Store.DB.QueryRow(`SELECT COUNT(*) FROM pdd_logistics_sync_tasks WHERE order_id='xy-single'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
}

func TestQueueLogisticsSyncBatchUsesTenSecondSpacing(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "logistics-batch-test-key")
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	saveLogisticsSyncPDDAccount(t, srv)
	seedLogisticsSyncOrder(t, srv, "xy-batch-1", "pdd-batch-1")
	seedLogisticsSyncOrder(t, srv, "xy-batch-2", "pdd-batch-2")
	router := srv.Router()
	rec := logisticsRequest(t, router, loginHelper(t, router), http.MethodPost, "/api/fulfillment/logistics-sync/batch", `{}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"queued":2`) || !strings.Contains(rec.Body.String(), `"interval_seconds":10`) {
		t.Fatalf("batch=%d %s", rec.Code, rec.Body.String())
	}
	rows, err := srv.Store.DB.Query(`SELECT scheduled_at FROM pdd_logistics_sync_tasks ORDER BY scheduled_at`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	times := []int64{}
	for rows.Next() {
		var value int64
		if rows.Scan(&value) == nil {
			times = append(times, value)
		}
	}
	if len(times) != 2 || times[1]-times[0] != manualLogisticsIntervalSecond {
		t.Fatalf("scheduled times=%v", times)
	}
}

func TestLogisticsSyncTaskLeaseSerializesAndCompletes(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "logistics-lease-test-key")
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	saveLogisticsSyncPDDAccount(t, srv)
	seedLogisticsSyncOrder(t, srv, "xy-lease", "pdd-lease")
	router, session := srv.Router(), loginHelper(t, srv.Router())
	queued := logisticsRequest(t, router, session, http.MethodPost, "/api/fulfillment/orders/xy-lease/logistics-sync", `{}`)
	if queued.Code != http.StatusCreated {
		t.Fatalf("queue=%d %s", queued.Code, queued.Body.String())
	}
	claimed := logisticsRequest(t, router, session, http.MethodPost, "/api/fulfillment/logistics-sync-tasks/claim", `{"worker_id":"test-worker","lease_seconds":120}`)
	if claimed.Code != http.StatusOK {
		t.Fatalf("claim=%d %s", claimed.Code, claimed.Body.String())
	}
	var claim map[string]any
	if err := json.Unmarshal(claimed.Body.Bytes(), &claim); err != nil {
		t.Fatal(err)
	}
	secondClaim := logisticsRequest(t, router, session, http.MethodPost, "/api/fulfillment/logistics-sync-tasks/claim", `{"worker_id":"other"}`)
	if secondClaim.Code != http.StatusNotFound {
		t.Fatalf("second claim=%d %s", secondClaim.Code, secondClaim.Body.String())
	}
	path := "/api/fulfillment/logistics-sync-tasks/" + claim["id"].(string) + "/result"
	body, _ := json.Marshal(map[string]any{"lease_token": claim["lease_token"], "status": "not_shipped", "result": map[string]any{"pdd_order_id": "pdd-lease"}})
	completed := logisticsRequest(t, router, session, http.MethodPost, path, string(body))
	if completed.Code != http.StatusOK {
		t.Fatalf("complete=%d %s", completed.Code, completed.Body.String())
	}
	var status string
	var active any
	if err := srv.Store.DB.QueryRow(`SELECT status,active_marker FROM pdd_logistics_sync_tasks WHERE id=?`, claim["id"]).Scan(&status, &active); err != nil {
		t.Fatal(err)
	}
	if status != "not_shipped" || active != nil {
		t.Fatalf("status=%q active=%v", status, active)
	}
	requeued := logisticsRequest(t, router, session, http.MethodPost, "/api/fulfillment/orders/xy-lease/logistics-sync", `{}`)
	if requeued.Code != http.StatusCreated {
		t.Fatalf("requeue=%d %s", requeued.Code, requeued.Body.String())
	}
}
