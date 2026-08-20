package pddproduct

import (
	"net/url"
	"strings"

	"xianyu-go/internal/pddsite"
)

// ProductURL preserves the captured Pinduoduo page context when it belongs to
// the requested product. Some products return a degraded page for the compact
// goods_id-only URL, so callers should only use the compact URL as a fallback.
func ProductURL(raw, goodsID string) string {
	return ProductURLForSite(raw, goodsID, pddsite.Default)
}

func ProductURLForSite(raw, goodsID string, site pddsite.Site) string {
	fallback := site.URL("/goods.html", url.Values{"goods_id": {goodsID}})
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Hostname(), site.Host()) || parsed.Path != "/goods.html" || parsed.Query().Get("goods_id") != goodsID {
		return fallback
	}
	return parsed.String()
}
