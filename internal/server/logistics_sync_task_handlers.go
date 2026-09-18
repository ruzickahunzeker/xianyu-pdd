package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"xianyu-go/internal/auth"
)

const (
	manualLogisticsBatchLimit     = 20
	manualLogisticsIntervalSecond = int64(10)
)

type logisticsSyncTaskView struct {
	ID, OrderID, PDDOrderID, PDDAccountID, Source, Status, WorkerID string
	ResultJSON, LastError                                           string
	ScheduledAt, LeaseExpiresAt, CreatedAt, StartedAt               int64
	FinishedAt, UpdatedAt                                           int64
}

const logisticsSyncTaskColumns = `id,order_id,pdd_order_id,pdd_account_id,source,status,worker_id,result_json,last_error,scheduled_at,lease_expires_at,created_at,started_at,finished_at,updated_at`

func scanLogisticsSyncTask(row interface{ Scan(...any) error }) (logisticsSyncTaskView, error) {
	var task logisticsSyncTaskView
	err := row.Scan(&task.ID, &task.OrderID, &task.PDDOrderID, &task.PDDAccountID, &task.Source, &task.Status, &task.WorkerID, &task.ResultJSON, &task.LastError, &task.ScheduledAt, &task.LeaseExpiresAt, &task.CreatedAt, &task.StartedAt, &task.FinishedAt, &task.UpdatedAt)
	return task, err
}

func logisticsSyncTaskJSON(task logisticsSyncTaskView) map[string]any {
	result := json.RawMessage(emptyJSONObject(task.ResultJSON))
	if task.Status == "queued" || task.Status == "processing" {
		result = json.RawMessage(`{}`)
	}
	return map[string]any{"id": task.ID, "order_id": task.OrderID, "pdd_order_id": task.PDDOrderID, "pdd_account_id": task.PDDAccountID, "source": task.Source, "status": task.Status, "worker_id": task.WorkerID, "result": result, "last_error": task.LastError, "scheduled_at": task.ScheduledAt, "lease_expires_at": task.LeaseExpiresAt, "created_at": task.CreatedAt, "started_at": task.StartedAt, "finished_at": task.FinishedAt, "updated_at": task.UpdatedAt}
}

func (s *Server) listLogisticsSyncTasks(w http.ResponseWriter, r *http.Request) {
	uid := auth.SessionFromContext(r.Context()).UserID
	rows, err := s.Store.DB.QueryContext(r.Context(), `SELECT `+logisticsSyncTaskColumns+` FROM pdd_logistics_sync_tasks WHERE user_id=? ORDER BY created_at DESC LIMIT 200`, uid)
	if err != nil {
		writeErr(w, 500, "读取物流同步任务失败")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		if task, scanErr := scanLogisticsSyncTask(rows); scanErr == nil {
			out = append(out, logisticsSyncTaskJSON(task))
		}
	}
	writeJSON(w, 200, out)
}

func (s *Server) logisticsSyncCandidate(r *http.Request, uid int64, orderID string) (string, error) {
	var pddOrderID string
	var pddShipped, xianyuShipped, exempt int
	err := s.Store.DB.QueryRowContext(r.Context(), `SELECT pdd_order_id,pdd_shipped,xianyu_shipped,fulfillment_exempt FROM order_fulfillments WHERE user_id=? AND order_id=?`, uid, orderID).Scan(&pddOrderID, &pddShipped, &xianyuShipped, &exempt)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(pddOrderID) == "" {
		return "", errors.New("订单缺少拼多多订单号")
	}
	if pddShipped != 0 {
		return "", errors.New("订单已经取得拼多多物流")
	}
	if xianyuShipped != 0 {
		return "", errors.New("闲鱼订单已经发货")
	}
	if exempt != 0 {
		return "", errors.New("该订单已排除履约")
	}
	return strings.TrimSpace(pddOrderID), nil
}

