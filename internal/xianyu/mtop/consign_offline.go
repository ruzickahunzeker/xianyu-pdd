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

const ConsignOfflineAPI = "https://h5api.m.goofish.com/h5/mtop.taobao.idle.logistics.merchant.consign.offline/1.0/"

type ConsignOfflineRequest struct {
	TradeID, MailNo, CPCode string
	AddressID               int64
}

func (c *ClientImpl) ConsignOfflineContext(ctx context.Context, cookies string, input ConsignOfflineRequest) (bool, []string, string, error) {
	current := cookies
	for attempt := 0; attempt < 4; attempt++ {
		ok, ret, updated, err := c.consignOfflineOnce(ctx, current, input)
		if updated != "" {
			current = updated
		}
		if err != nil {
			return false, ret, current, err
		}
		if ok || !isTokenExpiredRet(ret) {
			return ok, ret, current, nil
		}
		if attempt < 3 {
			refreshed, e := c.RefreshTokenContext(ctx, current)
			if e != nil {
				return false, ret, current, e
			}
			if refreshed.UpdatedCookies != "" {
				current = refreshed.UpdatedCookies
			}
			if e = sleepCtx(ctx, MTopRetryGap); e != nil {
				return false, ret, current, e
			}
		}
	}
	return false, nil, current, nil
}

func (c *ClientImpl) consignOfflineOnce(ctx context.Context, cookies string, input ConsignOfflineRequest) (bool, []string, string, error) {
	orderInfos, _ := json.Marshal([]map[string]string{{"tradeId": input.TradeID}})
	payload := map[string]any{"orderInfos": string(orderInfos), "mailNo": input.MailNo, "cpCode": input.CPCode, "addressId": input.AddressID}
	raw, _ := json.Marshal(payload)
	data := string(raw)
	t := strconv.FormatInt(time.Now().UnixMilli(), 10)
	endpoint := ConsignOfflineAPI
	signing, requestCookies := mtopRequestCookies(ctx, cookies, "https://seller.goofish.com/", endpoint)
	sign := protocol.GenerateSign(t, protocol.SignToken(signing), data)
	q := url.Values{"jsv": {"2.7.2"}, "appKey": {protocol.SignAppKey}, "t": {t}, "sign": {sign}, "v": {"1.0"}, "type": {"originaljson"}, "accountSite": {"xianyu"}, "dataType": {"json"}, "timeout": {"20000"}, "api": {"mtop.taobao.idle.logistics.merchant.consign.offline"}, "sessionOption": {"AutoLoginOnly"}, "spm_cnt": {"a21107h.42826273.0.0"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"?"+q.Encode(), strings.NewReader("data="+url.QueryEscape(data)))
	if err != nil {
		return false, nil, cookies, err
	}
	setCommonHeaders(req, requestCookies)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Origin", "https://seller.goofish.com")
	req.Header.Set("Referer", "https://seller.goofish.com/?site=COMMONPRO")
	req.Header.Set("idle_site_biz_code", "COMMONPRO")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return false, nil, cookies, fmt.Errorf("实物发货请求失败: %w", err)
	}
	defer resp.Body.Close()
	updated := absorbMTopResponseCookies(ctx, cookies, resp)
	body, err := readMTopBody(resp)
	if err != nil {
		return false, nil, updated, err
	}
	var decoded struct {
		Ret  []string `json:"ret"`
		Data any      `json:"data"`
	}
	if err = json.Unmarshal(body, &decoded); err != nil {
		return false, nil, updated, fmt.Errorf("解析实物发货响应失败: %w", err)
	}
	return hasMTopSuccess(decoded.Ret), decoded.Ret, updated, nil
}
