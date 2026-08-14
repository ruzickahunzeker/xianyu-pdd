package server

import "testing"

func TestMaterialImageURLTreatsMissingOrNilAsEmpty(t *testing.T) {
	tests := []struct {
		name     string
		property map[string]any
		want     string
	}{
		{name: "missing", property: map[string]any{}, want: ""},
		{name: "nil", property: map[string]any{"image_url": nil}, want: ""},
		{name: "blank", property: map[string]any{"image_url": "  "}, want: ""},
		{name: "url", property: map[string]any{"image_url": " https://img.example/spec.jpg "}, want: "https://img.example/spec.jpg"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := materialImageURL(test.property); got != test.want {
				t.Fatalf("materialImageURL()=%q want=%q", got, test.want)
			}
		})
	}
}

func TestMaterialPropertiesKey(t *testing.T) {
	properties := []materialProperty{{Name: "口味", Value: "金枪鱼"}, {Name: "款式", Value: "6罐"}}
	if got, want := materialPropertiesKey(properties), "口味\x00金枪鱼\x01款式\x006罐"; got != want {
		t.Fatalf("materialPropertiesKey()=%q, want %q", got, want)
	}
}

func TestMaterialSourceGoodsIDsSupportsBundlesAndLegacyRows(t *testing.T) {
	material := map[string]any{
		"source_id": "goods-a",
		"skus": []any{
			map[string]any{"source_sku_id": "sku-a"},
			map[string]any{"source_goods_id": "goods-b", "source_sku_id": "sku-b"},
			map[string]any{"source_goods_id": "goods-b", "source_sku_id": "sku-c"},
		},
	}
	got := materialSourceGoodsIDs(material)
	if len(got) != 2 || got[0] != "goods-a" || got[1] != "goods-b" {
		t.Fatalf("materialSourceGoodsIDs()=%v", got)
	}
	if key := materialSourceKey("goods-b", "sku-b"); key != "goods-b\x00sku-b" {
		t.Fatalf("materialSourceKey()=%q", key)
	}
}

func TestMaterialTextTreatsNilAsEmpty(t *testing.T) {
	if got := materialText(nil); got != "" {
		t.Fatalf("materialText(nil)=%q", got)
	}
}
