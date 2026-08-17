package server

import "testing"

func TestExactCarrierCodeUsesOfficialHotList(t *testing.T) {
	for name, want := range map[string]string{"韵达快递": "YUNDA", "极兔速递": "HTKY", "邮政快递包裹": "POSTB", "中通快递": "ZTO"} {
		if got := exactCarrierCode(name); got != want {
			t.Fatalf("%s=%s want %s", name, got, want)
		}
	}
	for _, name := range []string{"韵达", "京东快递", "中通快运"} {
		if got := exactCarrierCode(name); got != "" {
			t.Fatalf("ambiguous %s must not auto map: %s", name, got)
		}
	}
}
