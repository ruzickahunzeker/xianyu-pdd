package pddproduct

import (
	"testing"
	"time"
)

func TestParseHTML(t *testing.T) {
	html := []byte(`<script>window.rawData={"store":{"goods":{"goodsName":"测试商品","sku":[{"skuId":188,"goodsId":936,"quantity":7,"isOnsale":1,"groupPrice":"12.34","specs":[{"spec_key":"颜色","spec_value":"黑"}]}],"transmission":{"sku_direct_order_extend_info":{"detail_id":246,"group_id":163}},"oakData":{"activityCollection":{"activity":{"activityId":246}}},"neighborGroup":{"combine_group_list":[{"group_order_id":"336","expire_time":200}]}}}};</script>`)
	s, err := ParseHTML(html)
	if err != nil {
		t.Fatal(err)
	}
	if s.GoodsID != "936" || s.Title != "测试商品" || s.DetailID != "246" || s.GroupID != "163" || len(s.SKUs) != 1 || s.SKUs[0].PriceCent != 1234 || s.SKUs[0].Stock != 7 || !s.SKUs[0].IsOnsale || len(s.GroupOrderIDs) != 1 {
		t.Fatalf("unexpected snapshot: %#v", s)
	}
	if len(s.GroupOffers) != 1 || s.GroupOffers[0].GroupOrderID != "336" || s.GroupOffers[0].GroupID != "163" || s.GroupOffers[0].DetailID != "246" || s.GroupOffers[0].ExpireAt != 200 {
		t.Fatalf("unexpected group offers: %#v", s.GroupOffers)
	}
}

func TestSelectActiveGroupOfferHandlesSecondsAndMilliseconds(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	snapshot := Snapshot{GroupOffers: []GroupOffer{
		{GroupOrderID: "expired-seconds", GroupID: "g1", DetailID: "d1", ExpireAt: 1_799_999_999},
		{GroupOrderID: "expired-millis", GroupID: "g2", DetailID: "d2", ExpireAt: 1_799_999_999_000},
		{GroupOrderID: "active", GroupID: "g3", DetailID: "d3", ExpireAt: 1_800_000_001_000},
	}}
	offer, ok, err := SelectActiveGroupOffer(snapshot, now)
	if err != nil || !ok || offer.GroupOrderID != "active" {
		t.Fatalf("offer=%#v ok=%v err=%v", offer, ok, err)
	}
}

func TestSelectActiveGroupOfferRejectsIncompleteActiveOffer(t *testing.T) {
	_, _, err := SelectActiveGroupOffer(Snapshot{GroupOffers: []GroupOffer{{GroupOrderID: "active"}}}, time.Unix(100, 0))
	if err == nil {
		t.Fatal("expected incomplete group context error")
	}
}

func TestParseHTMLMarksPageStockCapAsInexact(t *testing.T) {
	html := []byte(`<script>window.rawData={"store":{"skus":[{"skuId":1883590772174,"goodsId":936179834486,"quantity":1000,"limitQuantity":999999,"isOnsale":1,"groupPrice":"9.99"}]}};</script>`)
	s, err := ParseHTML(html)
	if err != nil {
		t.Fatal(err)
	}
	if s.SKUs[0].Stock != 1000 || s.SKUs[0].StockExact {
		t.Fatalf("stock=%d exact=%v", s.SKUs[0].Stock, s.SKUs[0].StockExact)
	}
}

func TestProductURLPreservesCapturedContext(t *testing.T) {
	raw := "https://mobile.pinduoduo.com/goods.html?goods_id=977731220380&uin=user-token&page_from=35&_oak_rcto=fresh"
	if got := ProductURL(raw, "977731220380"); got != raw {
		t.Fatalf("ProductURL()=%q, want captured URL", got)
	}
	want := "https://mobile.pinduoduo.com/goods.html?goods_id=977731220380"
	for _, invalid := range []string{"", "chrome-extension://id/background.js", "https://mobile.pinduoduo.com/goods.html?goods_id=other"} {
		if got := ProductURL(invalid, "977731220380"); got != want {
			t.Fatalf("ProductURL(%q)=%q, want %q", invalid, got, want)
		}
	}
}
