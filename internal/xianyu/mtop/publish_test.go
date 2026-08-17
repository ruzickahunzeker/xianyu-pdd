package mtop

import (
	"errors"
	"strings"
	"testing"
)

func TestPublishPayloadBuilders(t *testing.T) {
	req := PublishItemRequest{PriceCents: 1234, OriginalPriceCents: 2000, PostageMode: "fixed", PostageCents: 800}
	price := publishPriceDTO(req)
	if price["priceInCent"] != "1234" || price["origPriceInCent"] != "2000" {
		t.Fatalf("publishPriceDTO = %#v", price)
	}
	postage := postageDTO(req)
	if postage["postPriceInCent"] != "800" || postage["templateId"] != "0" {
		t.Fatalf("postageDTO = %#v", postage)
	}
	if got := postageDTO(PublishItemRequest{PostageMode: "free"}); got["canFreeShipping"] != true {
		t.Fatalf("free postage = %#v", got)
	}
	if got := postageDTO(PublishItemRequest{PostageMode: "distance"}); got["templateId"] != "-100" {
		t.Fatalf("distance postage = %#v", got)
	}
}

func TestPublishParsingAndErrors(t *testing.T) {
	if w, h := parsePix("800x600"); w != 800 || h != 600 {
		t.Fatalf("parsePix = %d x %d", w, h)
	}
	if w, h := parsePix("bad"); w != 0 || h != 0 {
		t.Fatalf("invalid parsePix = %d x %d", w, h)
	}
	if got := centsText(1234); got != "12.34" {
		t.Fatalf("centsText = %q", got)
	}
	if got := findStringDeep(map[string]any{"outer": map[string]any{"itemId": "42"}}, "itemId"); got != "42" {
		t.Fatalf("findStringDeep = %q", got)
	}

	err := classifyPublishError([]string{"FAIL_SYS_TOKEN_EXPIRED::令牌过期"}, map[string]any{})
	var publishErr *PublishError
	if !errors.As(err, &publishErr) || publishErr.Code != PublishErrorTokenExpired {
		t.Fatalf("token error = %#v", err)
	}
	err = classifyPublishError([]string{"账号没有库存发布权限"}, map[string]any{})
	if !errors.As(err, &publishErr) || publishErr.Code != PublishErrorStockPermissionMissing {
		t.Fatalf("stock error = %#v", err)
	}
}

func TestPublishSKUPayload(t *testing.T) {
	skus := []PublishSKU{
		{PriceCents: 3000, Quantity: 1, Properties: []PublishSKUProperty{{Name: "颜色", Value: "黑色"}, {Name: "材质", Value: "游戏"}}},
		{PriceCents: 2000, Quantity: 2, Properties: []PublishSKUProperty{{Name: "颜色", Value: "白色"}, {Name: "材质", Value: "普通"}}},
	}
	if err := validatePublishSKUs(skus); err != nil {
		t.Fatal(err)
	}
	price, quantity := publishSKUSummary(skus)
	if price != 2000 || quantity != 3 {
		t.Fatalf("summary price=%d quantity=%d", price, quantity)
	}
	properties, rows, propertyImages := publishSKUPayload(skus)
	if len(properties) != 2 || len(rows) != 2 {
		t.Fatalf("properties=%#v rows=%#v", properties, rows)
	}
	if len(propertyImages) != 0 {
		t.Fatalf("propertyImages=%#v", propertyImages)
	}
	row := rows[0].(map[string]any)
	if row["priceInCent"] != "3000" || row["quantity"] != 1 {
		t.Fatalf("row=%#v", row)
	}
}

func TestPublishSKUPayloadIncludesPropertyImages(t *testing.T) {
	skus := []PublishSKU{
		{PriceCents: 3000, Quantity: 1, Properties: []PublishSKUProperty{{Name: "颜色", Value: "黑色", ImageURL: "https://img.example/black.jpg"}}},
		{PriceCents: 3200, Quantity: 1, Properties: []PublishSKUProperty{{Name: "颜色", Value: "白色", ImageURL: "https://img.example/white.jpg"}}},
	}
	properties, _, images := publishSKUPayload(skus)
	if len(images) != 2 {
		t.Fatalf("images=%#v", images)
	}
	definition := properties[0].(map[string]any)
	if definition["supportImage"] != true {
		t.Fatalf("definition=%#v", definition)
	}
	values := definition["propertyValues"].([]any)
	if values[0].(map[string]any)["propertyValueImg"] == nil {
		t.Fatalf("values=%#v", values)
	}
}

func TestValidatePublishSKUsRejectsDuplicateCombination(t *testing.T) {
	skus := []PublishSKU{{PriceCents: 100, Quantity: 1, Properties: []PublishSKUProperty{{Name: "颜色", Value: "黑"}}}, {PriceCents: 200, Quantity: 1, Properties: []PublishSKUProperty{{Name: "颜色", Value: "黑"}}}}
	if err := validatePublishSKUs(skus); err == nil {
		t.Fatal("expected duplicate error")
	}
}

func TestValidatePublishSKUsRejectsSingleValueDimension(t *testing.T) {
	skus := []PublishSKU{
		{PriceCents: 100, Quantity: 1, Properties: []PublishSKUProperty{{Name: "口味", Value: "牛肉"}, {Name: "包装", Value: "90支"}}},
		{PriceCents: 200, Quantity: 1, Properties: []PublishSKUProperty{{Name: "口味", Value: "鸡肉"}, {Name: "包装", Value: "90支"}}},
	}
	err := validatePublishSKUs(skus)
	if err == nil || !strings.Contains(err.Error(), "包装") {
		t.Fatalf("validate error=%v, want single-value 包装 error", err)
	}
}

func TestNormalizePublishSKUImagesKeepsOnlyLastProperty(t *testing.T) {
	image := &PublishImage{Filename: "spec.png", ContentType: "image/png", Data: []byte("image")}
	skus := []PublishSKU{
		{PriceCents: 100, Quantity: 1, Properties: []PublishSKUProperty{{Name: "颜色", Value: "红", Image: image}, {Name: "尺码", Value: "M", Image: image}}},
		{PriceCents: 100, Quantity: 1, Properties: []PublishSKUProperty{{Name: "颜色", Value: "蓝"}, {Name: "尺码", Value: "L"}}},
	}
	normalizePublishSKUImages(skus)
	if skus[0].Properties[0].Image != nil || skus[0].Properties[1].Image == nil {
		t.Fatalf("images were not normalized: %#v", skus[0].Properties)
	}
	if err := validatePublishSKUs(skus); err != nil {
		t.Fatalf("normalized SKU should validate: %v", err)
	}
}
