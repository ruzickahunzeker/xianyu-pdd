package mtop

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestFetchShippingAddressesRequest(t *testing.T) {
	c := &ClientImpl{HTTPClient: &http.Client{Transport: cookieSessionRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("api") != "mtop.alibaba.idle.seller.platform.merchant.delivery.address.list.query" || r.URL.Query().Get("appKey") != shippingAddressListAppKey || r.Header.Get("idle_site_biz_code") != "COMMONPRO" {
			t.Fatalf("request mismatch: %s %#v", r.URL, r.Header)
		}
		body, _ := io.ReadAll(r.Body)
		data, _ := url.QueryUnescape(strings.TrimPrefix(string(body), "data="))
		if data != `{}` {
			t.Fatalf("data=%q", data)
		}
		response := `{"ret":["SUCCESS::调用成功"],"data":{"code":"success","data":[{"areaId":440304,"contactId":3861442261,"contactName":"杨先生","defaultAddr":true,"detailAddress":"华强北市场","districtName":"福田区","mobilePhone":"17706603201","provinceName":"广东省","cityName":"深圳市"}]}}`
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(response))}, nil
	})}}
	addresses, _, _, err := c.FetchShippingAddressesContext(context.Background(), "_m_h5_tk=token_1; _m_h5_tk_enc=x")
	if err != nil || len(addresses) != 1 || addresses[0].ContactID != 3861442261 || !addresses[0].DefaultAddr {
		t.Fatalf("addresses=%#v err=%v", addresses, err)
	}
}
