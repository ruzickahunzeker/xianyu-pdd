package pddshipping

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

type Shipment struct {
	OrderID, GoodsID, SKUID, Company, TrackingNumber string
	Quantity, ShippedAt                              int64
}

var objectRE = regexp.MustCompile(`\{[^{}]{0,1000}(?:"order_sn"|"orderSn")[^{}]{0,1000}\}`)

var orderIDPatterns = []*regexp.Regexp{
	regexp.MustCompile(`"order_sn"\s*:\s*"([0-9-]{10,})"`),
	regexp.MustCompile(`"orderSn"\s*:\s*"([0-9-]{10,})"`),
	regexp.MustCompile(`(?:[?&]|\\u0026)order_sn=([0-9-]{10,})`),
}

// ParseOrderIDs extracts order numbers from an order-list page. PDD currently
// mixes orderSn, order_sn and order-detail URLs in the same raw payload.
func ParseOrderIDs(html []byte) []string {
	seen, out := map[string]bool{}, []string{}
	body := string(html)
	for _, pattern := range orderIDPatterns {
		for _, match := range pattern.FindAllStringSubmatch(body, -1) {
			if len(match) < 2 || seen[match[1]] {
				continue
			}
			seen[match[1]] = true
			out = append(out, match[1])
		}
	}
	return out
}

// ParseHTML extracts stable logistics fields from PDD's server-rendered raw data.
// Generated CSS classes are intentionally ignored.
func ParseHTML(html []byte) []Shipment {
	seen, out := map[string]bool{}, []Shipment{}
	for _, raw := range objectRE.FindAll(html, -1) {
		var v map[string]any
		if json.Unmarshal(raw, &v) != nil {
			continue
		}
		s := Shipment{
			OrderID: text(v, "order_sn", "orderSn"), GoodsID: text(v, "goods_id", "goodsId"),
			SKUID: text(v, "sku_id", "skuId"), Company: text(v, "shippingName", "shipping_name"),
			TrackingNumber: text(v, "trackingNumber", "tracking_number"),
			Quantity:       number(v, "goods_number", "goodsNumber", "quantity"), ShippedAt: number(v, "shippingTime", "shipping_time"),
		}
		if s.OrderID != "" && s.TrackingNumber != "" && !seen[s.OrderID+"\x00"+s.TrackingNumber] {
			seen[s.OrderID+"\x00"+s.TrackingNumber] = true
			out = append(out, s)
		}
	}
	// Detail pages often keep order and logistics in separate nested objects.
	if len(out) == 0 {
		body := string(html)
		s := Shipment{
			OrderID: captureAny(body, `"order_sn"\s*:\s*"([^"]+)"`, `"orderSn"\s*:\s*"([^"]+)"`),
			GoodsID: captureAny(body, `"goods_id"\s*:\s*"?([0-9]+)"?`, `"goodsId"\s*:\s*"?([0-9]+)"?`),
			SKUID: captureAny(body, `"sku_id"\s*:\s*"?([0-9]+)"?`, `"skuId"\s*:\s*"?([0-9]+)"?`),
			Company: capture(body, `"shippingName"\s*:\s*"([^"]+)"`), TrackingNumber: capture(body, `"trackingNumber"\s*:\s*"([^"]+)"`),
			Quantity: numberFromBody(body, `"goods_number"\s*:\s*"?([0-9]+)"?`, `"goodsNumber"\s*:\s*"?([0-9]+)"?`),
			ShippedAt: numberFromBody(body, `"shippingTime"\s*:\s*"?([0-9]+)"?`, `"shipping_time"\s*:\s*"?([0-9]+)"?`),
		}
		if s.OrderID != "" && s.TrackingNumber != "" {
			out = append(out, s)
		}
	}
	return out
}

func capture(body, pattern string) string {
	m := regexp.MustCompile(pattern).FindStringSubmatch(body)
	if len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}
func captureAny(body string, patterns ...string) string {
	for _, pattern := range patterns {
		if value := capture(body, pattern); value != "" {
			return value
		}
	}
	return ""
}
func numberFromBody(body string, patterns ...string) int64 {
	value := captureAny(body, patterns...)
	n, _ := strconv.ParseInt(value, 10, 64)
	return n
}
func text(v map[string]any, keys ...string) string {
	for _, k := range keys {
		if x, ok := v[k]; ok {
			switch n := x.(type) {
			case string:
				return strings.TrimSpace(n)
			case float64:
				return strconv.FormatInt(int64(n), 10)
			}
		}
	}
	return ""
}
func number(v map[string]any, keys ...string) int64 {
	s := text(v, keys...)
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}
