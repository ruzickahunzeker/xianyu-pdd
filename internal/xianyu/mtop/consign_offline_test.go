package mtop

import (
	"context"
	"encoding/json"
	"errors"
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
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"ret":["SUCCESS::调用成功"],"data":{"success":true}}`))}, nil
	})}}
	ok, _, _, _, err := c.ConsignOfflineContext(context.Background(), "_m_h5_tk=token_1; _m_h5_tk_enc=x", ConsignOfflineRequest{TradeID: "trade-1", MailNo: "mail-1", CPCode: "YUNDA", AddressID: 251})
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func TestConsignOfflineDoesNotTreatEmptyBusinessDataAsSuccess(t *testing.T) {
	c := &ClientImpl{HTTPClient: &http.Client{Transport: cookieSessionRoundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"ret":["SUCCESS::调用成功"],"data":{}}`))}, nil
	})}}
	ok, _, data, _, err := c.ConsignOfflineContext(context.Background(), "_m_h5_tk=token_1; _m_h5_tk_enc=x", ConsignOfflineRequest{TradeID: "trade-1", MailNo: "mail-1", CPCode: "STO", AddressID: 251})
	if ok || !errors.Is(err, ErrConsignResultUnverified) || string(data) != `{}` {
		t.Fatalf("empty business result must be unverified: ok=%v data=%s err=%v", ok, data, err)
	}
}

func TestConsignBusinessSuccessCounts(t *testing.T) {
	for _, test := range []struct {
		raw   string
		want  bool
		known bool
	}{
		{`{"successNum":1,"failNum":0,"successOrderIds":["trade-1"]}`, true, true},
		{`{"successNum":0,"failNum":1,"failOrderInfos":[{"errorCode":"ADDRESS_NOT_EXIST"}]}`, false, true},
		{`{}`, false, false},
	} {
		got, known := consignBusinessSuccess(json.RawMessage(test.raw))
		if got != test.want || known != test.known {
			t.Fatalf("raw=%s got=%v known=%v", test.raw, got, known)
		}
	}
}
