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

const ConsignPageAPI = "https://h5api.m.goofish.com/h5/mtop.taobao.idle.logistics.merchant.consign.page.render/1.0/"

// FetchConsignAddressContext renders the shipping page for one trade and
// extracts its actual consign addressId. This is intentionally separate from
// merchant.delivery.address.list.query, whose contactId is not accepted by
// the consign API.
func (c *ClientImpl) FetchConsignAddressContext(ctx context.Context, cookies, tradeID string) (int64, []string, string, error) {
	payload, _ := json.Marshal(map[string]string{"tradeId": strings.TrimSpace(tradeID)})
	data := string(payload)
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	signing, requestCookies := mtopRequestCookies(ctx, cookies, "https://seller.goofish.com/", ConsignPageAPI)
	query := url.Values{
		"jsv": {"2.7.2"}, "appKey": {protocol.SignAppKey}, "t": {timestamp},
		"sign": {protocol.GenerateSign(timestamp, protocol.SignToken(signing), data)},
		"v":    {"1.0"}, "type": {"json"}, "accountSite": {"xianyu"},
		"dataType": {"json"}, "timeout": {"20000"},
		"api":       {"mtop.taobao.idle.logistics.merchant.consign.page.render"},
		"valueType": {"string"}, "sessionOption": {"AutoLoginOnly"},
		"spm_cnt": {"a21107h.42826273.0.0"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ConsignPageAPI+"?"+query.Encode(), strings.NewReader("data="+url.QueryEscape(data)))
	if err != nil {
		return 0, nil, cookies, err
	}
	setCommonHeaders(req, requestCookies)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Origin", "https://seller.goofish.com")
	req.Header.Set("Referer", "https://seller.goofish.com/?site=COMMONPRO")
	req.Header.Set("idle_site_biz_code", "COMMONPRO")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return 0, nil, cookies, fmt.Errorf("读取闲鱼发货页失败: %w", err)
	}
	defer resp.Body.Close()
	updated := absorbMTopResponseCookies(ctx, cookies, resp)
	body, err := readMTopBody(resp)
	if err != nil {
		return 0, nil, updated, err
	}
	var decoded struct {
		Ret  []string        `json:"ret"`
		Data json.RawMessage `json:"data"`
	}
	if err = json.Unmarshal(body, &decoded); err != nil {
		return 0, nil, updated, fmt.Errorf("解析闲鱼发货页失败: %w", err)
	}
	if !hasMTopSuccess(decoded.Ret) {
		return 0, decoded.Ret, updated, fmt.Errorf("闲鱼发货页返回失败: %s", strings.Join(decoded.Ret, "; "))
	}
	ids := map[int64]struct{}{}
	collectAddressIDs(decoded.Data, ids)
	if len(ids) != 1 {
		return 0, decoded.Ret, updated, fmt.Errorf("闲鱼发货页 addressId 数量异常: %d", len(ids))
	}
	for id := range ids {
		return id, decoded.Ret, updated, nil
	}
	return 0, decoded.Ret, updated, fmt.Errorf("闲鱼发货页缺少 addressId")
}

func collectAddressIDs(raw json.RawMessage, ids map[int64]struct{}) {
	var value any
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return
	}
	collectAddressIDValue(value, ids)
}

func collectAddressIDValue(value any, ids map[int64]struct{}) {
	switch typed := value.(type) {
	case string:
		trimmed := strings.TrimSpace(typed)
		if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
			collectAddressIDs(json.RawMessage(trimmed), ids)
		}
	case []any:
		for _, item := range typed {
			collectAddressIDValue(item, ids)
		}
	case map[string]any:
		for key, item := range typed {
			if strings.EqualFold(key, "addressId") || strings.EqualFold(key, "address_id") {
				var id int64
				switch candidate := item.(type) {
				case string:
					id, _ = strconv.ParseInt(candidate, 10, 64)
				case float64:
					id = int64(candidate)
				}
				if id > 0 {
					ids[id] = struct{}{}
				}
				continue
			}
			collectAddressIDValue(item, ids)
		}
	}
}
