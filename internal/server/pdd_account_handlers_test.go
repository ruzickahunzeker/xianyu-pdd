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
	saved := request(http.MethodPut, "/api/pdd/account", `{"name":"主账号","cookie":"`+cookie+`","default_address_id":"60984097534","enabled":true}`)
	if saved.Code != http.StatusOK || strings.Contains(saved.Body.String(), "do-not-return") || !strings.Contains(saved.Body.String(), `"pdd_uid":"6670459375039"`) {
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