func (s *Server) insertLogisticsSyncTask(r *http.Request, uid int64, orderID, pddOrderID, accountID, source string, scheduledAt int64) (logisticsSyncTaskView, error) {
	now, id := time.Now().Unix(), uuid.NewString()
	_, err := s.Store.DB.ExecContext(r.Context(), `INSERT INTO pdd_logistics_sync_tasks(id,user_id,order_id,pdd_order_id,pdd_account_id,source,status,scheduled_at,active_marker,created_at,updated_at) VALUES(?,?,?,?,?,?,'queued',?,1,?,?)`, id, uid, orderID, pddOrderID, accountID, source, scheduledAt, now, now)
	if err != nil {
		return logisticsSyncTaskView{}, err
	}
	return scanLogisticsSyncTask(s.Store.DB.QueryRowContext(r.Context(), `SELECT `+logisticsSyncTaskColumns+` FROM pdd_logistics_sync_tasks WHERE id=?`, id))
}

func (s *Server) queueLogisticsSync(w http.ResponseWriter, r *http.Request) {
	orderID := strings.TrimSpace(chi.URLParam(r, "order_id"))
	if _, ok := s.requireOrderOwner(w, r, orderID); !ok {
		return
	}
	uid := auth.SessionFromContext(r.Context()).UserID
	s.logisticsMu.Lock()
	defer s.logisticsMu.Unlock()
	pddOrderID, err := s.logisticsSyncCandidate(r, uid, orderID)
	if err != nil {
		writeErr(w, 422, err.Error())
		return
	}
	account, err := s.Store.PDDAccounts.Default(r.Context(), uid)
	if err != nil || !account.Enabled {
		writeErr(w, 422, "请先配置并启用拼多多账号")
		return
	}
	var existingID string
	if s.Store.DB.QueryRowContext(r.Context(), `SELECT id FROM pdd_logistics_sync_tasks WHERE user_id=? AND order_id=? AND active_marker=1`, uid, orderID).Scan(&existingID) == nil {
		writeJSON(w, 200, map[string]any{"success": true, "status": "already_queued", "task_id": existingID})
		return
	}
	task, err := s.insertLogisticsSyncTask(r, uid, orderID, pddOrderID, account.ID, "single", time.Now().Unix())
	if err != nil {
		writeErr(w, 409, "物流同步任务已经存在")
		return
	}
	out := logisticsSyncTaskJSON(task)
	out["success"] = true
	writeJSON(w, 201, out)
}

