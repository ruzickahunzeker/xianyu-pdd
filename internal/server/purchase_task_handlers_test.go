package server

import "testing"

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
