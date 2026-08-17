package pddproduct

import (
	"net/url"
	"strings"
)

// ProductURL preserves the captured Pinduoduo page context when it belongs to
// the requested product. Some products return a degraded page for the compact
// goods_id-only URL, so callers should only use the compact URL as a fallback.
func ProductURL(raw, goodsID string) string {
	fallback := "https://mobile.pinduoduo.com/goods.html?goods_id=" + url.QueryEscape(goodsID)
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() != "mobile.pinduoduo.com" || parsed.Path != "/goods.html" || parsed.Query().Get("goods_id") != goodsID {
		return fallback
	}
	return parsed.String()
}
