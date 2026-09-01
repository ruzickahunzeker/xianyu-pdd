// Package mtop: 订单详情域 — mtop.idle.web.trade.order.detail 调用与重试。
package mtop

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"xianyu-go/internal/xianyu/protocol"
)

// OrderDetailResult 是订单详情接口中自动发货需要的字段。
type OrderDetailResult struct {
	Quantity       string
	SpecName       string
	SpecValue      string
	OrderStatus    string
	Amount         string
	UpdatedCookies string
}

// FetchOrderDetail 获取订单真实成交价、数量、状态和规格；token 过期时自动重签重试。
func (c *ClientImpl) FetchOrderDetail(ctx context.Context, cookiesStr, orderID string) (*OrderDetailResult, error) {
	currentCookies := cookiesStr
	if session := cookieSessionFromContext(ctx); session != nil {
		currentCookies, _, _ = session.State()
	}
	var lastRet []string
	for attempt := 0; attempt < 4; attempt++ {
		previousCookies := currentCookies
		result, ret, updated, err := c.fetchOrderDetailOnce(ctx, currentCookies, orderID)
		if err != nil {
			return nil, err
		}
		lastRet = ret
		if updated != "" {
			currentCookies = updated
		}
		if result != nil {
			result.UpdatedCookies = currentCookies
			return result, nil
		}
		if !isTokenExpiredRet(ret) {
			return nil, fmt.Errorf("订单详情接口返回非成功: ret=%v", ret)
		}
		if attempt == 3 {
			break
		}
		if currentCookies == previousCookies {
			refreshed, refreshErr := c.RefreshTokenContext(ctx, currentCookies)
			if refreshErr != nil {
				return nil, fmt.Errorf("订单详情 token 刷新失败: %w", refreshErr)
			}
			currentCookies = refreshed.UpdatedCookies
		}
		if err := sleepCtx(ctx, MTopRetryGap); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("订单详情 token 重试失败: ret=%v", lastRet)
}

func (c *ClientImpl) fetchOrderDetailOnce(ctx context.Context, cookiesStr, orderID string) (*OrderDetailResult, []string, string, error) {
	hc := c.httpClient()
	endpoint := c.OrderDetailURL
	if endpoint == "" {
		endpoint = OrderDetailAPI
	}
	documentURL := "https://www.goofish.com/order-detail?orderId=" + url.QueryEscape(orderID) + "&role=seller"
	signingCookies, requestCookies := mtopRequestCookies(ctx, cookiesStr, documentURL, endpoint)
	t := strconv.FormatInt(time.Now().UnixMilli(), 10)
	dataVal := `{"tid":"` + orderID + `"}`
	sign := protocol.GenerateSign(t, protocol.SignToken(signingCookies), dataVal)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"?"+buildOrderDetailQuery(t, sign), strings.NewReader("data="+url.QueryEscape(dataVal)))
	if err != nil {
		return nil, nil, cookiesStr, err
	}
	setCommonHeaders(req, requestCookies)
	req.Header.Set("Referer", documentURL)
	resp, err := hc.Do(req)
	if err != nil {
		return nil, nil, cookiesStr, fmt.Errorf("订单详情请求失败: %w", err)
	}
	defer resp.Body.Close()
	updated := absorbMTopResponseCookies(ctx, cookiesStr, resp)
	raw, err := readMTopBody(resp)
	if err != nil {
		return nil, nil, updated, err
	}
	var decoded struct {
		Ret  []string       `json:"ret"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, nil, updated, fmt.Errorf("解析订单详情响应失败: %w (body=%s)", err, truncate(string(raw), 300))
	}
	if !hasMTopSuccess(decoded.Ret) {
		return nil, decoded.Ret, updated, nil
	}
	result := &OrderDetailResult{Quantity: "1"}
	if utArgs, ok := decoded.Data["utArgs"].(map[string]any); ok {
		result.OrderStatus = mtopString(utArgs["orderStatus"])
	}
	components, _ := decoded.Data["components"].([]any)
	for _, component := range components {
		cm, _ := component.(map[string]any)
		if cm["render"] != "orderInfoVO" {
			continue
		}
		componentData, _ := cm["data"].(map[string]any)
		if itemInfo, ok := componentData["itemInfo"].(map[string]any); ok {
			if value := mtopString(itemInfo["buyAmount"]); value != "" {
				result.Quantity = value
			}
			result.SpecName, result.SpecValue = orderSpecFromItemInfo(itemInfo)
		}
		if priceInfo, ok := componentData["priceInfo"].(map[string]any); ok {
			if amount, ok := priceInfo["amount"].(map[string]any); ok {
				result.Amount = mtopString(amount["value"])
			}
		}
	}
	return result, decoded.Ret, updated, nil
}

type orderSpecPair struct {
	Name  string
	Value string
}

// orderSpecFromItemInfo 兼容订单详情接口已知的规格字段形状。
// 多规格使用稳定的“ / ”分隔名称和值，保留完整组合供后续 SKU 映射核对。
func orderSpecFromItemInfo(itemInfo map[string]any) (string, string) {
	pairs := collectOrderSpecPairs(itemInfo, 0)
	if len(pairs) == 0 {
		return "", ""
	}
	names := make([]string, 0, len(pairs))
	values := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		names = append(names, pair.Name)
		values = append(values, pair.Value)
	}
	return strings.Join(names, " / "), strings.Join(values, " / ")
}

func collectOrderSpecPairs(itemInfo map[string]any, depth int) []orderSpecPair {
	if itemInfo == nil || depth > 4 {
		return nil
	}
	var valueOnly string
	for _, fields := range [][2]string{
		{"specName", "specValue"}, {"spec_name", "spec_value"},
		{"skuName", "skuValue"}, {"sku_name", "sku_value"},
		{"propName", "propValue"}, {"propertyName", "propertyValue"},
		{"name", "value"},
	} {
		name := strings.TrimSpace(mtopString(itemInfo[fields[0]]))
		value := strings.TrimSpace(mtopString(itemInfo[fields[1]]))
		if name != "" && value != "" {
			return []orderSpecPair{{Name: name, Value: value}}
		}
		if valueOnly == "" && value != "" {
			valueOnly = value
		}
	}
	for _, key := range []string{"skuText", "sku_text", "specText", "spec_text", "skuDesc", "skuDescText"} {
		if pairs := splitOrderSpecText(mtopString(itemInfo[key])); len(pairs) > 0 {
			return pairs
		}
	}
	for _, key := range []string{"skuInfo", "sku_info", "specInfo", "spec_info", "sku", "properties", "props"} {
		switch nested := itemInfo[key].(type) {
		case string:
			if pairs := splitOrderSpecText(nested); len(pairs) > 0 {
				return pairs
			}
		case map[string]any:
			if pairs := collectOrderSpecPairs(nested, depth+1); len(pairs) > 0 {
				return pairs
			}
		case []any:
			var pairs []orderSpecPair
			for _, entry := range nested {
				child, ok := entry.(map[string]any)
				if !ok {
					continue
				}
				pairs = appendUniqueOrderSpecPairs(pairs, collectOrderSpecPairs(child, depth+1)...)
			}
			if len(pairs) > 0 {
				return pairs
			}
		}
	}
	if valueOnly != "" {
		return []orderSpecPair{{Value: valueOnly}}
	}
	return nil
}

func appendUniqueOrderSpecPairs(dst []orderSpecPair, values ...orderSpecPair) []orderSpecPair {
	for _, value := range values {
		if value.Value == "" {
			continue
		}
		duplicate := false
		for _, existing := range dst {
			if existing == value {
				duplicate = true
				break
			}
		}
		if !duplicate {
			dst = append(dst, value)
		}
	}
	return dst
}

// splitOrderSpecText 解析单个或多个“规格名:规格值”文本。
// 只有每个分段都能解析时才按斜杠分割，避免把“USB/Type-C”这类规格值误拆。
func splitOrderSpecText(raw string) []orderSpecPair {
	text := strings.TrimSpace(raw)
	if text == "" {
		return nil
	}
	for _, delimiter := range []string{" / ", "；", ";", "\n"} {
		segments := strings.Split(text, delimiter)
		if len(segments) < 2 {
			continue
		}
		pairs := make([]orderSpecPair, 0, len(segments))
		for _, segment := range segments {
			pair, ok := splitSingleOrderSpecText(segment)
			if !ok {
				pairs = nil
				break
			}
			pairs = appendUniqueOrderSpecPairs(pairs, pair)
		}
		if len(pairs) > 0 {
			return pairs
		}
	}
	if pair, ok := splitSingleOrderSpecText(text); ok {
		return []orderSpecPair{pair}
	}
	// 旧版接口偶尔只返回规格值；保留该值，后续可在值唯一时完成映射。
	return []orderSpecPair{{Value: text}}
}

func splitSingleOrderSpecText(raw string) (orderSpecPair, bool) {
	text := strings.TrimSpace(raw)
	for _, separator := range []string{"：", ":", "="} {
		index := strings.Index(text, separator)
		if index <= 0 || index >= len(text)-len(separator) {
			continue
		}
		pair := orderSpecPair{
			Name:  strings.TrimSpace(text[:index]),
			Value: strings.TrimSpace(text[index+len(separator):]),
		}
		return pair, pair.Name != "" && pair.Value != ""
	}
	fields := strings.Fields(text)
	if len(fields) == 2 {
		return orderSpecPair{Name: fields[0], Value: fields[1]}, true
	}
	return orderSpecPair{}, false
}

func buildOrderDetailQuery(t, sign string) string {
	return "jsv=2.7.2&appKey=" + protocol.SignAppKey +
		"&t=" + t + "&sign=" + sign +
		"&v=1.0&type=originaljson&accountSite=xianyu&dataType=json&timeout=20000" +
		"&api=mtop.idle.web.trade.order.detail&sessionOption=AutoLoginOnly&valueType=string"
}
