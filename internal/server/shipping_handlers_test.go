package server

import (
	"testing"
	"time"

	"xianyu-go/internal/db"
)

func TestExactCarrierCodeUsesOfficialHotList(t *testing.T) {
	for name, want := range map[string]string{"韵达快递": "YUNDA", "极兔速递": "HTKY", "邮政快递包裹": "POSTB", "中通快递": "ZTO"} {
		if got := exactCarrierCode(name); got != want {
			t.Fatalf("%s=%s want %s", name, got, want)
		}
	}
	for _, name := range []string{"韵达", "京东快递", "中通快运"} {
		if got := exactCarrierCode(name); got != "" {
			t.Fatalf("ambiguous %s must not auto map: %s", name, got)
		}
	}
}

func TestPhysicalShipmentSchedulesPhoneRestoreOneHourAfterXianyuShipment(t *testing.T) {
	server, store, cleanup := newTestServer(t)
	defer cleanup()
	admin, err := store.Users.GetByUsername(t.Context(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Settings.Set(t.Context(), "pdd_phone_change_enabled", "true"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB.Exec(`INSERT INTO cookies(id,value,user_id) VALUES('account-1','cookie',?)`, admin.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB.Exec(`INSERT INTO orders(order_id,item_id,cookie_id,order_status) VALUES('restore-delay-order','item-1','account-1','pending_ship')`); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB.Exec(`INSERT INTO order_fulfillments(order_id,user_id,cookie_id,item_id,created_at,updated_at) VALUES('restore-delay-order',?,'account-1','item-1',1,1)`, admin.ID); err != nil {
		t.Fatal(err)
	}
	finished := int64(1_700_000_000)
	server.markPhysicalShipmentSuccess(t.Context(), &db.Order{OrderID: "restore-delay-order", CookieID: "account-1", ItemID: "item-1"}, admin.ID, finished)
	var shippedAt, restoreDue int64
	var shipped, reminderExempt int
	if err = store.DB.QueryRow(`SELECT xianyu_shipped,xianyu_shipped_at,phone_restore_due_at,reminder_exempt FROM order_fulfillments WHERE order_id='restore-delay-order'`).Scan(&shipped, &shippedAt, &restoreDue, &reminderExempt); err != nil {
		t.Fatal(err)
	}
	if shipped != 1 || shippedAt != finished || restoreDue != finished+int64(time.Hour/time.Second) || reminderExempt != 0 {
		t.Fatalf("shipped=%d shipped_at=%d restore_due=%d reminder_exempt=%d", shipped, shippedAt, restoreDue, reminderExempt)
	}
}
