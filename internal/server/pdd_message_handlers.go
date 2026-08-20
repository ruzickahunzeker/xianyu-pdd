package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
)

var pddMessageTypes = map[string]bool{"change_phone": true, "restore_phone": true, "custom_message": true, "urge_shipping": true, "logistics_question": true, "product_question": true, "customer_question": true}

type pddMessageInput struct {
	PDDAccountID         string         `json:"pdd_account_id"`
	GoodsID              string         `json:"goods_id"`
	SKUID                string         `json:"sku_id"`
	MallSN               string         `json:"mall_sn"`
	CapturedChatURL      string         `json:"captured_chat_url"`
	TaskType             string         `json:"task_type"`
	BusinessID           string         `json:"business_id"`
	XianyuOrderID        string         `json:"xianyu_order_id"`
	PDDOrderID           string         `json:"pdd_order_id"`
	Message              string         `json:"message"`
	SendMode             string         `json:"send_mode"`
	SourcePlatform       string         `json:"source_platform"`
	SourceConversationID string         `json:"source_conversation_id"`
	SourceMessageID      string         `json:"source_message_id"`
	ParentTaskID         string         `json:"parent_task_id"`
	Metadata             map[string]any `json:"metadata"`
	ReplyExpected        bool           `json:"reply_expected"`
	Priority             int            `json:"priority"`
	ScheduledAt          int64          `json:"scheduled_at"`
}

func (s *Server) mountPDDMessages(r chi.Router) {
	s.mountPDDChatInbox(r)
	r.Get("/api/pdd/messages", s.listPDDMessages)
	r.Post("/api/pdd/messages", s.createPDDMessage)
	r.Post("/api/pdd/messages/claim", s.claimPDDMessage)
	r.Post("/api/pdd/messages/{task_id}/heartbeat", s.heartbeatPDDMessage)
	r.Post("/api/pdd/messages/{task_id}/preflight", s.preflightPDDMessage)
	r.Post("/api/pdd/messages/{task_id}/confirm", s.confirmPDDMessage)
	r.Post("/api/pdd/messages/{task_id}/retry", s.retryPDDMessage)
	r.Post("/api/pdd/messages/{task_id}/result", s.resultPDDMessage)
	r.Post("/api/pdd/messages/{task_id}/cancel", s.cancelPDDMessage)
}

func pddMessageMap(row interface{ Scan(...any) error }) (map[string]any, error) {
	var id, account, goods, sku, mall, chat, typ, message, business, xyOrder, pddOrder, metadata, source, conversation, sourceMessage, parent, mode, status, worker, lastError, result string
	var reply, priority, attempts int
	var scheduled, lease, sent, verified, created, updated int64
	err := row.Scan(&id, &account, &goods, &sku, &mall, &chat, &typ, &message, &business, &xyOrder, &pddOrder, &metadata, &source, &conversation, &sourceMessage, &parent, &reply, &mode, &priority, &scheduled, &status, &worker, &lease, &attempts, &sent, &verified, &lastError, &result, &created, &updated)
	return map[string]any{"id": id, "pdd_account_id": account, "goods_id": goods, "sku_id": sku, "mall_sn": mall, "captured_chat_url": chat, "task_type": typ, "message": message, "business_id": business, "xianyu_order_id": xyOrder, "pdd_order_id": pddOrder, "metadata": json.RawMessage(emptyJSONObject(metadata)), "source_platform": source, "source_conversation_id": conversation, "source_message_id": sourceMessage, "parent_task_id": parent, "reply_expected": reply != 0, "send_mode": mode, "priority": priority, "scheduled_at": scheduled, "status": status, "worker_id": worker, "lease_expires_at": lease, "attempts": attempts, "sent_at": sent, "verified_at": verified, "last_error": lastError, "result": json.RawMessage(emptyJSONObject(result)), "created_at": created, "updated_at": updated}, err
}

const pddMessageColumns = `id,pdd_account_id,goods_id,sku_id,mall_sn,captured_chat_url,task_type,message_text,business_id,xianyu_order_id,pdd_order_id,metadata_json,source_platform,source_conversation_id,source_message_id,parent_task_id,reply_expected,send_mode,priority,scheduled_at,status,worker_id,lease_expires_at,attempts,sent_at,verified_at,last_error,result_json,created_at,updated_at`

