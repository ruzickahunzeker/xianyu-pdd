package server

import (
	"context"
	"net/http/httptest"
	"testing"
)

func TestCanSavePurchaseBaseline(t *testing.T) {
	for _, status := range []string{"claimed", "validating", "goods_validated", "browser_preparing"} {
		if !canSavePurchaseBaseline(status) {
			t.Fatalf("status %q should allow saving the baseline", status)
		}
	}
	for _, status := range []string{"loading_goods", "validating_goods", "submitting_unpaid_order", "completed", "failed", "aborted"} {
		if canSavePurchaseBaseline(status) {
			t.Fatalf("status %q must not allow saving the baseline", status)
		}
	}
}

func TestFulfillmentExceptionDeduplicatesOpenEvent(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	admin, _ := store.Users.GetByUsername(context.Background(), "admin")
	if _, err := store.DB.Exec(`INSERT INTO orders(order_id,item_id,cookie_id,order_status) VALUES('exception-order','item','acc1','pending_ship')`); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/", nil)
	for range 2 {
		srv.createFulfillmentException(req, admin.ID, "exception-order", "task-1", "purchase_failed", "同一异常", map[string]any{"reason": "test"})
	}
	var count int
	var notificationStatus string
	if err := store.DB.QueryRow(`SELECT COUNT(*),MAX(notification_status) FROM fulfillment_exception_events WHERE order_id='exception-order'`).Scan(&count, &notificationStatus); err != nil {
		t.Fatal(err)
	}
	if count != 1 || notificationStatus != "not_configured" {
		t.Fatalf("count=%d notification_status=%q", count, notificationStatus)
	}
}

func TestRecoverExpiredPurchaseLeasesOnlyReconcilesPostSubmit(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	admin, _ := store.Users.GetByUsername(context.Background(), "admin")
	now := int64(100)
	for _, row := range []struct {
		id, status string
		lease      int64
	}{
		{"submit-expired", "submitting_unpaid_order", 99},
		{"reconcile-expired", "reconciling_result", 99},
		{"submit-active", "submitting_unpaid_order", 101},
		{"pre-submit", "browser_preparing", 99},
	} {
		if _, err := store.DB.Exec(`INSERT INTO pdd_purchase_tasks(id,user_id,order_id,attempt,status,worker_id,lease_token,lease_expires_at,created_at,updated_at) VALUES(?,?,?,?,?,'worker','token',?,?,?)`, row.id, admin.ID, "order-"+row.id, 1, row.status, row.lease, now-10, now-10); err != nil {
			t.Fatal(err)
		}
	}
	if err := srv.recoverExpiredPurchaseLeases(context.Background(), admin.ID, now); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"submit-expired": "result_unknown", "reconcile-expired": "result_unknown", "submit-active": "submitting_unpaid_order", "pre-submit": "browser_preparing"}
	for id, expected := range want {
		var status, token string
		var lease int64
		if err := store.DB.QueryRow(`SELECT status,lease_token,lease_expires_at FROM pdd_purchase_tasks WHERE id=?`, id).Scan(&status, &token, &lease); err != nil {
			t.Fatal(err)
		}
		if status != expected {
			t.Fatalf("%s status=%q want=%q", id, status, expected)
		}
		if expected == "result_unknown" && (token != "" || lease != 0) {
			t.Fatalf("%s recovery lease not cleared: token=%q lease=%d", id, token, lease)
		}
	}
}
