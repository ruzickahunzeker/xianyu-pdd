package mtop

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestParseCapturedMultiSpecItemDetail(t *testing.T) {
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	raw := []byte(`{"data":{"itemDO":{"itemId":"sample","title":"测试","desc":"描述","minPrice":"20","maxPrice":"30","quantity":4,"skuList":[{"skuId":"s1","inventoryId":1106523020457808920,"priceInCent":3000,"quantity":1,"propertyList":[{"propertyText":"颜色","valueText":"黑色","enabled":true,"status":0},{"propertyText":"材质","valueText":"游戏","enabled":true,"status":0}]},{"skuId":"s2","inventoryId":"i2","priceInCent":2000,"quantity":1,"propertyList":[{"propertyText":"颜色","valueText":"黑色","enabled":true,"status":0},{"propertyText":"材质","valueText":"普通","enabled":true,"status":0}]},{"skuId":"s3","inventoryId":"i3","priceInCent":3000,"quantity":1,"propertyImage":{"url":"https://img.example/white.jpg"},"propertyList":[{"propertyText":"颜色","valueText":"白色","enabled":true,"status":0},{"propertyText":"材质","valueText":"游戏","enabled":true,"status":0}]},{"skuId":"s4","inventoryId":"i4","priceInCent":2000,"quantity":1,"propertyList":[{"propertyText":"颜色","valueText":"白色","enabled":true,"status":0},{"propertyText":"材质","valueText":"普通","enabled":true,"status":0}] }]}}}`)
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	detail := parseItemRemoteDetail(envelope.Data)
	if !detail.IsMultiSpec || len(detail.SKUs) != 4 || detail.MinPriceCents != 2000 || detail.MaxPriceCents != 3000 || detail.TotalQuantity != 4 {
		t.Fatalf("unexpected detail: %+v", detail)
	}
	if got := detail.SKUs[2]; got.SKUID != "s3" || got.PropertyImageURL == "" || len(got.Properties) != 2 {
		t.Fatalf("unexpected sku: %+v", got)
	}
	if got := detail.SKUs[0].InventoryID; got != "1106523020457808920" {
		t.Fatalf("inventory id lost precision: %q", got)
	}
}