func (s *Server) createPDDMessage(w http.ResponseWriter, r *http.Request) {
	var in pddMessageInput
	if decodeJSON(r, &in) != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	in.Message = strings.TrimSpace(in.Message)
	in.TaskType = strings.TrimSpace(in.TaskType)
	if key == "" || len(key) > 191 {
		writeErr(w, 400, "缺少或无效的 Idempotency-Key")
		return
	}
	if !pddMessageTypes[in.TaskType] || in.Message == "" || len([]rune(in.Message)) > 2000 {
		writeErr(w, 400, "任务类型或消息正文无效")
		return
	}
	uid := auth.SessionFromContext(r.Context()).UserID
	if in.PDDAccountID == "" {
		if account, err := s.Store.PDDAccounts.Default(r.Context(), uid); err == nil {
			in.PDDAccountID = account.ID
		}
	}
	var accountCount int
	if s.Store.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM pdd_accounts WHERE id=? AND user_id=? AND enabled=1`, in.PDDAccountID, uid).Scan(&accountCount) != nil || accountCount != 1 {
		writeErr(w, 422, "拼多多账号不可用")
		return
	}
	if in.MallSN == "" && in.GoodsID != "" {
		_ = s.Store.DB.QueryRowContext(r.Context(), `SELECT mall_sn FROM pdd_products WHERE goods_id=?`, in.GoodsID).Scan(&in.MallSN)
	}
	if strings.TrimSpace(in.GoodsID) == "" || strings.TrimSpace(in.MallSN) == "" {
		writeErr(w, 422, "缺少 goods_id 或 mall_sn，无法唯一定位商家")
		return
	}
	if in.SendMode == "" {
		in.SendMode = "manual_confirm"
	}
	if in.SendMode != "manual_confirm" {
		writeErr(w, 400, "当前只支持人工确认发送")
		return
	}
	if (in.TaskType == "change_phone" || in.TaskType == "restore_phone" || in.TaskType == "urge_shipping" || in.TaskType == "logistics_question") && strings.TrimSpace(in.PDDOrderID) == "" {
		writeErr(w, 422, "订单类消息必须提供拼多多订单号")
		return
	}
	if in.ScheduledAt == 0 {
		in.ScheduledAt = time.Now().Unix()
	}
	metadata, _ := json.Marshal(in.Metadata)
	digest := sha256.Sum256([]byte(in.Message))
	now, id := time.Now().Unix(), uuid.NewString()
	_, err := s.Store.DB.ExecContext(r.Context(), `INSERT INTO pdd_message_tasks(id,user_id,idempotency_key,pdd_account_id,goods_id,sku_id,mall_sn,captured_chat_url,task_type,message_text,message_fingerprint,business_id,xianyu_order_id,pdd_order_id,metadata_json,source_platform,source_conversation_id,source_message_id,parent_task_id,reply_expected,send_mode,priority,scheduled_at,status,last_error,result_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,'pending','','{}',?,?)`, id, uid, key, in.PDDAccountID, strings.TrimSpace(in.GoodsID), strings.TrimSpace(in.SKUID), strings.TrimSpace(in.MallSN), strings.TrimSpace(in.CapturedChatURL), in.TaskType, in.Message, hex.EncodeToString(digest[:]), strings.TrimSpace(in.BusinessID), strings.TrimSpace(in.XianyuOrderID), strings.TrimSpace(in.PDDOrderID), string(metadata), strings.TrimSpace(in.SourcePlatform), strings.TrimSpace(in.SourceConversationID), strings.TrimSpace(in.SourceMessageID), strings.TrimSpace(in.ParentTaskID), boolInt(in.ReplyExpected), in.SendMode, in.Priority, in.ScheduledAt, now, now)
	if err != nil {
		row := s.Store.DB.QueryRowContext(r.Context(), `SELECT `+pddMessageColumns+` FROM pdd_message_tasks WHERE user_id=? AND idempotency_key=?`, uid, key)
		if out, scanErr := pddMessageMap(row); scanErr == nil {
			writeJSON(w, 200, out)
			return
		}
		writeErr(w, 500, "创建消息任务失败")
		return
	}
	out, _ := pddMessageMap(s.Store.DB.QueryRowContext(r.Context(), `SELECT `+pddMessageColumns+` FROM pdd_message_tasks WHERE id=?`, id))
	writeJSON(w, 201, out)
}

func (s *Server) listPDDMessages(w http.ResponseWriter, r *http.Request) {
	uid := auth.SessionFromContext(r.Context()).UserID
	query, args := `SELECT `+pddMessageColumns+` FROM pdd_message_tasks WHERE user_id=?`, []any{uid}
	if status := strings.TrimSpace(r.URL.Query().Get("status")); status != "" {
		query += ` AND status=?`
		args = append(args, status)
	}
	rows, err := s.Store.DB.QueryContext(r.Context(), query+` ORDER BY created_at DESC LIMIT 200`, args...)
	if err != nil {
		writeErr(w, 500, "读取消息任务失败")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		if item, e := pddMessageMap(rows); e == nil {
			out = append(out, item)
		}
	}
	writeJSON(w, 200, out)
}

func (s *Server) claimPDDMessage(w http.ResponseWriter, r *http.Request) {
	var in struct {
		WorkerID     string `json:"worker_id"`
		LeaseSeconds int    `json:"lease_seconds"`
	}
	_ = decodeJSON(r, &in)
	if in.WorkerID == "" {
		in.WorkerID = "pdd-worker"
	}
	if in.LeaseSeconds < 30 || in.LeaseSeconds > 900 {
		in.LeaseSeconds = 180
	}
	uid, now := auth.SessionFromContext(r.Context()).UserID, time.Now().Unix()
	s.purchaseMu.Lock()
	defer s.purchaseMu.Unlock()
	_, _ = s.Store.DB.ExecContext(r.Context(), `DELETE FROM pdd_message_merchant_locks WHERE expires_at<=?`, now)
	_, _ = s.Store.DB.ExecContext(r.Context(), `DELETE FROM pdd_account_locks WHERE expires_at<=?`, now)
	_, _ = s.Store.DB.ExecContext(r.Context(), `UPDATE pdd_message_tasks SET status='pending',worker_id='',lease_token='',lease_expires_at=0,last_error='预检租约过期，已重新排队',updated_at=? WHERE user_id=? AND status='preflighting' AND lease_expires_at<=?`, now, uid, now)
	_, _ = s.Store.DB.ExecContext(r.Context(), `UPDATE pdd_message_tasks SET status='result_unknown',worker_id='',lease_token='',lease_expires_at=0,last_error='发送租约过期，禁止自动重发，请人工核对',updated_at=? WHERE user_id=? AND status='sending' AND lease_expires_at<=?`, now, uid, now)
	var id, account, mall, status string
	err := s.Store.DB.QueryRowContext(r.Context(), `SELECT id,pdd_account_id,mall_sn,status FROM pdd_message_tasks WHERE user_id=? AND status IN ('pending','confirmed') AND scheduled_at<=? ORDER BY priority DESC,created_at LIMIT 1`, uid, now).Scan(&id, &account, &mall, &status)
	if err != nil {
		writeErr(w, 404, "没有可领取的消息任务")
		return
	}
	prefix := db.DialectInsertIgnorePrefix(s.Store.Dialect)
	suffix := db.DialectInsertIgnore(s.Store.Dialect, []string{"pdd_account_id"})
	res, err := s.Store.DB.ExecContext(r.Context(), prefix+` INTO pdd_account_locks(pdd_account_id,user_id,order_id,operation_id,locked_at,expires_at) VALUES(?,?,?,?,?,?)`+suffix, account, uid, "message:"+id, id, now, now+int64(in.LeaseSeconds))
	if err != nil {
		writeErr(w, 500, "锁定拼多多账号失败")
		return
	}
	affected, _ := res.RowsAffected()
	if affected != 1 {
		writeErr(w, 409, "拼多多账号正在执行其他写操作")
		return
	}
	prefix = db.DialectInsertIgnorePrefix(s.Store.Dialect)
	suffix = db.DialectInsertIgnore(s.Store.Dialect, []string{"user_id", "pdd_account_id", "mall_sn"})
	res, err = s.Store.DB.ExecContext(r.Context(), prefix+` INTO pdd_message_merchant_locks(user_id,pdd_account_id,mall_sn,task_id,expires_at) VALUES(?,?,?,?,?)`+suffix, uid, account, mall, id, now+int64(in.LeaseSeconds))
	if err != nil {
		_, _ = s.Store.DB.ExecContext(r.Context(), `DELETE FROM pdd_account_locks WHERE operation_id=?`, id)
		writeErr(w, 500, "锁定拼多多商家失败")
		return
	}
	affected, _ = res.RowsAffected()
	if affected != 1 {
		_, _ = s.Store.DB.ExecContext(r.Context(), `DELETE FROM pdd_account_locks WHERE operation_id=?`, id)
		writeErr(w, 409, "该商家正在执行其他消息任务")
		return
	}
	token := uuid.NewString()
	action := "preflight"
	claimedStatus := "preflighting"
	if status == "confirmed" {
		action = "send"
		claimedStatus = "sending"
	}
	res, err = s.Store.DB.ExecContext(r.Context(), `UPDATE pdd_message_tasks SET status=?,worker_id=?,lease_token=?,lease_expires_at=?,attempts=attempts+1,last_error='',updated_at=? WHERE id=? AND user_id=? AND status=?`, claimedStatus, in.WorkerID, token, now+int64(in.LeaseSeconds), now, id, uid, status)
	if err != nil {
		s.releasePDDMessageLocks(r, id)
		writeErr(w, 500, "领取消息任务失败")
		return
	}
	affected, _ = res.RowsAffected()
	if affected != 1 {
		s.releasePDDMessageLocks(r, id)
		writeErr(w, 409, "消息任务状态已变化")
		return
	}
	out, _ := pddMessageMap(s.Store.DB.QueryRowContext(r.Context(), `SELECT `+pddMessageColumns+` FROM pdd_message_tasks WHERE id=?`, id))
	out["lease_token"], out["action"] = token, action
	writeJSON(w, 200, out)
}

func (s *Server) messageLease(r *http.Request) (int64, string, string, bool) {
	var in struct {
		LeaseToken string `json:"lease_token"`
	}
	if decodeJSON(r, &in) != nil {
		return 0, "", "", false
	}
	uid := auth.SessionFromContext(r.Context()).UserID
	id := chi.URLParam(r, "task_id")
	var status string
	if s.Store.DB.QueryRowContext(r.Context(), `SELECT status FROM pdd_message_tasks WHERE id=? AND user_id=? AND lease_token=? AND lease_expires_at>?`, id, uid, in.LeaseToken, time.Now().Unix()).Scan(&status) != nil {
		return uid, id, in.LeaseToken, false
	}
	return uid, id, in.LeaseToken, true
}

func (s *Server) heartbeatPDDMessage(w http.ResponseWriter, r *http.Request) {
	uid, id, token, ok := s.messageLease(r)
	if !ok {
		writeErr(w, 409, "任务租约无效或已过期")
		return
	}
	now := time.Now().Unix()
	_, _ = s.Store.DB.ExecContext(r.Context(), `UPDATE pdd_message_tasks SET lease_expires_at=?,updated_at=? WHERE id=? AND user_id=? AND lease_token=?`, now+180, now, id, uid, token)
	_, _ = s.Store.DB.ExecContext(r.Context(), `UPDATE pdd_account_locks SET expires_at=? WHERE operation_id=?`, now+180, id)
	_, _ = s.Store.DB.ExecContext(r.Context(), `UPDATE pdd_message_merchant_locks SET expires_at=? WHERE task_id=?`, now+180, id)
	writeJSON(w, 200, map[string]any{"success": true})
}

func (s *Server) preflightPDDMessage(w http.ResponseWriter, r *http.Request) {
	uid, id, _, ok := s.messageLease(r)
	if !ok {
		writeErr(w, 409, "任务租约无效或已过期")
		return
	}
	now := time.Now().Unix()
	_, _ = s.Store.DB.ExecContext(r.Context(), `UPDATE pdd_message_tasks SET status='awaiting_confirmation',worker_id='',lease_token='',lease_expires_at=0,updated_at=? WHERE id=? AND user_id=?`, now, id, uid)
	s.releasePDDMessageLocks(r, id)
	writeJSON(w, 200, map[string]any{"success": true, "status": "awaiting_confirmation"})
}

func (s *Server) confirmPDDMessage(w http.ResponseWriter, r *http.Request) {
	uid, id := auth.SessionFromContext(r.Context()).UserID, chi.URLParam(r, "task_id")
	enabled, _ := s.Store.Settings.Get(r.Context(), "pdd_message_real_send_enabled")
	if !strings.EqualFold(strings.TrimSpace(enabled), "true") && strings.TrimSpace(enabled) != "1" {
		writeErr(w, 409, "真实发送安全开关未开启")
		return
	}
	res, _ := s.Store.DB.ExecContext(r.Context(), `UPDATE pdd_message_tasks SET status='confirmed',updated_at=? WHERE id=? AND user_id=? AND status='awaiting_confirmation'`, time.Now().Unix(), id, uid)
	n, _ := res.RowsAffected()
	if n != 1 {
		writeErr(w, 409, "任务当前不可确认发送")
		return
	}
	writeJSON(w, 200, map[string]any{"success": true, "status": "confirmed"})
}

func (s *Server) resultPDDMessage(w http.ResponseWriter, r *http.Request) {
	var in struct {
		LeaseToken string         `json:"lease_token"`
		Status     string         `json:"status"`
		Error      string         `json:"error"`
		Result     map[string]any `json:"result"`
	}
	if decodeJSON(r, &in) != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	uid, id, now := auth.SessionFromContext(r.Context()).UserID, chi.URLParam(r, "task_id"), time.Now().Unix()
	if in.Status != "verified" && in.Status != "result_unknown" && in.Status != "failed" {
		writeErr(w, 400, "结果状态无效")
		return
	}
	var current string
	if s.Store.DB.QueryRowContext(r.Context(), `SELECT status FROM pdd_message_tasks WHERE id=? AND user_id=? AND lease_token=?`, id, uid, in.LeaseToken).Scan(&current) != nil || (current != "sending" && !(current == "preflighting" && in.Status == "failed")) {
		writeErr(w, 409, "消息任务状态已变化")
		return
	}
	raw, _ := json.Marshal(in.Result)
	sent, verified := int64(0), int64(0)
	if in.Status == "verified" {
		sent, verified = now, now
	}
	if clicked, _ := in.Result["clicked"].(bool); clicked && in.Status == "result_unknown" {
		sent = now
	}
	res, _ := s.Store.DB.ExecContext(r.Context(), `UPDATE pdd_message_tasks SET status=?,sent_at=CASE WHEN ?>0 THEN ? ELSE sent_at END,verified_at=CASE WHEN ?>0 THEN ? ELSE verified_at END,last_error=?,result_json=?,worker_id='',lease_token='',lease_expires_at=0,updated_at=? WHERE id=? AND user_id=? AND lease_token=? AND status=?`, in.Status, sent, sent, verified, verified, strings.TrimSpace(in.Error), string(raw), now, id, uid, in.LeaseToken, current)
	n, _ := res.RowsAffected()
	if n != 1 {
		writeErr(w, 409, "消息任务状态已变化")
		return
	}
	s.releasePDDMessageLocks(r, id)
	writeJSON(w, 200, map[string]any{"success": true, "status": in.Status})
}

func (s *Server) cancelPDDMessage(w http.ResponseWriter, r *http.Request) {
	uid, id := auth.SessionFromContext(r.Context()).UserID, chi.URLParam(r, "task_id")
	res, _ := s.Store.DB.ExecContext(r.Context(), `UPDATE pdd_message_tasks SET status='cancelled',updated_at=? WHERE id=? AND user_id=? AND status IN ('pending','awaiting_confirmation','failed')`, time.Now().Unix(), id, uid)
	n, _ := res.RowsAffected()
	if n != 1 {
		writeErr(w, 409, "任务当前不可取消")
		return
	}
	writeJSON(w, 200, map[string]any{"success": true})
}

func (s *Server) retryPDDMessage(w http.ResponseWriter, r *http.Request) {
	uid, id := auth.SessionFromContext(r.Context()).UserID, chi.URLParam(r, "task_id")
	now := time.Now().Unix()
	res, _ := s.Store.DB.ExecContext(r.Context(), `UPDATE pdd_message_tasks SET status='pending',scheduled_at=?,last_error='',result_json='{}',updated_at=? WHERE id=? AND user_id=? AND status='failed'`, now, now, id, uid)
	n, _ := res.RowsAffected()
	if n != 1 {
		writeErr(w, 409, "仅明确失败的任务可以重试；结果未知时禁止重发")
		return
	}
	writeJSON(w, 200, map[string]any{"success": true, "status": "pending"})
}

func (s *Server) releasePDDMessageLocks(r *http.Request, id string) {
	_, _ = s.Store.DB.ExecContext(r.Context(), `DELETE FROM pdd_account_locks WHERE operation_id=?`, id)
	_, _ = s.Store.DB.ExecContext(r.Context(), `DELETE FROM pdd_message_merchant_locks WHERE task_id=?`, id)
}
