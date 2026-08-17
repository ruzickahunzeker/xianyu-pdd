package pddproduct

import "testing"

func TestParseHTML(t *testing.T) {
	html := []byte(`<script>window.rawData={"store":{"goods":{"goodsName":"测试商品","sku":[{"skuId":188,"goodsId":936,"quantity":7,"isOnsale":1,"groupPrice":"12.34","specs":[{"spec_key":"颜色","spec_value":"黑"}]}],"transmission":{"sku_direct_order_extend_info":{"detail_id":246}},"oakData":{"activityCollection":{"activity":{"activityId":246}}},"neighborGroup":{"combine_group_list":[{"group_order_id":"336"}]}}}};</script>`)
	s, err := ParseHTML(html)
	if err != nil {
		t.Fatal(err)
	}
	if s.GoodsID != "936" || s.Title != "测试商品" || s.DetailID != "246" || len(s.SKUs) != 1 || s.SKUs[0].PriceCent != 1234 || s.SKUs[0].Stock != 7 || !s.SKUs[0].IsOnsale || len(s.GroupOrderIDs) != 1 {
		t.Fatalf("unexpected snapshot: %#v", s)
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
