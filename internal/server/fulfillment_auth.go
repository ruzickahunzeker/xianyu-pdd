package server

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
)

func newFulfillmentAPIKey() (string, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return "xyf_" + base64.RawURLEncoding.EncodeToString(random), nil
}

func (s *Server) fulfillmentKeyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if !strings.HasPrefix(token, "xyf_") {
			writeErr(w, http.StatusUnauthorized, "履约密钥无效")
			return
		}
		var keyID string
		var userID int64
		if err := s.Store.DB.QueryRowContext(r.Context(), `SELECT id,user_id FROM fulfillment_api_keys WHERE token_hash=? AND enabled=1`, tokenDigest(token)).Scan(&keyID, &userID); err != nil {
			writeErr(w, http.StatusUnauthorized, "履约密钥无效")
			return
		}
		user, err := s.Store.Users.GetByID(r.Context(), userID)
		if err != nil || user == nil {
			writeErr(w, http.StatusUnauthorized, "履约密钥所属用户不可用")
			return
		}
		_, _ = s.Store.DB.ExecContext(r.Context(), `UPDATE fulfillment_api_keys SET last_used_at=? WHERE id=?`, time.Now().Unix(), keyID)
		session := &db.Session{UserID: user.ID, Username: user.Username, IsAdmin: user.IsAdmin}
		next.ServeHTTP(w, r.WithContext(auth.WithSession(r.Context(), session)))
	})
}

// fulfillmentAccessMiddleware allows the web workbench to use the same API with
// an administrator session while external scripts continue to use a dedicated key.
func (s *Server) fulfillmentAccessMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if session := auth.SessionFromContext(r.Context()); session != nil {
			if !session.IsAdmin {
				writeErr(w, http.StatusForbidden, "需要管理员权限")
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		s.fulfillmentKeyMiddleware(next).ServeHTTP(w, r)
	})
}

func (s *Server) mountFulfillmentKeyAdmin(r chi.Router) {
	r.Get("/api/fulfillment/keys", s.listFulfillmentKeys)
	r.Post("/api/fulfillment/keys", s.createFulfillmentKey)
	r.Delete("/api/fulfillment/keys/{key_id}", s.deleteFulfillmentKey)
}

func (s *Server) createFulfillmentKey(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name string `json:"name"`
	}
	if decodeJSON(r, &input) != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len([]rune(input.Name)) > 100 {
		writeErr(w, http.StatusBadRequest, "密钥名称不能为空且不能超过100个字符")
		return
	}
	token, err := newFulfillmentAPIKey()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "生成履约密钥失败")
		return
	}
	id, now := uuid.NewString(), time.Now().Unix()
	userID := auth.SessionFromContext(r.Context()).UserID
	if _, err := s.Store.DB.ExecContext(r.Context(), `INSERT INTO fulfillment_api_keys(id,user_id,name,token_hash,enabled,last_used_at,created_at) VALUES(?,?,?,?,1,0,?)`, id, userID, input.Name, tokenDigest(token), now); err != nil {
		writeErr(w, http.StatusInternalServerError, "保存履约密钥失败")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "name": input.Name, "api_key": token, "created_at": now})
}

func (s *Server) listFulfillmentKeys(w http.ResponseWriter, r *http.Request) {
	userID := auth.SessionFromContext(r.Context()).UserID
	rows, err := s.Store.DB.QueryContext(r.Context(), `SELECT id,name,enabled,last_used_at,created_at FROM fulfillment_api_keys WHERE user_id=? ORDER BY created_at DESC`, userID)
	if err != nil {
		writeErr(w, 500, "查询履约密钥失败")
		return
	}
	defer rows.Close()
	result := []map[string]any{}
	for rows.Next() {
		var id, name string
		var enabled int
		var lastUsed, created int64
		if rows.Scan(&id, &name, &enabled, &lastUsed, &created) == nil {
			result = append(result, map[string]any{"id": id, "name": name, "enabled": enabled != 0, "last_used_at": lastUsed, "created_at": created})
		}
	}
	writeJSON(w, 200, result)
}

func (s *Server) deleteFulfillmentKey(w http.ResponseWriter, r *http.Request) {
	userID := auth.SessionFromContext(r.Context()).UserID
	result, err := s.Store.DB.ExecContext(r.Context(), `DELETE FROM fulfillment_api_keys WHERE id=? AND user_id=?`, chi.URLParam(r, "key_id"), userID)
	if err != nil {
		writeErr(w, 500, "吊销履约密钥失败")
		return
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		writeErr(w, 404, "履约密钥不存在")
		return
	}
	writeJSON(w, 200, map[string]any{"success": true})
}
