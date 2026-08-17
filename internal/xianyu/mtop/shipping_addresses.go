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

const (
	ShippingAddressListAPI    = "https://h5api.m.goofish.com/h5/mtop.alibaba.idle.seller.platform.merchant.delivery.address.list.query/1.0/"
	shippingAddressListAppKey = "12574478"
)

type ShippingAddress struct {
	ContactID     int64  `json:"contactId"`
	AreaID        int64  `json:"areaId"`
	ContactName   string `json:"contactName"`
	MobilePhone   string `json:"mobilePhone"`
	ProvinceName  string `json:"provinceName"`
	CityName      string `json:"cityName"`
	DistrictName  string `json:"districtName"`
	DetailAddress string `json:"detailAddress"`
	DefaultAddr   bool   `json:"defaultAddr"`
}

func (c *ClientImpl) FetchShippingAddressesContext(ctx context.Context, cookies string) ([]ShippingAddress, []string, string, error) {
	current := cookies
	for attempt := 0; attempt < 4; attempt++ {
		addresses, ret, updated, err := c.fetchShippingAddressesOnce(ctx, current)
		if updated != "" {
			current = updated
		}
		if err != nil {
			return nil, ret, current, err
		}
		if hasMTopSuccess(ret) || !isTokenExpiredRet(ret) {
			return addresses, ret, current, nil
		}
		if attempt < 3 {
			refreshed, refreshErr := c.RefreshTokenContext(ctx, current)
			if refreshErr != nil {
				return nil, ret, current, refreshErr
			}
			if refreshed.UpdatedCookies != "" {
				current = refreshed.UpdatedCookies
			}
			if err = sleepCtx(ctx, MTopRetryGap); err != nil {
				return nil, ret, current, err
			}
		}
	}
	return nil, nil, current, nil
}

func (c *ClientImpl) fetchShippingAddressesOnce(ctx context.Context, cookies string) ([]ShippingAddress, []string, string, error) {
	data, timestamp := `{}`, strconv.FormatInt(time.Now().UnixMilli(), 10)
	signing, requestCookies := mtopRequestCookies(ctx, cookies, "https://seller.goofish.com/", ShippingAddressListAPI)
	sign := protocol.GenerateSignWithAppKey(timestamp, protocol.SignToken(signing), shippingAddressListAppKey, data)
	q := url.Values{"jsv": {"2.7.2"}, "appKey": {shippingAddressListAppKey}, "t": {timestamp}, "sign": {sign}, "v": {"1.0"}, "type": {"originaljson"}, "accountSite": {"xianyu"}, "dataType": {"json"}, "timeout": {"20000"}, "api": {"mtop.alibaba.idle.seller.platform.merchant.delivery.address.list.query"}, "sessionOption": {"AutoLoginOnly"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ShippingAddressListAPI+"?"+q.Encode(), strings.NewReader("data="+url.QueryEscape(data)))
	if err != nil {
		return nil, nil, cookies, err
	}
	setCommonHeaders(req, requestCookies)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Origin", "https://seller.goofish.com")
	req.Header.Set("Referer", "https://seller.goofish.com/?site=COMMONPRO")
	req.Header.Set("idle_site_biz_code", "COMMONPRO")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, nil, cookies, fmt.Errorf("查询卖家发货地址失败: %w", err)
	}
	defer resp.Body.Close()
	updated := absorbMTopResponseCookies(ctx, cookies, resp)
	body, err := readMTopBody(resp)
	if err != nil {
		return nil, nil, updated, err
	}
	var decoded struct {
		Ret  []string `json:"ret"`
		Data struct {
			Code string            `json:"code"`
			Data []ShippingAddress `json:"data"`
			Msg  string            `json:"msg"`
		} `json:"data"`
	}
	if err = json.Unmarshal(body, &decoded); err != nil {
		return nil, nil, updated, fmt.Errorf("解析卖家发货地址失败: %w", err)
	}
	return decoded.Data.Data, decoded.Ret, updated, nil
}
