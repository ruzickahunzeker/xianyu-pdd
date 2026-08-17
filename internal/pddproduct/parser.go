package pddproduct

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Snapshot is the purchase-time view of a Pinduoduo goods page. RawData is
// deliberately omitted: callers persist the normalized snapshot, not cookies
// or arbitrary executable page content.
type Snapshot struct {
	GoodsID       string   `json:"goods_id"`
	Title         string   `json:"title"`
	DetailID      string   `json:"detail_id,omitempty"`
	ActivityID    string   `json:"activity_id,omitempty"`
	GroupOrderIDs []string `json:"group_order_ids,omitempty"`
	SKUs          []SKU    `json:"skus"`
}

type SKU struct {
	SKUID      string         `json:"sku_id"`
	GoodsID    string         `json:"goods_id"`
	ThumbURL   string         `json:"thumb_url,omitempty"`
	PriceCent  int64          `json:"price_cent"`
	Stock      int64          `json:"stock"`
	StockExact bool           `json:"stock_exact"`
	IsOnsale   bool           `json:"is_onsale"`
	Prices     map[string]any `json:"prices,omitempty"`
	Specs      []Spec         `json:"specs,omitempty"`
}

type Spec struct {
	SpecKey     string `json:"spec_key"`
	SpecKeyID   string `json:"spec_key_id,omitempty"`
	SpecValueID string `json:"spec_value_id,omitempty"`
	RawValue    string `json:"raw_value"`
}

func ParseHTML(html []byte) (Snapshot, error) {
	const marker = "window.rawData="
	i := bytes.Index(html, []byte(marker))
	if i < 0 {
		return Snapshot{}, errors.New("商品页缺少 window.rawData")
	}
	dec := json.NewDecoder(bytes.NewReader(html[i+len(marker):]))
	dec.UseNumber()
	var root any
	if err := dec.Decode(&root); err != nil {
		return Snapshot{}, fmt.Errorf("解析商品页 rawData: %w", err)
	}
	out := Snapshot{SKUs: []SKU{}}
	skuIndex, seenGroup := map[string]int{}, map[string]bool{}
	walk(root, func(m map[string]any) {
		if out.Title == "" {
			out.Title = text(m["goodsName"])
		}
		if out.DetailID == "" {
			out.DetailID = text(m["detail_id"])
		}
		if out.ActivityID == "" {
			out.ActivityID = text(m["activityId"])
		}
		if id := text(m["group_order_id"]); id != "" && !seenGroup[id] {
			seenGroup[id] = true
			out.GroupOrderIDs = append(out.GroupOrderIDs, id)
		}
		skuID, goodsID := text(m["skuId"]), text(m["goodsId"])
		if skuID == "" || goodsID == "" {
			return
		}
		price := cents(m["groupPrice"])
		if price == 0 {
			price = integer(m["groupPriceCent"])
		}
		if price == 0 {
			price = integer(m["skuPrice"])
		}
		if price == 0 {
			price = cents(m["normalPrice"])
		}
		if price == 0 {
			price = integer(m["oldGroupPrice"])
		}
		onsale := integer(m["isOnsale"]) == 1
		prices := map[string]any{"group_price": m["groupPrice"], "old_group_price": m["oldGroupPrice"], "sku_price": m["skuPrice"], "normal_price": m["normalPrice"]}
		stock := integer(m["quantity"])
		stockExact := !(stock >= 1000 && integer(m["limitQuantity"]) > stock)
		candidate := SKU{SKUID: skuID, GoodsID: goodsID, ThumbURL: text(m["thumbUrl"]), PriceCent: price, Stock: stock, StockExact: stockExact, IsOnsale: onsale, Prices: prices, Specs: normalizeSpecs(m["specs"])}
		if index, exists := skuIndex[skuID]; exists {
			current := &out.SKUs[index]
			if candidate.PriceCent > 0 {
				current.PriceCent = candidate.PriceCent
			}
			if candidate.Stock > 0 {
				current.Stock = candidate.Stock
				current.StockExact = candidate.StockExact
			}
			if candidate.IsOnsale {
				current.IsOnsale = true
			}
			if len(candidate.Specs) > 0 {
				current.Specs = candidate.Specs
			}
			if candidate.ThumbURL != "" {
				current.ThumbURL = candidate.ThumbURL
			}
			if candidate.PriceCent > 0 {
				current.Prices = candidate.Prices
			}
		} else {
			skuIndex[skuID] = len(out.SKUs)
			out.SKUs = append(out.SKUs, candidate)
		}
		if out.GoodsID == "" {
			out.GoodsID = goodsID
		}
	})
	if out.GoodsID == "" || len(out.SKUs) == 0 {
		return Snapshot{}, errors.New("商品页未解析到 goods_id 或 SKU")
	}
	return out, nil
}

func normalizeSpecs(v any) []Spec {
	rows, _ := v.([]any)
	out := make([]Spec, 0, len(rows))
	for _, row := range rows {
		m, _ := row.(map[string]any)
		if m == nil {
			continue
		}
		spec := Spec{SpecKey: text(m["spec_key"]), SpecKeyID: text(m["spec_key_id"]), SpecValueID: text(m["spec_value_id"]), RawValue: text(m["spec_value"])}
		if spec.RawValue == "" {
			spec.RawValue = text(m["raw_value"])
		}
		if spec.SpecKey != "" || spec.RawValue != "" {
			out = append(out, spec)
		}
	}
	return out
}

func walk(v any, fn func(map[string]any)) {
	switch x := v.(type) {
	case map[string]any:
		fn(x)
		for _, child := range x {
			walk(child, fn)
		}
	case []any:
		for _, child := range x {
			walk(child, fn)
		}
	}
}

func text(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case json.Number:
		return x.String()
	case float64:
		return strconv.FormatInt(int64(x), 10)
	case int64:
		return strconv.FormatInt(x, 10)
	}
	return ""
}

func integer(v any) int64 {
	s, _ := strconv.ParseInt(text(v), 10, 64)
	return s
}

func cents(v any) int64 {
	s := text(v)
	if s == "" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f < 0 {
		return 0
	}
	return int64(f*100 + .5)
}
