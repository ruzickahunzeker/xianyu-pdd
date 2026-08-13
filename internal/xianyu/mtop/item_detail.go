// Package mtop: 商品详情域 — 补充商品列表未返回的多规格信息。
package mtop

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"xianyu-go/internal/xianyu/protocol"
)

// ItemDetailFetcher 是商品同步使用的可选详情能力。
type ItemDetailFetcher interface {
	DetectItemMultiSpec(ctx context.Context, cookies, itemID string) (bool, error)
	FetchItemDetail(ctx context.Context, cookies, itemID string) (*ItemRemoteDetail, error)
}

var _ ItemDetailFetcher = (*ClientImpl)(nil)

// DetectItemMultiSpec 查询商品详情并识别多规格结构。该调用不主动刷新 token；
// 当 ctx 携带 CookieSession 时会像浏览器一样吸收响应 Cookie。
func (c *ClientImpl) DetectItemMultiSpec(ctx context.Context, cookies, itemID string) (bool, error) {
	detail, err := c.FetchItemDetail(ctx, cookies, itemID)
	if err != nil {
		return false, err
	}
	return detail.IsMultiSpec, nil
}

type ItemRemoteDetail struct {
	ItemID, Title, Description, StatusText, TransportFee string
	Category                                             map[string]any
	Images                                               []string
	MinPriceCents, MaxPriceCents, TotalQuantity          int64
	Status                                               int
	IsMultiSpec                                          bool
	SKUs                                                 []ItemRemoteSKU
	RawData                                              map[string]any
}

type ItemRemoteSKU struct {
	SKUID, InventoryID   string
	PriceCents, Quantity int64
	Properties           []map[string]any
	PropertyImageURL     string
	Features             map[string]any
	Enabled              bool
	Status, SortOrder    int
	RawData              map[string]any
}