func (s *Server) queueLogisticsSyncBatch(w http.ResponseWriter, r *http.Request) {
	uid := auth.SessionFromContext(r.Context()).UserID
	s.logisticsMu.Lock()
	defer s.logisticsMu.Unlock()
	account, err := s.Store.PDDAccounts.Default(r.Context(), uid)
	if err != nil || !account.Enabled {
		writeErr(w, 422, "请先配置并启用拼多多账号")
		return
	}
	rows, err := s.Store.DB.QueryContext(r.Context(), `SELECT order_id,pdd_order_id FROM order_fulfillments WHERE user_id=? AND pdd_order_id<>'' AND pdd_shipped=0 AND xianyu_shipped=0 AND fulfillment_exempt=0 ORDER BY updated_at LIMIT ?`, uid, manualLogisticsBatchLimit)
	if err != nil {
		writeErr(w, 500, "读取待同步物流订单失败")
		return
	}
	type candidate struct{ orderID, pddOrderID string }
	candidates := []candidate{}
	for rows.Next() {
		var item candidate
		if rows.Scan(&item.orderID, &item.pddOrderID) == nil {
			candidates = append(candidates, item)
		}
	}
	_ = rows.Close()
	next := time.Now().Unix()
	var lastScheduled int64
	_ = s.Store.DB.QueryRowContext(r.Context(), `SELECT COALESCE(MAX(scheduled_at),0) FROM pdd_logistics_sync_tasks WHERE user_id=? AND active_marker=1`, uid).Scan(&lastScheduled)
	if lastScheduled >= next {
		next = lastScheduled + manualLogisticsIntervalSecond
	}
	queued, skipped := 0, 0
	for _, item := range candidates {
		var exists int
		_ = s.Store.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM pdd_logistics_sync_tasks WHERE user_id=? AND order_id=? AND active_marker=1`, uid, item.orderID).Scan(&exists)
		if exists > 0 {
			skipped++
			continue
		}
		if _, insertErr := s.insertLogisticsSyncTask(r, uid, item.orderID, item.pddOrderID, account.ID, "batch", next); insertErr != nil {
			skipped++
			continue
		}
		queued++
		next += manualLogisticsIntervalSecond
	}
	writeJSON(w, 200, map[string]any{"success": true, "eligible": len(candidates), "queued": queued, "skipped": skipped, "interval_seconds": manualLogisticsIntervalSecond, "limit": manualLogisticsBatchLimit})
}

func (s *Server) claimLogisticsSyncTask(w http.ResponseWriter, r *http.Request) {
	var in struct {
		WorkerID     string `json:"worker_id"`
		LeaseSeconds int    `json:"lease_seconds"`
	}
	_ = decodeJSON(r, &in)
	if strings.TrimSpace(in.WorkerID) == "" {
		in.WorkerID = "pdd-worker"
	}
	if in.LeaseSeconds < 30 || in.LeaseSeconds > 900 {
		in.LeaseSeconds = 120
	}
	uid, now := auth.SessionFromContext(r.Context()).UserID, time.Now().Unix()
	s.purchaseMu.Lock()
	defer s.purchaseMu.Unlock()
	s.logisticsMu.Lock()
	defer s.logisticsMu.Unlock()
	_, _ = s.Store.DB.ExecContext(r.Context(), `DELETE FROM pdd_account_locks WHERE expires_at<=?`, now)
	_, _ = s.Store.DB.ExecContext(r.Context(), `UPDATE pdd_logistics_sync_tasks SET status='queued',worker_id='',lease_token='',lease_expires_at=0,scheduled_at=?,last_error='任务租约过期，已恢复队列',updated_at=? WHERE user_id=? AND status='processing' AND lease_expires_at>0 AND lease_expires_at<=?`, now, now, uid, now)
	var active int
	_ = s.Store.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM pdd_logistics_sync_tasks WHERE user_id=? AND status='processing' AND lease_expires_at>?`, uid, now).Scan(&active)
	if active > 0 {
		writeErr(w, 404, "没有可领取的物流同步任务")
		return
	}
	var id, accountID string
	err := s.Store.DB.QueryRowContext(r.Context(), `SELECT t.id,t.pdd_account_id FROM pdd_logistics_sync_tasks t JOIN pdd_accounts a ON a.id=t.pdd_account_id AND a.user_id=t.user_id WHERE t.user_id=? AND t.status='queued' AND t.scheduled_at<=? AND a.enabled=1 ORDER BY t.scheduled_at,t.created_at LIMIT 1`, uid, now).Scan(&id, &accountID)
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, 404, "没有可领取的物流同步任务")
		return
	}
	if err != nil {
		writeErr(w, 500, "读取物流同步任务失败")
		return
	}
	lockID, lease := uuid.NewString(), uuid.NewString()
	lockResult, lockErr := s.Store.DB.ExecContext(r.Context(), `INSERT INTO pdd_account_locks(pdd_account_id,user_id,order_id,operation_id,locked_at,expires_at) SELECT pdd_account_id,user_id,order_id,?, ?,? FROM pdd_logistics_sync_tasks WHERE id=? AND NOT EXISTS (SELECT 1 FROM pdd_account_locks WHERE pdd_account_id=?)`, lockID, now, now+int64(in.LeaseSeconds), id, accountID)
	if lockErr != nil {
		writeErr(w, 500, "获取拼多多账号物流锁失败")
		return
	}
	if affected, _ := lockResult.RowsAffected(); affected != 1 {
		writeErr(w, 404, "拼多多账号正在执行其他任务")
		return
	}
	result, err := s.Store.DB.ExecContext(r.Context(), `UPDATE pdd_logistics_sync_tasks SET status='processing',worker_id=?,lease_token=?,lease_expires_at=?,started_at=CASE WHEN started_at=0 THEN ? ELSE started_at END,updated_at=?,result_json=? WHERE id=? AND user_id=? AND status='queued'`, strings.TrimSpace(in.WorkerID), lease, now+int64(in.LeaseSeconds), now, now, `{"lock_id":"`+lockID+`"}`, id, uid)
	if err != nil {
		_, _ = s.Store.DB.ExecContext(r.Context(), `DELETE FROM pdd_account_locks WHERE operation_id=?`, lockID)
		writeErr(w, 500, "领取物流同步任务失败")
		return
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		_, _ = s.Store.DB.ExecContext(r.Context(), `DELETE FROM pdd_account_locks WHERE operation_id=?`, lockID)
		writeErr(w, 409, "物流同步任务状态已变化")
		return
	}
	task, _ := scanLogisticsSyncTask(s.Store.DB.QueryRowContext(r.Context(), `SELECT `+logisticsSyncTaskColumns+` FROM pdd_logistics_sync_tasks WHERE id=?`, id))
	out := logisticsSyncTaskJSON(task)
	out["lease_token"] = lease
	writeJSON(w, 200, out)
}

