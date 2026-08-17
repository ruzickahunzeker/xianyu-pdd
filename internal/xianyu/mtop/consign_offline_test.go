package mtop

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestConsignOfflineRequest(t *testing.T) {
	c := &ClientImpl{HTTPClient: &http.Client{Transport: cookieSessionRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("api") != "mtop.taobao.idle.logistics.merchant.consign.offline" || r.Header.Get("idle_site_biz_code") != "COMMONPRO" {
			t.Fatalf("request mismatch: %s %#v", r.URL, r.Header)
		}
		body, _ := io.ReadAll(r.Body)
		data, _ := url.QueryUnescape(strings.TrimPrefix(string(body), "data="))
		for _, want := range []string{`"orderInfos":"[{\"tradeId\":\"trade-1\"}]"`, `"mailNo":"mail-1"`, `"cpCode":"YUNDA"`, `"addressId":251`} {
			if !strings.Contains(data, want) {
				t.Fatalf("missing %s in %s", want, data)
			}
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"ret":["SUCCESS::调用成功"],"data":{}}`))}, nil
	})}}
	ok, _, _, err := c.ConsignOfflineContext(context.Background(), "_m_h5_tk=token_1; _m_h5_tk_enc=x", ConsignOfflineRequest{TradeID: "trade-1", MailNo: "mail-1", CPCode: "YUNDA", AddressID: 251})
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}