func (c *ClientImpl) FetchItemDetail(ctx context.Context, cookies, itemID string) (*ItemRemoteDetail, error) {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return nil, fmt.Errorf("item_id 不能为空")
	}
	endpoint := c.ItemDetailURL
	if endpoint == "" {
		endpoint = ItemDetailAPI
	}
	documentURL := "https://www.goofish.com/item?id=" + url.QueryEscape(itemID)
	signingCookies, requestCookies := mtopRequestCookies(ctx, cookies, documentURL, endpoint)
	token := protocol.SignToken(signingCookies)
	if token == "" {
		return nil, fmt.Errorf("cookie 缺少 _m_h5_tk，无法获取商品详情")
	}
	dataVal := `{"itemId":` + strconv.Quote(itemID) + `}`
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	sign := protocol.GenerateSign(timestamp, token, dataVal)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"?"+buildItemDetailQuery(timestamp, sign), strings.NewReader("data="+url.QueryEscape(dataVal)))
	if err != nil {
		return nil, err
	}
	setCommonHeaders(req, requestCookies)
	req.Header.Set("Origin", "https://www.goofish.com")
	req.Header.Set("Referer", documentURL)

	hc := c.httpClient()
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("商品详情请求失败: %w", err)
	}
	defer resp.Body.Close()
	absorbMTopResponseCookies(ctx, cookies, resp)
	raw, err := readMTopBody(resp)
	if err != nil {
		return nil, err
	}
	captureItemDetailResponse(itemID, resp.StatusCode, raw)
	var decoded struct {
		Ret  []string       `json:"ret"`
		Data map[string]any `json:"data"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return nil, fmt.Errorf("解析商品详情响应失败: %w (body=%s)", err, truncate(string(raw), 300))
	}
	if !hasMTopSuccess(decoded.Ret) {
		return nil, fmt.Errorf("商品详情接口返回非成功: ret=%v", decoded.Ret)
	}
	return parseItemRemoteDetail(decoded.Data), nil
}

func parseItemRemoteDetail(data map[string]any) *ItemRemoteDetail {
	item := mapFromAny(data["itemDO"])
	out := &ItemRemoteDetail{ItemID: mtopString(item["itemId"]), Title: mtopString(item["title"]), Description: mtopString(item["desc"]), Status: mtopInt(item["itemStatus"]), StatusText: mtopString(item["itemStatusStr"]), TransportFee: mtopString(item["transportFee"]), Category: mapFromAny(item["itemCatDTO"]), TotalQuantity: int64(mtopInt(item["quantity"])), RawData: data}
	out.MinPriceCents = moneyTextCents(mtopString(item["minPrice"]))
	out.MaxPriceCents = moneyTextCents(mtopString(item["maxPrice"]))
	if images, ok := item["imageInfos"].([]any); ok {
		for _, raw := range images {
			if u := mtopString(mapFromAny(raw)["url"]); u != "" {
				out.Images = append(out.Images, u)
			}
		}
	}
	rawSKUs, _ := item["skuList"].([]any)
	if len(rawSKUs) == 0 {
		rawSKUs, _ = item["idleItemSkuList"].([]any)
	}
	for idx, raw := range rawSKUs {
		sku := mapFromAny(raw)
		propsRaw, _ := sku["propertyList"].([]any)
		props := make([]map[string]any, 0, len(propsRaw))
		enabled := true
		status := 0
		for _, p := range propsRaw {
			pm := mapFromAny(p)
			props = append(props, pm)
			if v, ok := pm["enabled"]; ok && !mtopBool(v) {
				enabled = false
			}
			if st := mtopInt(pm["status"]); st != 0 {
				status = st
			}
		}
		img := mapFromAny(sku["propertyImage"])
		out.SKUs = append(out.SKUs, ItemRemoteSKU{SKUID: mtopString(sku["skuId"]), InventoryID: mtopString(sku["inventoryId"]), PriceCents: int64(mtopInt(sku["priceInCent"])), Quantity: int64(mtopInt(sku["quantity"])), Properties: props, PropertyImageURL: mtopString(img["url"]), Features: mapFromAny(sku["features"]), Enabled: enabled, Status: status, SortOrder: idx, RawData: sku})
	}
	out.IsMultiSpec = len(out.SKUs) > 1 || detectItemMultiSpec(data)
	return out
}

func moneyTextCents(v string) int64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil || f < 0 {
		return 0
	}
	return int64(f*100 + 0.5)
}

// captureItemDetailResponse 是显式启用的本地同步诊断探针。它只保存指定商品的
// 原始详情响应和不含 Cookie/Token/签名的请求摘要，默认完全关闭。
func captureItemDetailResponse(itemID string, statusCode int, raw []byte) {
	dir := strings.TrimSpace(os.Getenv("XIANYU_ITEM_SYNC_CAPTURE_DIR"))
	target := strings.TrimSpace(os.Getenv("XIANYU_ITEM_SYNC_CAPTURE_ITEM_ID"))
	if dir == "" || target == "" || itemID != target {
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	stamp := time.Now().UTC().Format("20060102T150405.000000000Z")
	base := filepath.Join(dir, stamp+"-item-"+itemID)
	meta, err := json.MarshalIndent(map[string]any{
		"captured_at": time.Now().UTC().Format(time.RFC3339Nano),
		"item_id":     itemID,
		"request": map[string]any{
			"method": "POST", "api": "mtop.taobao.idle.pc.detail", "version": "1.0",
			"data":       map[string]string{"itemId": itemID},
			"query_keys": []string{"accountSite", "api", "appKey", "dataType", "jsv", "sessionOption", "sign", "spm_cnt", "t", "timeout", "type", "v"},
			"redacted":   []string{"Cookie", "_m_h5_tk", "sign"},
		},
		"response": map[string]any{"http_status": statusCode, "bytes": len(raw)},
	}, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(base+".json", raw, 0o600)
	_ = os.WriteFile(base+".meta.json", meta, 0o600)
}

func buildItemDetailQuery(timestamp, sign string) string {
	values := url.Values{
		"jsv":           {"2.7.2"},
		"appKey":        {protocol.SignAppKey},
		"t":             {timestamp},
		"sign":          {sign},
		"v":             {"1.0"},
		"type":          {"originaljson"},
		"accountSite":   {"xianyu"},
		"dataType":      {"json"},
		"timeout":       {"20000"},
		"api":           {"mtop.taobao.idle.pc.detail"},
		"sessionOption": {"AutoLoginOnly"},
		"spm_cnt":       {"a21ybx.item.0.0"},
	}
	return values.Encode()
}

func detectItemMultiSpec(value any) bool {
	return detectMultiSpecValue(value, false, 0)
}

func detectMultiSpecValue(value any, skuContext bool, depth int) bool {
	if depth > 16 {
		return false
	}
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			normalized := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))
			switch normalized {
			case "multisku", "ismultisku", "ismultispec", "multiplesku":
				if mtopBool(child) {
					return true
				}
			case "skulist", "skus":
				if list, ok := child.([]any); ok && len(list) > 1 {
					return true
				}
			case "skuprops", "skuproperties", "specprops", "specifications":
				if list, ok := child.([]any); ok && len(list) > 0 {
					return true
				}
			case "props", "properties":
				if skuContext {
					if list, ok := child.([]any); ok && len(list) > 0 {
						return true
					}
				}
			}
			nextSKUContext := skuContext || normalized == "skudo" || normalized == "skubase" || normalized == "skumodel"
			if detectMultiSpecValue(child, nextSKUContext, depth+1) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if detectMultiSpecValue(child, skuContext, depth+1) {
				return true
			}
		}
	}
	return false
}
