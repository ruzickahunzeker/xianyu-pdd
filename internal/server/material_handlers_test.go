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
