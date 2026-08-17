package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOrderSyncInterval(t *testing.T) {
	cases := map[string]int{
		"":     defaultOrderSyncInterval,
		"bad":  defaultOrderSyncInterval,
		"4":    defaultOrderSyncInterval,
		"5":    5,
		"60":   60,
		"2000": maxOrderSyncInterval,
	}
	for raw, want := range cases {
		if got := orderSyncInterval(raw); got != want {
			t.Fatalf("orderSyncInterval(%q)=%d want %d", raw, got, want)
		}
	}
}

func TestOrderSyncStatusAndNonReentrantManualSync(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	statusReq := httptest.NewRequest(http.MethodGet, "/api/orders/sync-status", nil)
	statusReq.AddCookie(cookie)
	statusRec := httptest.NewRecorder()
	h.ServeHTTP(statusRec, statusReq)
	if statusRec.Code != http.StatusOK || !strings.Contains(statusRec.Body.String(), `"interval_minutes":10`) {
		t.Fatalf("status=%d body=%s", statusRec.Code, statusRec.Body.String())
	}

	srv.orderSyncMu.Lock()
	srv.orderSyncRunning[1] = true
	srv.orderSyncMu.Unlock()
	defer func() {
		srv.orderSyncMu.Lock()
		delete(srv.orderSyncRunning, 1)
		srv.orderSyncMu.Unlock()
	}()
	req := httptest.NewRequest(http.MethodPost, "/api/orders/refresh", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("concurrent sync status=%d body=%s", rec.Code, rec.Body.String())
	}
}
