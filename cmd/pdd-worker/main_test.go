package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"time"
	"xianyu-go/internal/pddproduct"
	"xianyu-go/internal/pddsite"
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
	if got := purchaseGoodsURL(raw, "977731220380", pddsite.Pinduoduo); got != raw {
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
	pddUnpaidOrdersURL := unpaidOrdersURL(pddsite.Pinduoduo)
	for _, value := range []string{"type=1", "comment_tab=1", "combine_orders=1", "main_orders=1", "order_index=0"} {
		if !strings.Contains(pddUnpaidOrdersURL, value) {
			t.Fatalf("missing %s in %s", value, pddUnpaidOrdersURL)
		}
	}
}

func TestMerchantEntryUsesOrderOrCapturedProductPage(t *testing.T) {
	orderURL, button, pageName, err := merchantEntryForTask(pddsite.Pinduoduo, messageTask{PDDOrderID: "260821-1", GoodsID: "123"}, "")
	if err != nil || orderURL != "https://mobile.pinduoduo.com/order.html?order_sn=260821-1" || button != "联系商家" || pageName != "订单" {
		t.Fatalf("order entry url=%q button=%q page=%q err=%v", orderURL, button, pageName, err)
	}
	productURL := "https://mobile.pinduoduo.com/goods.html?goods_id=123&uin=saved&page_from=35"
	got, button, pageName, err := merchantEntryForTask(pddsite.Pinduoduo, messageTask{GoodsID: "123"}, productURL)
	if err != nil || got != productURL || button != "客服" || pageName != "商品" {
		t.Fatalf("product entry url=%q button=%q page=%q err=%v", got, button, pageName, err)
	}
}

func TestPurchaseCheckoutURLSelectsActiveGroup(t *testing.T) {
	now := time.Unix(100, 0)
	snapshot := pddproduct.Snapshot{GroupID: "fallback", DetailID: "fallback-detail", GroupOffers: []pddproduct.GroupOffer{
		{GroupOrderID: "expired", GroupID: "g-old", DetailID: "d-old", ExpireAt: 99},
		{GroupOrderID: "active", GroupID: "g-new", DetailID: "d-new", ExpireAt: 200},
	}}
	got, mode, err := purchaseCheckoutURL(pddsite.Pinduoduo, snapshot, "936", "188", 2, now)
	if err != nil || mode != "join_group" {
		t.Fatalf("url=%q mode=%q err=%v", got, mode, err)
	}
	u, _ := url.Parse(got)
	want := map[string]string{"goods_id": "936", "sku_id": "188", "goods_number": "2", "group_id": "g-new", "detail_id": "d-new", "group_order_id": "active", "is_history_group": "1", "page_from": "0"}
	for key, value := range want {
		if u.Query().Get(key) != value {
			t.Errorf("%s=%q want %q in %s", key, u.Query().Get(key), value, got)
		}
	}
}

func TestPurchaseCheckoutURLCreatesGroupOrFallsBackDirect(t *testing.T) {
	create, mode, err := purchaseCheckoutURL(pddsite.Pinduoduo, pddproduct.Snapshot{GroupID: "g", DetailID: "d"}, "936", "188", 1, time.Unix(100, 0))
	if err != nil || mode != "create_group" || !strings.Contains(create, "group_id=g") || !strings.Contains(create, "detail_id=d") || !strings.Contains(create, "page_from=31") {
		t.Fatalf("create url=%q mode=%q err=%v", create, mode, err)
	}
	direct, mode, err := purchaseCheckoutURL(pddsite.Pinduoduo, pddproduct.Snapshot{}, "936", "188", 1, time.Unix(100, 0))
	if err != nil || mode != "direct" || strings.Contains(direct, "group_id=") || !strings.Contains(direct, "goods_number=1") {
		t.Fatalf("direct url=%q mode=%q err=%v", direct, mode, err)
	}
}

func TestPurchaseCheckoutURLRejectsIncompleteJoinContext(t *testing.T) {
	_, _, err := purchaseCheckoutURL(pddsite.Pinduoduo, pddproduct.Snapshot{GroupOffers: []pddproduct.GroupOffer{{GroupOrderID: "active"}}}, "936", "188", 1, time.Unix(100, 0))
	if err == nil || !strings.Contains(err.Error(), "group_id") {
		t.Fatalf("err=%v", err)
	}
}
