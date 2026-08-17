package pddshipping

import "testing"

func TestParseDetailHTML(t *testing.T) {
	html := []byte(`<script>window.rawData={"order_sn":"260816-1","goods_id":"979","sku_id":"188","shippingTime":1786881784,"shipping":{"shippingName":"韵达快递","trackingNumber":"465592976160187"},"shippingName":"韵达快递","trackingNumber":"465592976160187"}</script>`)
	got := ParseHTML(html)
	if len(got) != 1 || got[0].OrderID != "260816-1" || got[0].Company != "韵达快递" || got[0].TrackingNumber != "465592976160187" {
		t.Fatalf("unexpected: %#v", got)
	}
}

func TestParseDetailHTMLCamelCaseGoodsAndSKU(t *testing.T) {
	html := []byte(`<script>window.rawData={"data":{"orderGoods":[{"goodsId":979869739072,"skuId":1942483899844,"goodsNumber":1}],"orderSn":"260816-024421353043519","shippingTime":1786881784,"shipping":{"shippingName":"韵达快递"},"trackingNumber":"465592976160187"}}</script>`)
	got := ParseHTML(html)
	if len(got) != 1 || got[0].GoodsID != "979869739072" || got[0].SKUID != "1942483899844" || got[0].Quantity != 1 {
		t.Fatalf("unexpected: %#v", got)
	}
}

func TestParseOrderIDsSupportsPDDListVariants(t *testing.T) {
	html := []byte(`<script>window.rawData={"initOdersList":{"3":[{"orderSn":"260816-024421353043519"}]},"link":"order.html?order_sn=260816-111111111111111&main_orders=1","legacy":{"order_sn":"260816-222222222222222"}}</script>`)
	got := ParseOrderIDs(html)
	want := map[string]bool{"260816-024421353043519": true, "260816-111111111111111": true, "260816-222222222222222": true}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for _, id := range got {
		if !want[id] {
			t.Fatalf("unexpected id %q in %v", id, got)
		}
	}
}
