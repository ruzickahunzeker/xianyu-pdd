package pddsite

import (
	"net/url"
	"strings"
	"testing"
)

func TestSiteURLsAndIsolation(t *testing.T) {
	q := url.Values{"goods_id": {"123"}}
	if got := Yangkeduo.URL("/goods.html", q); got != "https://mobile.yangkeduo.com/goods.html?goods_id=123" {
		t.Fatalf("unexpected URL: %s", got)
	}
	if Pinduoduo.CookieDomain() == Yangkeduo.CookieDomain() {
		t.Fatal("site cookies must use separate domains")
	}
	if Pinduoduo.ProfileDir("browser_data", "acc") == Yangkeduo.ProfileDir("browser_data", "acc") {
		t.Fatal("site browser profiles must be isolated")
	}
	if strings.Contains(Yangkeduo.ProfileDir("browser_data", "../acc"), "..") {
		t.Fatal("profile account ID should be sanitized")
	}
}

func TestParseAndDetect(t *testing.T) {
	if site, err := Parse(""); err != nil || site != Pinduoduo {
		t.Fatalf("legacy default: %v %v", site, err)
	}
	if site := Detect("https://mobile.yangkeduo.com/orders.html"); site != Yangkeduo {
		t.Fatalf("detect: %v", site)
	}
	if _, err := Parse("other"); err == nil {
		t.Fatal("unsupported site should fail")
	}
}
