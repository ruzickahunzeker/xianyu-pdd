package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPDDAccountSettingsDoNotExposeCookie(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "pdd-handler-test-key")
	server, _, cleanup := newTestServer(t)
	defer cleanup()
	router := server.Router()
	session := loginHelper(t, router)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.AddCookie(session)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	cookie := "token=x; pdd_user_id=6670459375039; secret=do-not-return"
	saved := request(http.MethodPut, "/api/pdd/account", `{"name":"主账号","site":"pinduoduo","cookie":"`+cookie+`","default_address_id":"60984097534","enabled":true}`)
	if saved.Code != http.StatusOK || strings.Contains(saved.Body.String(), "do-not-return") || !strings.Contains(saved.Body.String(), `"pdd_uid":"6670459375039"`) || !strings.Contains(saved.Body.String(), `"base_url":"https://mobile.pinduoduo.com"`) {
		t.Fatalf("save=%d %s", saved.Code, saved.Body.String())
	}
	got := request(http.MethodGet, "/api/pdd/account", "")
	if got.Code != http.StatusOK || strings.Contains(got.Body.String(), "do-not-return") || !strings.Contains(got.Body.String(), `"cookie_configured":true`) {
		t.Fatalf("get=%d %s", got.Code, got.Body.String())
	}
	verified := request(http.MethodPost, "/api/pdd/account/verify", `{}`)
	if verified.Code != http.StatusOK || !strings.Contains(verified.Body.String(), `"credential_status":"valid"`) {
		t.Fatalf("verify=%d %s", verified.Code, verified.Body.String())
	}
}

func TestPDDAccountSiteSwitchRequiresNewCookie(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "pdd-handler-site-key")
	server, _, cleanup := newTestServer(t)
	defer cleanup()
	router := server.Router()
	session := loginHelper(t, router)
	request := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "/api/pdd/account", strings.NewReader(body))
		req.AddCookie(session)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	if rec := request(`{"site":"pinduoduo","cookie":"pdd_user_id=1","default_address_id":"10"}`); rec.Code != http.StatusOK {
		t.Fatalf("initial save=%d %s", rec.Code, rec.Body.String())
	}
	if rec := request(`{"site":"yangkeduo","default_address_id":"20"}`); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("site switch without cookie=%d %s", rec.Code, rec.Body.String())
	}
	if rec := request(`{"site":"yangkeduo","cookie":"pdd_user_id=1","default_address_id":"20"}`); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"cookie_domain":".yangkeduo.com"`) {
		t.Fatalf("site switch=%d %s", rec.Code, rec.Body.String())
	}
}

func TestPDDAccountRejectsCookieWithoutUserID(t *testing.T) {
	server, _, cleanup := newTestServer(t)
	defer cleanup()
	router := server.Router()
	req := httptest.NewRequest(http.MethodPut, "/api/pdd/account", strings.NewReader(`{"cookie":"token=x","default_address_id":"1"}`))
	req.AddCookie(loginHelper(t, router))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPDDAccountRuntimeRecordsRiskAndPausesAccount(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "pdd-runtime-test-key")
	server, _, cleanup := newTestServer(t)
	defer cleanup()
	router := server.Router()
	session := loginHelper(t, router)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.AddCookie(session)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	saved := request(http.MethodPut, "/api/pdd/account", `{"site":"pinduoduo","cookie":"pdd_user_id=123","default_address_id":"609","enabled":true}`)
	if saved.Code != http.StatusOK {
		t.Fatalf("save=%d %s", saved.Code, saved.Body.String())
	}
	account, _ := server.Store.PDDAccounts.Default(t.Context(), 1)
	event := request(http.MethodPost, "/api/fulfillment/pdd-account-events", `{"account_id":"`+account.ID+`","site":"pinduoduo","operation":"purchase","status":"risk","error_type":"captcha_required","message":"captcha_required"}`)
	if event.Code != http.StatusOK {
		t.Fatalf("event=%d %s", event.Code, event.Body.String())
	}
	runtime := request(http.MethodGet, "/api/pdd/account/runtime", "")
	if runtime.Code != http.StatusOK || !strings.Contains(runtime.Body.String(), `"status":"risk_blocked"`) || !strings.Contains(runtime.Body.String(), `"consecutive_failures":1`) {
		t.Fatalf("runtime=%d %s", runtime.Code, runtime.Body.String())
	}
	account, _ = server.Store.PDDAccounts.Default(t.Context(), 1)
	if account.Enabled {
		t.Fatal("risk event must pause account")
	}
	resumed := request(http.MethodPost, "/api/pdd/account/resume", `{}`)
	if resumed.Code != http.StatusOK {
		t.Fatalf("resume=%d %s", resumed.Code, resumed.Body.String())
	}
}
