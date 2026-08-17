package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCheckoutAmountCent(t *testing.T) {
	got, err := checkoutAmountCent("商品金额 ¥12.00 实付 ¥10.90")
	if err != nil || got != 1090 {
		t.Fatalf("got=%d err=%v", got, err)
	}
	if _, err := checkoutAmountCent("实付 ¥10.90 合计 ¥11.90"); err == nil {
		t.Fatal("不同金额候选必须阻断")
	}
}

func TestPurchaseGoodsURLPreservesOriginalCapturedURL(t *testing.T) {
	raw := "https://mobile.pinduoduo.com/goods.html?goods_id=977731220380&uin=user-token&page_from=35&_oak_rcto=fresh"
	if got := purchaseGoodsURL(raw, "977731220380"); got != raw {
		t.Fatalf("purchaseGoodsURL()=%q, want original captured URL", got)
	}
}

func TestWaitForAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Fatalf("path=%q", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	w := &worker{baseURL: server.URL, client: server.Client()}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := w.waitForAPI(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestWaitForAPITimesOutWithUsefulError(t *testing.T) {
	w := &worker{baseURL: "http://127.0.0.1:1", client: &http.Client{Timeout: 20 * time.Millisecond}}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	err := w.waitForAPI(ctx)
	if err == nil || !strings.Contains(err.Error(), "等待履约 API 就绪超时") {
		t.Fatalf("err=%v", err)
	}
}

func TestQuantityButtonPlan(t *testing.T) {
	tests := []struct {
		current, desired int
		label            string
		clicks           int
	}{
		{1, 1, "", 0},
		{1, 3, "增加数量", 2},
		{4, 2, "减少数量", 2},
	}
	for _, test := range tests {
		label, clicks, err := quantityButtonPlan(test.current, test.desired)
		if err != nil || label != test.label || clicks != test.clicks {
			t.Fatalf("%d→%d label=%q clicks=%d err=%v", test.current, test.desired, label, clicks, err)
		}
	}
	if _, _, err := quantityButtonPlan(1, 0); err == nil {
		t.Fatal("zero desired quantity must fail")
	}
}

func TestUnpaidOrdersURLKeepsRequiredListParameters(t *testing.T) {
	for _, value := range []string{"type=1", "comment_tab=1", "combine_orders=1", "main_orders=1", "order_index=0"} {
		if !strings.Contains(pddUnpaidOrdersURL, value) {
			t.Fatalf("missing %s in %s", value, pddUnpaidOrdersURL)
		}
	}
}