func (s *Server) heartbeatLogisticsSyncTask(w http.ResponseWriter, r *http.Request) {
	var in struct {
		LeaseToken string `json:"lease_token"`
	}
	if decodeJSON(r, &in) != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	uid, now := auth.SessionFromContext(r.Context()).UserID, time.Now().Unix()
	result, err := s.Store.DB.ExecContext(r.Context(), `UPDATE pdd_logistics_sync_tasks SET lease_expires_at=?,updated_at=? WHERE id=? AND user_id=? AND status='processing' AND lease_token=? AND lease_expires_at>?`, now+120, now, chi.URLParam(r, "task_id"), uid, strings.TrimSpace(in.LeaseToken), now)
	if err != nil {
		writeErr(w, 500, "物流同步任务续租失败")
		return
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		writeErr(w, 409, "物流同步任务租约无效或已过期")
		return
	}
	var lockResult string
	if s.Store.DB.QueryRowContext(r.Context(), `SELECT result_json FROM pdd_logistics_sync_tasks WHERE id=? AND user_id=?`, chi.URLParam(r, "task_id"), uid).Scan(&lockResult) == nil {
		var lockMeta map[string]any
		_ = json.Unmarshal([]byte(lockResult), &lockMeta)
		if lockID, _ := lockMeta["lock_id"].(string); lockID != "" {
			_, _ = s.Store.DB.ExecContext(r.Context(), `UPDATE pdd_account_locks SET expires_at=? WHERE operation_id=?`, now+120, lockID)
		}
	}
	writeJSON(w, 200, map[string]any{"success": true, "lease_expires_at": now + 120})
}

func (s *Server) completeLogisticsSyncTask(w http.ResponseWriter, r *http.Request) {
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
	if in.Status != "succeeded" && in.Status != "not_shipped" && in.Status != "failed" && in.Status != "blocked" {
		writeErr(w, 400, "物流同步结果状态无效")
		return
	}
	uid, now, taskID := auth.SessionFromContext(r.Context()).UserID, time.Now().Unix(), chi.URLParam(r, "task_id")
	var lockResult string
	var leaseExpires int64
	err := s.Store.DB.QueryRowContext(r.Context(), `SELECT result_json,lease_expires_at FROM pdd_logistics_sync_tasks WHERE id=? AND user_id=? AND status='processing' AND lease_token=?`, taskID, uid, strings.TrimSpace(in.LeaseToken)).Scan(&lockResult, &leaseExpires)
	if err != nil || leaseExpires <= now {
		writeErr(w, 409, "物流同步任务租约无效或已过期")
		return
	}
	var lockMeta map[string]any
	_ = json.Unmarshal([]byte(lockResult), &lockMeta)
	resultRaw, _ := json.Marshal(in.Result)
	result, err := s.Store.DB.ExecContext(r.Context(), `UPDATE pdd_logistics_sync_tasks SET status=?,result_json=?,last_error=?,active_marker=NULL,lease_token='',lease_expires_at=0,finished_at=?,updated_at=? WHERE id=? AND user_id=? AND status='processing'`, in.Status, string(resultRaw), strings.TrimSpace(in.Error), now, now, taskID, uid)
	if err != nil {
		writeErr(w, 500, "保存物流同步结果失败")
		return
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		writeErr(w, 409, "物流同步任务状态已变化")
		return
	}
	if lockID, _ := lockMeta["lock_id"].(string); lockID != "" {
		_, _ = s.Store.DB.ExecContext(r.Context(), `DELETE FROM pdd_account_locks WHERE operation_id=?`, lockID)
	}
	writeJSON(w, 200, map[string]any{"success": true, "status": in.Status})
}
