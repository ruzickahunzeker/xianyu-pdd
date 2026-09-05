package server

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"xianyu-go/internal/auth"
)

var pddRiskErrors = map[string]bool{"login_required": true, "captcha_required": true, "access_limited": true, "account_mismatch": true}

func (s *Server) recordPDDAccountEvent(w http.ResponseWriter, r *http.Request) {
	uid := auth.SessionFromContext(r.Context()).UserID
	var in struct {
		AccountID string `json:"account_id"`
		Site      string `json:"site"`
		Operation string `json:"operation"`
		Status    string `json:"status"`
		ErrorType string `json:"error_type"`
		Message   string `json:"message"`
		TaskID    string `json:"task_id"`
	}
	if decodeJSON(r, &in) != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	in.AccountID, in.Site, in.Operation, in.Status = strings.TrimSpace(in.AccountID), strings.TrimSpace(in.Site), strings.TrimSpace(in.Operation), strings.TrimSpace(in.Status)
	if in.AccountID == "" || in.Operation == "" || (in.Status != "success" && in.Status != "failed" && in.Status != "risk") {
		writeErr(w, 422, "拼多多事件参数无效")
		return
	}
	var count int
	if s.Store.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM pdd_accounts WHERE id=? AND user_id=?`, in.AccountID, uid).Scan(&count) != nil || count != 1 {
		writeErr(w, 404, "拼多多账号不存在")
		return
	}
	if len(in.Message) > 500 {
		in.Message = in.Message[:500]
	}
	now := time.Now().Unix()
	if _, err := s.Store.DB.ExecContext(r.Context(), `INSERT INTO pdd_account_events(user_id,pdd_account_id,site,operation,status,error_type,message,task_id,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, uid, in.AccountID, in.Site, in.Operation, in.Status, strings.TrimSpace(in.ErrorType), strings.TrimSpace(in.Message), strings.TrimSpace(in.TaskID), now); err != nil {
		writeErr(w, 500, "记录拼多多运行事件失败")
		return
	}
	_, _ = s.Store.DB.ExecContext(r.Context(), `DELETE FROM pdd_account_events WHERE created_at<?`, now-30*86400)
	if pddRiskErrors[strings.TrimSpace(in.ErrorType)] {
		_, _ = s.Store.DB.ExecContext(r.Context(), `UPDATE pdd_accounts SET enabled=0,credential_status='risk_blocked',last_error=?,updated_at=? WHERE id=? AND user_id=?`, "检测到拼多多风控或登录异常，已自动暂停："+strings.TrimSpace(in.ErrorType), now, in.AccountID, uid)
	}
	writeJSON(w, 200, map[string]any{"success": true})
}

func (s *Server) pddAccountRuntime(w http.ResponseWriter, r *http.Request) {
	uid := auth.SessionFromContext(r.Context()).UserID
	account, err := s.Store.PDDAccounts.Default(r.Context(), uid)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, 200, map[string]any{"status": "unconfigured", "current_task": "空闲", "today_operations": 0, "consecutive_failures": 0})
		return
	}
	if err != nil {
		writeErr(w, 500, "读取拼多多账号失败")
		return
	}
	nowLocal := time.Now().In(time.Local)
	start := time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(), 0, 0, 0, 0, nowLocal.Location()).Unix()
	var today, lastSuccess, lastFailure int64
	var lastError, currentTask string
	_ = s.Store.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM pdd_account_events WHERE pdd_account_id=? AND created_at>=?`, account.ID, start).Scan(&today)
	_ = s.Store.DB.QueryRowContext(r.Context(), `SELECT COALESCE(MAX(created_at),0) FROM pdd_account_events WHERE pdd_account_id=? AND status='success'`, account.ID).Scan(&lastSuccess)
	_ = s.Store.DB.QueryRowContext(r.Context(), `SELECT COALESCE(MAX(created_at),0) FROM pdd_account_events WHERE pdd_account_id=? AND status IN ('failed','risk')`, account.ID).Scan(&lastFailure)
	_ = s.Store.DB.QueryRowContext(r.Context(), `SELECT COALESCE(error_type,''),COALESCE(message,'') FROM pdd_account_events WHERE pdd_account_id=? AND status IN ('failed','risk') ORDER BY created_at DESC,id DESC LIMIT 1`, account.ID).Scan(&lastError, &currentTask)
	lastMessage := currentTask
	currentTask = "空闲"
	var active int
	if s.Store.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM pdd_account_locks WHERE pdd_account_id=? AND expires_at>?`, account.ID, time.Now().Unix()).Scan(&active) == nil && active > 0 {
		currentTask = "采购任务"
	} else if s.Store.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM pdd_message_tasks WHERE pdd_account_id=? AND status IN ('preflighting','sending')`, account.ID).Scan(&active) == nil && active > 0 {
		currentTask = "客服任务"
	}
	consecutive := 0
	rows, _ := s.Store.DB.QueryContext(r.Context(), `SELECT status FROM pdd_account_events WHERE pdd_account_id=? ORDER BY created_at DESC,id DESC LIMIT 100`, account.ID)
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var status string
			_ = rows.Scan(&status)
			if status == "success" {
				break
			}
			if status == "failed" || status == "risk" {
				consecutive++
			}
		}
	}
	status := account.CredentialStatus
	if !account.Enabled && status != "risk_blocked" {
		status = "paused"
	}
	writeJSON(w, 200, map[string]any{"status": status, "last_success_at": lastSuccess, "last_failure_at": lastFailure, "last_error_type": lastError, "last_error": lastMessage, "consecutive_failures": consecutive, "current_task": currentTask, "today_operations": today})
}

func (s *Server) listPDDAccountEvents(w http.ResponseWriter, r *http.Request) {
	uid := auth.SessionFromContext(r.Context()).UserID
	account, err := s.Store.PDDAccounts.Default(r.Context(), uid)
	if err != nil {
		writeJSON(w, 200, []any{})
		return
	}
	rows, err := s.Store.DB.QueryContext(r.Context(), `SELECT id,site,operation,status,error_type,message,task_id,created_at FROM pdd_account_events WHERE user_id=? AND pdd_account_id=? ORDER BY created_at DESC,id DESC LIMIT 20`, uid, account.ID)
	if err != nil {
		writeErr(w, 500, "读取拼多多运行日志失败")
		return
	}
	defer rows.Close()
	result := []map[string]any{}
	for rows.Next() {
		var id, created int64
		var site, operation, status, errorType, message, taskID string
		if rows.Scan(&id, &site, &operation, &status, &errorType, &message, &taskID, &created) == nil {
			result = append(result, map[string]any{"id": id, "site": site, "operation": operation, "status": status, "error_type": errorType, "message": message, "task_id": taskID, "created_at": created})
		}
	}
	writeJSON(w, 200, result)
}

func (s *Server) setPDDAccountPaused(w http.ResponseWriter, r *http.Request, paused bool) {
	uid := auth.SessionFromContext(r.Context()).UserID
	status := "unchecked"
	if paused {
		status = "paused"
	}
	res, _ := s.Store.DB.ExecContext(r.Context(), `UPDATE pdd_accounts SET enabled=?,credential_status=?,last_error='',updated_at=? WHERE user_id=? AND is_default=1`, boolInt(!paused), status, time.Now().Unix(), uid)
	if affected, _ := res.RowsAffected(); affected != 1 {
		writeErr(w, 404, "拼多多账号不存在")
		return
	}
	writeJSON(w, 200, map[string]any{"success": true})
}
