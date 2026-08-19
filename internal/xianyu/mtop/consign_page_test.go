package mtop

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestFetchConsignAddressContext(t *testing.T) {
	c := &ClientImpl{HTTPClient: &http.Client{Transport: cookieSessionRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		data, _ := url.QueryUnescape(strings.TrimPrefix(string(body), "data="))
		if r.URL.Query().Get("api") != "mtop.taobao.idle.logistics.merchant.consign.page.render" || !strings.Contains(data, `"tradeId":"trade-1"`) {
			t.Fatalf("request mismatch: %s %s", r.URL, data)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"ret":["SUCCESS::调用成功"],"data":"{\"module\":{\"addressId\":\"25152147714\"}}"}`))}, nil
	})}}
	id, _, _, err := c.FetchConsignAddressContext(context.Background(), "_m_h5_tk=token_1; _m_h5_tk_enc=x", "trade-1")
	if err != nil || id != 25152147714 {
		t.Fatalf("id=%d err=%v", id, err)
	}
}

func TestCollectConsignAddressRejectsAmbiguousIDs(t *testing.T) {
	ids := map[int64]struct{}{}
	collectAddressIDs([]byte(`{"a":{"addressId":1},"b":{"address_id":"2"}}`), ids)
	if len(ids) != 2 {
		t.Fatalf("ids=%v", ids)
	}
}
