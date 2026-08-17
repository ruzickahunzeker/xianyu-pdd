package server

import "testing"

func TestNormalizePublishedMaterialSKUZerosUnmappedPDDCombination(t *testing.T) {
	unmapped := map[string]any{"source_sku_id": "", "quantity": int64(1000)}
	normalizePublishedMaterialSKU("pdd", unmapped)
	if got := jsonInt64(unmapped["quantity"]); got != 0 {
		t.Fatalf("unmapped quantity=%d, want 0", got)
	}

	mapped := map[string]any{"source_sku_id": "1883590772174", "quantity": int64(1000)}
	normalizePublishedMaterialSKU("pdd", mapped)
	if got := jsonInt64(mapped["quantity"]); got != 1000 {
		t.Fatalf("mapped quantity=%d, want 1000", got)
	}

	manual := map[string]any{"source_sku_id": "", "quantity": int64(12)}
	normalizePublishedMaterialSKU("manual", manual)
	if got := jsonInt64(manual["quantity"]); got != 12 {
		t.Fatalf("manual quantity=%d, want 12", got)
	}
}

func TestNormalizeCollectedMaterialSpecificationsHidesFixedDimension(t *testing.T) {
	skus := []materialSKU{
		{Enabled: true, Properties: []materialProperty{{Name: "口味", Value: "牛肉"}, {Name: "包装", Value: "90支"}}},
		{Enabled: true, Properties: []materialProperty{{Name: "口味", Value: "鸡肉"}, {Name: "包装", Value: "90支"}}},
	}
	for index := range skus {
		skus[index].SourceProperties = append([]materialProperty(nil), skus[index].Properties...)
	}
	normalizeCollectedMaterialSpecifications(skus)
	for index, sku := range skus {
		if len(sku.Properties) != 1 || sku.Properties[0].Name != "口味" {
			t.Fatalf("sku %d properties=%v, want only 口味", index, sku.Properties)
		}
		if len(sku.SourceProperties) != 2 {
			t.Fatalf("sku %d source properties were changed: %v", index, sku.SourceProperties)
		}
	}
}
