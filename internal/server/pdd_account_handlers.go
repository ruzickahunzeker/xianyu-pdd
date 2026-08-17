package server

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"xianyu-go/internal/auth"
	"xianyu-go/internal/pddaddress"
)

const defaultPDDUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36"

func (s *Server) mountPDDAccountAdmin(r interface {
	Get(string, http.HandlerFunc)
	Put(string, http.HandlerFunc)
	Delete(string, http.HandlerFunc)
	Post(string, http.HandlerFunc)
}) {
	r.Get("/api/pdd/account", s.getPDDAccount)
	r.Put("/api/pdd/account", s.savePDDAccount)
	r.Delete("/api/pdd/account", s.deletePDDAccount)
	r.Post("/api/pdd/account/verify", s.verifyPDDAccount)
}

func pddAccountJSON(accountID, name, pddUID, addressID, userAgent, status, lastError string, enabled bool, verifiedAt int64, configured bool) map[string]any {
	return map[string]any{"id": accountID, "name": name, "pdd_uid": pddUID, "default_address_id": addressID, "user_agent": userAgent, "enabled": enabled, "credential_status": status, "last_verified_at": verifiedAt, "last_error": lastError, "configured": configured, "cookie_configured": configured}
}

func (s *Server) getPDDAccount(w http.ResponseWriter, r *http.Request) {
	userID := auth.SessionFromContext(r.Context()).UserID
	a, err := s.Store.PDDAccounts.Default(r.Context(), userID)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusOK, pddAccountJSON("", "拼多多主账号", "", "", defaultPDDUserAgent, "unconfigured", "", true, 0, false))
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取拼多多账号失败")
		return
	}
	writeJSON(w, http.StatusOK, pddAccountJSON(a.ID, a.Name, a.PDDUID, a.DefaultAddressID, a.UserAgent, a.CredentialStatus, a.LastError, a.Enabled, a.LastVerifiedAt, a.Cookie != ""))
}

func (s *Server) savePDDAccount(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name             string `json:"name"`
		Cookie           string `json:"cookie"`
		DefaultAddressID string `json:"default_address_id"`
		UserAgent        string `json:"user_agent"`
		Enabled          *bool  `json:"enabled"`
	}
	if decodeJSON(r, &in) != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	userID := auth.SessionFromContext(r.Context()).UserID
	existing, _ := s.Store.PDDAccounts.Default(r.Context(), userID)
	cookie := strings.TrimSpace(in.Cookie)
	if cookie == "" && existing != nil {
		cookie = existing.Cookie
	}
	pddUID := pddaddress.PDDUIDFromCookie(cookie)
	if pddUID == "" {
		writeErr(w, http.StatusUnprocessableEntity, "Cookie 中缺少 pdd_user_id")
		return
	}
	addressID := strings.TrimSpace(in.DefaultAddressID)
	if addressID == "" {
		writeErr(w, http.StatusUnprocessableEntity, "默认地址 ID 不能为空")
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = "拼多多主账号"
	}
	if len(name) > 100 || len(addressID) > 191 {
		writeErr(w, http.StatusBadRequest, "账号名称或地址 ID 过长")
		return
	}
	ua := strings.TrimSpace(in.UserAgent)
	if ua == "" {
		ua = defaultPDDUserAgent
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	a, err := s.Store.PDDAccounts.SaveSingle(r.Context(), userID, name, cookie, pddUID, addressID, ua, enabled)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "保存拼多多账号失败")
		return
	}
	writeJSON(w, http.StatusOK, pddAccountJSON(a.ID, a.Name, a.PDDUID, a.DefaultAddressID, a.UserAgent, a.CredentialStatus, a.LastError, a.Enabled, a.LastVerifiedAt, true))
}

func (s *Server) deletePDDAccount(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.PDDAccounts.DeleteDefault(r.Context(), auth.SessionFromContext(r.Context()).UserID); err != nil {
		writeErr(w, 500, "清除拼多多账号失败")
		return
	}
	writeJSON(w, 200, map[string]any{"success": true})
}

func (s *Server) verifyPDDAccount(w http.ResponseWriter, r *http.Request) {
	userID := auth.SessionFromContext(r.Context()).UserID
	a, err := s.Store.PDDAccounts.Default(r.Context(), userID)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "尚未配置拼多多账号")
		return
	}
	status, lastError := "valid", ""
	if !a.Enabled {
		status, lastError = "invalid", "账号已禁用"
	} else if pddaddress.PDDUIDFromCookie(a.Cookie) == "" {
		status, lastError = "invalid", "Cookie 中缺少 pdd_user_id"
	} else if strings.TrimSpace(a.DefaultAddressID) == "" {
		status, lastError = "invalid", "默认地址 ID 为空"
	}
	_ = s.Store.PDDAccounts.MarkVerified(r.Context(), a.ID, userID, status, lastError)
	if status != "valid" {
		writeErr(w, http.StatusUnprocessableEntity, lastError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "credential_status": status, "pdd_uid": a.PDDUID, "message": "配置格式有效；未修改拼多多地址"})
}
