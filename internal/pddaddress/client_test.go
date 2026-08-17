package pddaddress

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientUpdate(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/proxy/api/api/origenes/address_info/address-1" || r.URL.Query().Get("pdduid") != "user-1" {
			t.Errorf("unexpected URL %s", r.URL.String())
		}
		if r.Header.Get("Cookie") != "secret-cookie" || r.Header.Get("anti-content") != "" {
			t.Error("credentials missing")
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()
	client := NewClient(Config{BaseURL: server.URL, PDDUID: "user-1", AddressID: "address-1", Cookie: "secret-cookie"})
	result, err := client.Update(context.Background(), UpdateRequest{Name: "张三", Mobile: "13800138000", Match: Match{ProvinceID: 1, CityID: 2, DistrictID: 3, Address: "测试路1号"}})
	if err != nil || result.Status != "applied" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if received["mobile"] != "13800138000" || received["check_region"] != true {
		t.Fatalf("payload=%v", received)
	}
}

func TestClientDoesNotLeakCredentialsInError(t *testing.T) {
	client := NewClient(Config{BaseURL: "http://127.0.0.1:1", PDDUID: "uid", AddressID: "aid", Cookie: "secret-cookie"})
	_, err := client.Update(context.Background(), UpdateRequest{})
	if err == nil || strings.Contains(err.Error(), "secret-") {
		t.Fatalf("unsafe error: %v", err)
	}
}

func TestPDDUIDFromCookie(t *testing.T) {
	if got := PDDUIDFromCookie("api_uid=x; pdd_user_id=6670459375039; token=y"); got != "6670459375039" {
		t.Fatalf("got %q", got)
	}
	if got := PDDUIDFromCookie("api_uid=x"); got != "" {
		t.Fatalf("unexpected %q", got)
	}
}
