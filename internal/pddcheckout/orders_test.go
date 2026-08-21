package pddcheckout

import "testing"

func TestParseUnpaidHTML(t *testing.T) {
	html := []byte(`<script>window.rawData={"initOdersList":{"1":[{"order_sn":"260815-1","group_order_id":"g1","address_id":"609","order_amount":179,"order_time":10,"next_pay_time_out":20,"pay_status":0,"shipping_status":0,"order_goods":[{"goods_id":"388","sku_id":"187","goods_name":"测试","goods_number":1,"spec":"16"}]}]}};</script>`)
	orders, err := ParseUnpaidHTML(html)
	if err != nil || len(orders) != 1 {
		t.Fatalf("orders=%+v err=%v", orders, err)
	}
	got := orders[0]
	if got.OrderID != "260815-1" || got.GoodsID != "388" || got.SKUID != "187" || got.AmountCent != 179 || got.Quantity != 1 {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestNormalizeAddress(t *testing.T) {
	if got := NormalizeAddress(" 北京市 海淀区，中关村 1 号。 "); got != "北京市海淀区,中关村1号" {
		t.Fatalf("got %q", got)
	}
}

func TestParseUnpaidHTMLCamelCase(t *testing.T) {
	html := []byte(`<script>window.rawData={"initOrdersList":{"1":[{"orderSn":"260815-2","groupOrderId":"g2","addressId":609,"orderAmount":"10.90","orderTime":11,"nextPayTimeOut":21,"orderGoods":[{"goodsId":975,"skuId":193,"goodsName":"数据线","goodsNumber":2,"spec":"Type-C"}]}]}};</script>`)
	orders, err := ParseUnpaidHTML(html)
	if err != nil || len(orders) != 1 {
		t.Fatalf("orders=%+v err=%v", orders, err)
	}
	got := orders[0]
	if got.OrderID != "260815-2" || got.GoodsID != "975" || got.SKUID != "193" || got.AmountCent != 1090 || got.Quantity != 2 {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestParseOrdersHTMLAllOrders(t *testing.T) {
	html := []byte(`<script>window.rawData={"initOdersList":{"0":[{"order_sn":"paid-1","order_amount":399,"order_time":12,"pay_status":2,"order_goods":[{"goods_id":"936","sku_id":"188","goods_number":2}]}],"1":[]}};</script>`)
	orders, err := ParseOrdersHTML(html, "0")
	if err != nil || len(orders) != 1 || orders[0].OrderID != "paid-1" || orders[0].PayStatus != 2 {
		t.Fatalf("orders=%+v err=%v", orders, err)
	}
}
