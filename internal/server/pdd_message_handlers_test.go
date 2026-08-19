package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPDDMessageManualConfirmationWorkflow(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "pdd-message-handler-test-key")
	server, store, cleanup := newTestServer(t)
	defer cleanup()
	admin, err := store.Users.GetByUsername(t.Context(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	account, err := store.PDDAccounts.SaveSingle(t.Context(), admin.ID, "主账号", "api_uid=1; webp=1", "uin", "609", "test-agent", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB.Exec(`INSERT INTO pdd_products(goods_id,mall_sn,final_url,title,images_json,first_collected_at,last_collected_at) VALUES('goods-1','mall-1','https://mobile.pinduoduo.com/goods.html?goods_id=goods-1','商品','[]',1,1)`); err != nil {
		t.Fatal(err)
	}
	cookie := loginHelper(t, server.Router())
	request := func(method, path string, body any, key string) *httptest.ResponseRecorder {
		var raw []byte
		if body != nil {
			raw, _ = json.Marshal(body)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
		req.AddCookie(cookie)
		req.Header.Set("Content-Type", "application/json")
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		rec := httptest.NewRecorder()
		server.Router().ServeHTTP(rec, req)
		return rec
	}
	input := map[string]any{"pdd_account_id": account.ID, "goods_id": "goods-1", "task_type": "custom_message", "message": "您好，测试消息", "send_mode": "manual_confirm"}
	created := request(http.MethodPost, "/api/pdd/messages", input, "custom:goods-1:1")
	if created.Code != http.StatusCreated || !strings.Contains(created.Body.String(), `"mall_sn":"mall-1"`) {
		t.Fatalf("create=%d %s", created.Code, created.Body.String())
	}
	replayed := request(http.MethodPost, "/api/pdd/messages", input, "custom:goods-1:1")
	if replayed.Code != http.StatusOK {
		t.Fatalf("replay=%d %s", replayed.Code, replayed.Body.String())
	}
	claimed := request(http.MethodPost, "/api/pdd/messages/claim", map[string]any{"worker_id": "test", "lease_seconds": 180}, "")
	if claimed.Code != http.StatusOK || !strings.Contains(claimed.Body.String(), `"action":"preflight"`) {
		t.Fatalf("claim=%d %s", claimed.Code, claimed.Body.String())
	}
	var task map[string]any
	_ = json.Unmarshal(claimed.Body.Bytes(), &task)
	id, _ := task["id"].(string)
	token, _ := task["lease_token"].(string)
	preflight := request(http.MethodPost, "/api/pdd/messages/"+id+"/preflight", map[string]any{"lease_token": token}, "")
	if preflight.Code != http.StatusOK {
		t.Fatalf("preflight=%d %s", preflight.Code, preflight.Body.String())
	}
	blocked := request(http.MethodPost, "/api/pdd/messages/"+id+"/confirm", map[string]any{}, "")
	if blocked.Code != http.StatusConflict {
		t.Fatalf("unsafe confirm=%d %s", blocked.Code, blocked.Body.String())
	}
	if err = store.Settings.Set(t.Context(), "pdd_message_real_send_enabled", "true"); err != nil {
		t.Fatal(err)
	}
	confirmed := request(http.MethodPost, "/api/pdd/messages/"+id+"/confirm", map[string]any{}, "")
	if confirmed.Code != http.StatusOK {
		t.Fatalf("confirm=%d %s", confirmed.Code, confirmed.Body.String())
	}
	claimed = request(http.MethodPost, "/api/pdd/messages/claim", map[string]any{"worker_id": "test", "lease_seconds": 180}, "")
	if claimed.Code != http.StatusOK || !strings.Contains(claimed.Body.String(), `"action":"send"`) {
		t.Fatalf("send claim=%d %s", claimed.Code, claimed.Body.String())
	}
}
