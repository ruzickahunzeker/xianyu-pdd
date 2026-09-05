package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
	"xianyu-go/internal/notify"
	"xianyu-go/internal/xianyu/mtop"
)

const scheduledPublishMinimumProfitCents int64 = 30

type scheduledPublishPreflightInput struct {
	MaterialIDs       []int64               `json:"material_ids"`
	AccountID         string                `json:"account_id"`
	MinimumProfitCent int64                 `json:"minimum_profit_cent"`
	Location          *mtop.PublishLocation `json:"location,omitempty"`
}

type scheduledPublishCreateInput struct {
	scheduledPublishPreflightInput
	StartAt int64 `json:"start_at"`
}

type scheduledPublishCloneInput struct {
	StartAt int64 `json:"start_at"`
}

type scheduledMaterialCheck struct {
	MaterialID       int64    `json:"material_id"`
	Title            string   `json:"title"`
	OK               bool     `json:"ok"`
	MinimumProfit    int64    `json:"minimum_profit_cent"`
	EnabledSKUCount  int      `json:"enabled_sku_count"`
	Reasons          []string `json:"reasons"`
	MaterialRevision int64    `json:"material_revision"`
	Snapshot         string   `json:"-"`
}

func (s *Server) mountScheduledPublish(r chi.Router) {
	r.Post("/scheduled-publish/preflight", s.scheduledPublishPreflight)
	r.Post("/scheduled-publish/batches", s.createScheduledPublishBatch)
	r.Get("/scheduled-publish/batches", s.listScheduledPublishBatches)
	r.Get("/scheduled-publish/tasks", s.listScheduledPublishTasks)
	r.Post("/scheduled-publish/tasks/{id}/cancel", s.cancelScheduledPublishTask)
	r.Post("/scheduled-publish/tasks/{id}/clone", s.cloneScheduledPublishTask)
	r.Post("/scheduled-publish/batches/{id}/cancel", s.cancelScheduledPublishBatch)
	r.Post("/scheduled-publish/accounts/{id}/resume", s.resumeScheduledPublishAccount)
}

func normalizeScheduledProfit(value int64) int64 {
	if value < scheduledPublishMinimumProfitCents {
		return scheduledPublishMinimumProfitCents
	}
	return value
}

func (s *Server) scheduledAccountAvailable(ctx context.Context, userID int64, accountID string) bool {
	accountID = strings.TrimSpace(accountID)
	var ownerID int64
	if err := s.Store.DB.QueryRowContext(ctx, `SELECT user_id FROM cookies WHERE id=?`, accountID).Scan(&ownerID); err != nil || ownerID != userID {
		return false
	}
	enabled, err := s.Store.Cookies.Status(ctx, accountID)
	return err == nil && enabled
}

func (s *Server) checkScheduledMaterial(ctx context.Context, userID, materialID, minimumProfit int64) scheduledMaterialCheck {
	result := scheduledMaterialCheck{MaterialID: materialID, OK: true, MinimumProfit: -1}
	material, err := scanMaterial(s.Store.DB.QueryRowContext(ctx, `SELECT id,user_id,source_type,source_id,title,description,images_json,category_json,skus_json,postage_mode,postage_cent,status,created_at,updated_at,image_property_name,video_enabled,videos_json,parent_material_id,split_batch_id,split_group_name,is_split_source,publish_parameters_json,revision FROM product_materials WHERE id=? AND user_id=? AND deleted_at IS NULL`, materialID, userID))
	if err != nil {
		result.OK = false
		result.Reasons = append(result.Reasons, "素材不存在")
		return result
	}
	result.Title = materialText(material["title"])
	result.MaterialRevision = jsonInt64(material["revision"])
	images := stringSlice(material["images"])
	if len(images) < 1 || len(images) > 9 {
		result.Reasons = append(result.Reasons, "商品图片必须为1到9张")
	}
	videos, _ := material["videos"].([]any)
	if material["video_enabled"] == true && len(videos) > 0 {
		result.Reasons = append(result.Reasons, "当前发布协议尚不支持素材视频")
	}
	rawSKUs, _ := material["skus"].([]any)
	for _, raw := range rawSKUs {
		row, _ := raw.(map[string]any)
		if enabled, exists := row["enabled"]; exists && enabled == false {
			continue
		}
		result.EnabledSKUCount++
		skuType := materialText(row["sku_type"])
		goodsID, skuID := materialText(row["source_goods_id"]), materialText(row["source_sku_id"])
		if skuType != materialSKUTypeSource || goodsID == "" || skuID == "" {
			result.Reasons = append(result.Reasons, fmt.Sprintf("规格 %s 缺少可信拼多多SKU映射", materialPropertiesLabel(row["properties"])))
			continue
		}
		var sourcePrice, stock int64
		var onSale int
		if err := s.Store.DB.QueryRowContext(ctx, `SELECT price_cent,stock,is_onsale FROM pdd_skus WHERE goods_id=? AND sku_id=?`, goodsID, skuID).Scan(&sourcePrice, &stock, &onSale); err != nil || sourcePrice <= 0 {
			result.Reasons = append(result.Reasons, fmt.Sprintf("规格 %s 缺少有效拼多多采购价", materialPropertiesLabel(row["properties"])))
			continue
		}
		if onSale == 0 || stock <= 0 {
			result.Reasons = append(result.Reasons, fmt.Sprintf("规格 %s 已下架或无库存", materialPropertiesLabel(row["properties"])))
		}
		profit := jsonInt64(row["price_cent"]) - sourcePrice
		if result.MinimumProfit < 0 || profit < result.MinimumProfit {
			result.MinimumProfit = profit
		}
		if profit <= minimumProfit {
			result.Reasons = append(result.Reasons, fmt.Sprintf("规格 %s 利润¥%.2f，必须严格大于¥%.2f", materialPropertiesLabel(row["properties"]), float64(profit)/100, float64(minimumProfit)/100))
		}
	}
	if result.EnabledSKUCount == 0 {
		result.Reasons = append(result.Reasons, "素材没有启用的SKU")
	}
	if result.EnabledSKUCount > materialSKUGroupLimit {
		result.Reasons = append(result.Reasons, fmt.Sprintf("启用SKU为%d个，超过%d个发布上限", result.EnabledSKUCount, materialSKUGroupLimit))
	}
	if result.MinimumProfit < 0 {
		result.MinimumProfit = 0
	}
	result.OK = len(result.Reasons) == 0
	result.Snapshot = mustJSON(material)
	return result
}

func materialPropertiesLabel(raw any) string {
	values, _ := raw.([]any)
	parts := make([]string, 0, len(values))
	for _, value := range values {
		row, _ := value.(map[string]any)
		parts = append(parts, materialText(row["name"])+"="+materialText(row["value"]))
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, "/")
}

func (s *Server) runScheduledPreflight(ctx context.Context, userID int64, input scheduledPublishPreflightInput) ([]scheduledMaterialCheck, error) {
	if len(input.MaterialIDs) == 0 || len(input.MaterialIDs) > 200 {
		return nil, fmt.Errorf("请选择1到200个素材")
	}
	if !s.scheduledAccountAvailable(ctx, userID, input.AccountID) {
		return nil, fmt.Errorf("发布账号不存在、已停用或不属于当前用户")
	}
	minimum := normalizeScheduledProfit(input.MinimumProfitCent)
	seen := map[int64]bool{}
	results := make([]scheduledMaterialCheck, 0, len(input.MaterialIDs))
	for _, id := range input.MaterialIDs {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		check := s.checkScheduledMaterial(ctx, userID, id, minimum)
		var pending int
		_ = s.Store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM scheduled_publish_tasks WHERE user_id=? AND material_id=? AND account_id=? AND status IN ('pending','prechecking','publishing','blocked')`, userID, id, input.AccountID).Scan(&pending)
		if pending > 0 {
			check.OK = false
			check.Reasons = append(check.Reasons, "该素材和账号已有未完成的定时发布任务")
		}
		results = append(results, check)
	}
	return results, nil
}

func (s *Server) scheduledPublishPreflight(w http.ResponseWriter, r *http.Request) {
	userID := auth.SessionFromContext(r.Context()).UserID
	var input scheduledPublishPreflightInput
	if decodeJSON(r, &input) != nil {
		writeErr(w, http.StatusBadRequest, "请求参数无效")
		return
	}
	input.MinimumProfitCent = normalizeScheduledProfit(input.MinimumProfitCent)
	results, err := s.runScheduledPreflight(r.Context(), userID, input)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": results, "minimum_profit_cent": input.MinimumProfitCent})
}

func (s *Server) createScheduledPublishBatch(w http.ResponseWriter, r *http.Request) {
	userID := auth.SessionFromContext(r.Context()).UserID
	var input scheduledPublishCreateInput
	if decodeJSON(r, &input) != nil {
		writeErr(w, http.StatusBadRequest, "请求参数无效")
		return
	}
	input.AccountID = strings.TrimSpace(input.AccountID)
	input.MinimumProfitCent = normalizeScheduledProfit(input.MinimumProfitCent)
	if input.StartAt < time.Now().Unix()-30 {
		writeErr(w, http.StatusBadRequest, "开始时间不能早于当前时间")
		return
	}
	checks, err := s.runScheduledPreflight(r.Context(), userID, input.scheduledPublishPreflightInput)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	for _, check := range checks {
		if !check.OK {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"detail": "存在未通过预检的素材", "items": checks})
			return
		}
	}
	locationJSON := "{}"
	if input.Location != nil {
		raw, _ := json.Marshal(input.Location)
		locationJSON = string(raw)
	}
	tx, err := s.Store.DB.BeginTx(r.Context(), nil)
	if err != nil {
		writeErr(w, 500, "创建发布批次失败")
		return
	}
	defer tx.Rollback()
	batchID, now := uuid.NewString(), time.Now().Unix()
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO scheduled_publish_batches(id,user_id,account_id,start_at,interval_min_seconds,interval_max_seconds,minimum_profit_cent,location_json,task_count,status,created_at) VALUES(?,?,?,?,300,600,?,?,?,'pending',?)`, batchID, userID, input.AccountID, input.StartAt, input.MinimumProfitCent, locationJSON, len(checks), now); err != nil {
		writeErr(w, 500, "保存发布批次失败")
		return
	}
	plannedAt := input.StartAt
	tasks := make([]map[string]any, 0, len(checks))
	for index, check := range checks {
		interval := int64(0)
		if index > 0 {
			interval = 300 + rand.Int63n(301)
			plannedAt += interval
		}
		taskID := uuid.NewString()
		if _, err = tx.ExecContext(r.Context(), `INSERT INTO scheduled_publish_tasks(id,batch_id,user_id,material_id,material_revision,account_id,sequence_no,planned_at,interval_seconds,not_before,status,attempt_count,idempotency_key,snapshot_json,minimum_profit_cent,minimum_actual_profit_cent,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,'pending',0,?,?,?,?,?)`, taskID, batchID, userID, check.MaterialID, check.MaterialRevision, input.AccountID, index+1, plannedAt, interval, plannedAt, "scheduled-publish:"+taskID+":"+strconv.FormatInt(check.MaterialRevision, 10), check.Snapshot, input.MinimumProfitCent, check.MinimumProfit, now); err != nil {
			writeErr(w, 500, "保存定时发布任务失败")
			return
		}
		tasks = append(tasks, map[string]any{"id": taskID, "material_id": check.MaterialID, "title": check.Title, "planned_at": plannedAt, "interval_seconds": interval})
	}
	if err = tx.Commit(); err != nil {
		writeErr(w, 500, "提交发布批次失败")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": batchID, "tasks": tasks, "minimum_profit_cent": input.MinimumProfitCent})
}

func (s *Server) listScheduledPublishBatches(w http.ResponseWriter, r *http.Request) {
	userID := auth.SessionFromContext(r.Context()).UserID
	rows, err := s.Store.DB.QueryContext(r.Context(), `SELECT b.id,b.account_id,b.start_at,b.minimum_profit_cent,b.task_count,b.status,b.created_at,b.finished_at,COALESCE(SUM(CASE WHEN t.status='success' THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN t.status='failed' THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN t.status='pending' THEN 1 ELSE 0 END),0) FROM scheduled_publish_batches b LEFT JOIN scheduled_publish_tasks t ON t.batch_id=b.id WHERE b.user_id=? GROUP BY b.id,b.account_id,b.start_at,b.minimum_profit_cent,b.task_count,b.status,b.created_at,b.finished_at ORDER BY b.created_at DESC`, userID)
	if err != nil {
		writeErr(w, 500, "查询发布批次失败")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, accountID, status string
		var startAt, minimum, count, created, finished, success, failed, pending int64
		if rows.Scan(&id, &accountID, &startAt, &minimum, &count, &status, &created, &finished, &success, &failed, &pending) == nil {
			out = append(out, map[string]any{"id": id, "account_id": accountID, "start_at": startAt, "minimum_profit_cent": minimum, "task_count": count, "status": status, "created_at": created, "finished_at": finished, "success_count": success, "failed_count": failed, "pending_count": pending})
		}
	}
	writeJSON(w, 200, out)
}

func (s *Server) listScheduledPublishTasks(w http.ResponseWriter, r *http.Request) {
	userID := auth.SessionFromContext(r.Context()).UserID
	query := `SELECT t.id,t.batch_id,t.material_id,COALESCE(m.title,''),t.account_id,t.sequence_no,t.planned_at,t.interval_seconds,t.status,t.attempt_count,t.minimum_profit_cent,t.minimum_actual_profit_cent,t.published_item_id,t.error_stage,t.error_code,t.error_message,t.account_queue_paused,t.created_at,t.started_at,t.finished_at FROM scheduled_publish_tasks t LEFT JOIN product_materials m ON m.id=t.material_id WHERE t.user_id=?`
	args := []any{userID}
	if status := strings.TrimSpace(r.URL.Query().Get("status")); status != "" {
		query += ` AND t.status=?`
		args = append(args, status)
	}
	query += ` ORDER BY t.created_at DESC,t.sequence_no`
	rows, err := s.Store.DB.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeErr(w, 500, "查询定时任务失败")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, batch, title, account, status, itemID, stage, code, message string
		var materialID, sequence, planned, interval, attempt, minimum, actual, paused, created, started, finished int64
		if rows.Scan(&id, &batch, &materialID, &title, &account, &sequence, &planned, &interval, &status, &attempt, &minimum, &actual, &itemID, &stage, &code, &message, &paused, &created, &started, &finished) == nil {
			out = append(out, map[string]any{"id": id, "batch_id": batch, "material_id": materialID, "title": title, "account_id": account, "sequence": sequence, "planned_at": planned, "interval_seconds": interval, "status": status, "attempt_count": attempt, "minimum_profit_cent": minimum, "minimum_actual_profit_cent": actual, "published_item_id": itemID, "error_stage": stage, "error_code": code, "error_message": message, "account_queue_paused": paused != 0, "created_at": created, "started_at": started, "finished_at": finished})
		}
	}
	writeJSON(w, 200, out)
}

func (s *Server) cancelScheduledPublishTask(w http.ResponseWriter, r *http.Request) {
	userID := auth.SessionFromContext(r.Context()).UserID
	id := chi.URLParam(r, "id")
	now := time.Now().Unix()
	result, err := s.Store.DB.ExecContext(r.Context(), `UPDATE scheduled_publish_tasks SET status='cancelled',finished_at=? WHERE id=? AND user_id=? AND status IN ('pending','blocked')`, now, id, userID)
	if err != nil {
		writeErr(w, 500, "取消任务失败")
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		writeErr(w, 409, "任务已开始或已经结束")
		return
	}
	s.refreshScheduledBatch(r.Context(), id, true)
	writeJSON(w, 200, map[string]any{"success": true})
}

func (s *Server) cloneScheduledPublishTask(w http.ResponseWriter, r *http.Request) {
	userID := auth.SessionFromContext(r.Context()).UserID
	var input scheduledPublishCloneInput
	if decodeJSON(r, &input) != nil {
		writeErr(w, http.StatusBadRequest, "请求参数无效")
		return
	}
	if input.StartAt == 0 {
		input.StartAt = time.Now().Add(time.Minute).Unix()
	}
	if input.StartAt < time.Now().Unix()-30 {
		writeErr(w, http.StatusBadRequest, "开始时间不能早于当前时间")
		return
	}
	var materialID, minimum int64
	var accountID, locationJSON, oldStatus string
	err := s.Store.DB.QueryRowContext(r.Context(), `SELECT t.material_id,t.account_id,t.minimum_profit_cent,t.status,b.location_json FROM scheduled_publish_tasks t JOIN scheduled_publish_batches b ON b.id=t.batch_id WHERE t.id=? AND t.user_id=?`, chi.URLParam(r, "id"), userID).Scan(&materialID, &accountID, &minimum, &oldStatus, &locationJSON)
	if err != nil {
		writeErr(w, http.StatusNotFound, "原定时任务不存在")
		return
	}
	if oldStatus != "failed" && oldStatus != "cancelled" {
		writeErr(w, http.StatusConflict, "仅失败或已取消任务可以复制")
		return
	}
	checks, err := s.runScheduledPreflight(r.Context(), userID, scheduledPublishPreflightInput{MaterialIDs: []int64{materialID}, AccountID: accountID, MinimumProfitCent: minimum})
	if err != nil || len(checks) != 1 || !checks[0].OK {
		if err != nil {
			writeErr(w, http.StatusUnprocessableEntity, err.Error())
		} else {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"detail": "素材未通过最新预检", "items": checks})
		}
		return
	}
	check := checks[0]
	tx, err := s.Store.DB.BeginTx(r.Context(), nil)
	if err != nil {
		writeErr(w, 500, "复制定时任务失败")
		return
	}
	defer tx.Rollback()
	batchID, taskID, now := uuid.NewString(), uuid.NewString(), time.Now().Unix()
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO scheduled_publish_batches(id,user_id,account_id,start_at,interval_min_seconds,interval_max_seconds,minimum_profit_cent,location_json,task_count,status,created_at) VALUES(?,?,?,?,300,600,?,?,1,'pending',?)`, batchID, userID, accountID, input.StartAt, minimum, locationJSON, now); err != nil {
		writeErr(w, 500, "复制发布批次失败")
		return
	}
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO scheduled_publish_tasks(id,batch_id,user_id,material_id,material_revision,account_id,sequence_no,planned_at,interval_seconds,not_before,status,attempt_count,idempotency_key,snapshot_json,minimum_profit_cent,minimum_actual_profit_cent,created_at) VALUES(?,?,?,?,?,?,1,?,0,?,'pending',0,?,?,?,?,?)`, taskID, batchID, userID, materialID, check.MaterialRevision, accountID, input.StartAt, input.StartAt, "scheduled-publish:"+taskID+":"+strconv.FormatInt(check.MaterialRevision, 10), check.Snapshot, minimum, check.MinimumProfit, now); err != nil {
		writeErr(w, 500, "复制定时任务失败")
		return
	}
	if err = tx.Commit(); err != nil {
		writeErr(w, 500, "提交复制任务失败")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"success": true, "batch_id": batchID, "task_id": taskID, "planned_at": input.StartAt})
}

func (s *Server) cancelScheduledPublishBatch(w http.ResponseWriter, r *http.Request) {
	userID := auth.SessionFromContext(r.Context()).UserID
	id := chi.URLParam(r, "id")
	now := time.Now().Unix()
	result, err := s.Store.DB.ExecContext(r.Context(), `UPDATE scheduled_publish_tasks SET status='cancelled',finished_at=? WHERE batch_id=? AND user_id=? AND status IN ('pending','blocked')`, now, id, userID)
	if err != nil {
		writeErr(w, 500, "取消批次失败")
		return
	}
	affected, _ := result.RowsAffected()
	s.refreshScheduledBatchByID(r.Context(), id)
	writeJSON(w, 200, map[string]any{"success": true, "cancelled": affected})
}

func (s *Server) resumeScheduledPublishAccount(w http.ResponseWriter, r *http.Request) {
	userID := auth.SessionFromContext(r.Context()).UserID
	accountID := strings.TrimSpace(chi.URLParam(r, "id"))
	if !s.scheduledAccountAvailable(r.Context(), userID, accountID) {
		writeErr(w, http.StatusBadRequest, "发布账号不存在、已停用或不属于当前用户")
		return
	}
	batchIDs := []string{}
	rows, err := s.Store.DB.QueryContext(r.Context(), `SELECT DISTINCT batch_id FROM scheduled_publish_tasks WHERE user_id=? AND account_id=? AND status='blocked'`, userID, accountID)
	if err != nil {
		writeErr(w, 500, "恢复账号发布队列失败")
		return
	}
	for rows.Next() {
		var batchID string
		if rows.Scan(&batchID) == nil {
			batchIDs = append(batchIDs, batchID)
		}
	}
	_ = rows.Close()
	now := time.Now().Unix()
	result, err := s.Store.DB.ExecContext(r.Context(), `UPDATE scheduled_publish_tasks SET status='pending',account_queue_paused=0,error_stage='',error_code='',error_message='',not_before=CASE WHEN not_before<? THEN ? ELSE not_before END WHERE user_id=? AND account_id=? AND status='blocked' AND attempt_count=0`, now, now, userID, accountID)
	if err != nil {
		writeErr(w, 500, "恢复账号发布队列失败")
		return
	}
	for _, batchID := range batchIDs {
		s.refreshScheduledBatchByID(r.Context(), batchID)
	}
	affected, _ := result.RowsAffected()
	writeJSON(w, 200, map[string]any{"success": true, "resumed": affected})
}

func (s *Server) refreshScheduledBatch(ctx context.Context, taskID string, task bool) {
	var batchID string
	if task {
		_ = s.Store.DB.QueryRowContext(ctx, `SELECT batch_id FROM scheduled_publish_tasks WHERE id=?`, taskID).Scan(&batchID)
	} else {
		batchID = taskID
	}
	if batchID != "" {
		s.refreshScheduledBatchByID(ctx, batchID)
	}
}

func (s *Server) refreshScheduledBatchByID(ctx context.Context, batchID string) {
	var total, success, failed, cancelled, pending, publishing, blocked int64
	_ = s.Store.DB.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(CASE WHEN status='success' THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN status='failed' THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN status='cancelled' THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN status='pending' THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN status='publishing' THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN status='blocked' THEN 1 ELSE 0 END),0) FROM scheduled_publish_tasks WHERE batch_id=?`, batchID).Scan(&total, &success, &failed, &cancelled, &pending, &publishing, &blocked)
	status := "running"
	finished := int64(0)
	switch {
	case blocked > 0:
		status = "paused"
	case pending+publishing > 0:
		status = "running"
	case success == total:
		status = "completed"
	case failed == total:
		status = "failed"
	case cancelled == total:
		status = "cancelled"
	default:
		status = "completed_with_failures"
	}
	if pending+publishing+blocked == 0 {
		finished = time.Now().Unix()
	}
	_, _ = s.Store.DB.ExecContext(ctx, `UPDATE scheduled_publish_batches SET status=?,finished_at=? WHERE id=?`, status, finished, batchID)
}

func (s *Server) delayNextScheduledTask(ctx context.Context, batchID string, sequence, finishedAt int64) {
	var id string
	var notBefore, interval int64
	if s.Store.DB.QueryRowContext(ctx, `SELECT id,not_before,interval_seconds FROM scheduled_publish_tasks WHERE batch_id=? AND sequence_no>? AND status='pending' ORDER BY sequence_no LIMIT 1`, batchID, sequence).Scan(&id, &notBefore, &interval) != nil {
		return
	}
	minimum := finishedAt + interval
	if notBefore < minimum {
		_, _ = s.Store.DB.ExecContext(ctx, `UPDATE scheduled_publish_tasks SET not_before=? WHERE id=? AND status='pending'`, minimum, id)
	}
}

func scheduledAccountError(message string) bool {
	lower := strings.ToLower(message)
	for _, part := range []string{"unauthorized", "无权限", "cookie", "登录", "验证码", "风控", "频率", "凭证", "账号", "被拒绝"} {
		if strings.Contains(lower, strings.ToLower(part)) {
			return true
		}
	}
	return false
}

func (s *Server) refreshScheduledMaterialSources(ctx context.Context, userID, materialID int64) error {
	var raw string
	if err := s.Store.DB.QueryRowContext(ctx, `SELECT skus_json FROM product_materials WHERE id=? AND user_id=? AND deleted_at IS NULL`, materialID, userID).Scan(&raw); err != nil {
		return errors.New("素材不存在")
	}
	var skus []materialSKU
	if json.Unmarshal([]byte(raw), &skus) != nil {
		return errors.New("素材SKU数据无效")
	}
	goodsIDs := map[string]bool{}
	for _, sku := range skus {
		if sku.Enabled && sku.SKUType == materialSKUTypeSource && strings.TrimSpace(sku.SourceGoodsID) != "" {
			goodsIDs[strings.TrimSpace(sku.SourceGoodsID)] = true
		}
	}
	for goodsID := range goodsIDs {
		request := httptest.NewRequest(http.MethodPost, "/api/pdd-collector/catalog/"+goodsID+"/refresh", nil)
		routeContext := chi.NewRouteContext()
		routeContext.URLParams.Add("goodsID", goodsID)
		request = request.WithContext(auth.WithSession(context.WithValue(ctx, chi.RouteCtxKey, routeContext), &db.Session{UserID: userID}))
		recorder := httptest.NewRecorder()
		s.pddRefreshProduct(recorder, request)
		if recorder.Code < 200 || recorder.Code >= 300 {
			var payload map[string]any
			_ = json.Unmarshal(recorder.Body.Bytes(), &payload)
			message := strings.TrimSpace(fmt.Sprint(payload["detail"]))
			if message == "" {
				message = "刷新拼多多商品失败"
			}
			return fmt.Errorf("商品%s：%s", goodsID, message)
		}
	}
	return nil
}

func (s *Server) executeScheduledPublishTask(ctx context.Context, taskID string) {
	now := time.Now().Unix()
	result, err := s.Store.DB.ExecContext(ctx, `UPDATE scheduled_publish_tasks SET status='publishing',attempt_count=1,started_at=? WHERE id=? AND status='pending' AND attempt_count=0`, now, taskID)
	if err != nil {
		return
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return
	}
	var batchID, accountID, locationJSON string
	var userID, materialID, revision, minimum, sequence int64
	if s.Store.DB.QueryRowContext(ctx, `SELECT t.batch_id,t.user_id,t.material_id,t.material_revision,t.account_id,t.minimum_profit_cent,t.sequence_no,b.location_json FROM scheduled_publish_tasks t JOIN scheduled_publish_batches b ON b.id=t.batch_id WHERE t.id=?`, taskID).Scan(&batchID, &userID, &materialID, &revision, &accountID, &minimum, &sequence, &locationJSON) != nil {
		return
	}
	s.refreshScheduledBatchByID(ctx, batchID)
	if err := s.refreshScheduledMaterialSources(ctx, userID, materialID); err != nil {
		s.failScheduledTask(ctx, taskID, batchID, accountID, "source_refresh", "PDD_REFRESH_FAILED", err.Error(), scheduledAccountError(err.Error()))
		return
	}
	check := s.checkScheduledMaterial(ctx, userID, materialID, minimum)
	if !check.OK || check.MaterialRevision != revision {
		message := strings.Join(check.Reasons, "；")
		if check.MaterialRevision != revision {
			message = "素材已被修改，请重新创建定时任务"
		}
		s.failScheduledTask(ctx, taskID, batchID, accountID, "preflight", "PRECHECK_FAILED", message, false)
		return
	}
	input := publishMaterialInput{CookieID: accountID}
	if strings.TrimSpace(locationJSON) != "" && locationJSON != "{}" {
		var location mtop.PublishLocation
		if json.Unmarshal([]byte(locationJSON), &location) == nil {
			input.Location = &location
		}
	}
	body, _ := json.Marshal(input)
	request := httptest.NewRequest(http.MethodPost, "/materials/"+strconv.FormatInt(materialID, 10)+"/publish", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("id", strconv.FormatInt(materialID, 10))
	request = request.WithContext(auth.WithSession(context.WithValue(ctx, chi.RouteCtxKey, routeContext), &db.Session{UserID: userID}))
	recorder := httptest.NewRecorder()
	s.publishMaterial(recorder, request)
	var payload map[string]any
	_ = json.Unmarshal(recorder.Body.Bytes(), &payload)
	if recorder.Code < 200 || recorder.Code >= 300 {
		message := strings.TrimSpace(fmt.Sprint(payload["detail"]))
		if message == "" {
			message = strings.TrimSpace(fmt.Sprint(payload["message"]))
		}
		if message == "" {
			message = "发布请求失败"
		}
		s.failScheduledTask(ctx, taskID, batchID, accountID, "publish", strings.TrimSpace(fmt.Sprint(payload["code"])), message, scheduledAccountError(message))
		return
	}
	itemID := strings.TrimSpace(fmt.Sprint(payload["item_id"]))
	finishedAt := time.Now().Unix()
	_, _ = s.Store.DB.ExecContext(ctx, `UPDATE scheduled_publish_tasks SET status='success',minimum_actual_profit_cent=?,published_item_id=?,finished_at=? WHERE id=?`, check.MinimumProfit, itemID, finishedAt, taskID)
	s.delayNextScheduledTask(ctx, batchID, sequence, finishedAt)
	s.refreshScheduledBatchByID(ctx, batchID)
}

func (s *Server) failScheduledTask(ctx context.Context, taskID, batchID, accountID, stage, code, message string, pause bool) {
	paused := 0
	if pause {
		paused = 1
	}
	finishedAt := time.Now().Unix()
	_, _ = s.Store.DB.ExecContext(ctx, `UPDATE scheduled_publish_tasks SET status='failed',error_stage=?,error_code=?,error_message=?,account_queue_paused=?,finished_at=? WHERE id=?`, stage, code, message, paused, finishedAt, taskID)
	if pause {
		var userID int64
		_ = s.Store.DB.QueryRowContext(ctx, `SELECT user_id FROM scheduled_publish_tasks WHERE id=?`, taskID).Scan(&userID)
		_, _ = s.Store.DB.ExecContext(ctx, `UPDATE scheduled_publish_tasks SET status='blocked',account_queue_paused=1,error_stage='account',error_code='ACCOUNT_QUEUE_PAUSED',error_message='同账号前序任务发生账号级异常，已停止自动发布' WHERE user_id=? AND account_id=? AND status='pending'`, userID, accountID)
		rows, err := s.Store.DB.QueryContext(ctx, `SELECT DISTINCT batch_id FROM scheduled_publish_tasks WHERE user_id=? AND account_id=? AND status='blocked'`, userID, accountID)
		if err == nil {
			for rows.Next() {
				var affectedBatch string
				if rows.Scan(&affectedBatch) == nil {
					s.refreshScheduledBatchByID(ctx, affectedBatch)
				}
			}
			_ = rows.Close()
		}
	} else {
		var sequence int64
		_ = s.Store.DB.QueryRowContext(ctx, `SELECT sequence_no FROM scheduled_publish_tasks WHERE id=?`, taskID).Scan(&sequence)
		s.delayNextScheduledTask(ctx, batchID, sequence, finishedAt)
	}
	s.refreshScheduledBatchByID(ctx, batchID)
	if s.notifier != nil {
		s.notifier.NotifyEvent(ctx, notify.NotificationEvent{AccountID: accountID, Type: notify.EventSystemError, Level: "error", Title: "定时发布失败", Body: message, Fields: map[string]string{"task_id": taskID, "batch_id": batchID}, Time: time.Now()})
	}
}

func (s *Server) RunScheduledPublishScheduler(ctx context.Context) {
	recoveryBatches := []string{}
	if rows, err := s.Store.DB.QueryContext(ctx, `SELECT DISTINCT batch_id FROM scheduled_publish_tasks WHERE status='publishing'`); err == nil {
		for rows.Next() {
			var batchID string
			if rows.Scan(&batchID) == nil {
				recoveryBatches = append(recoveryBatches, batchID)
			}
		}
		_ = rows.Close()
	}
	_, _ = s.Store.DB.ExecContext(ctx, `UPDATE scheduled_publish_tasks SET status='failed',error_stage='recovery',error_code='PUBLISH_RESULT_UNKNOWN',error_message='服务重启时任务仍处于发布中；为防止重复发布，已停止并等待人工核对',finished_at=? WHERE status='publishing'`, time.Now().Unix())
	for _, batchID := range recoveryBatches {
		s.refreshScheduledBatchByID(ctx, batchID)
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rows, err := s.Store.DB.QueryContext(ctx, `SELECT id,account_id FROM scheduled_publish_tasks t WHERE status='pending' AND attempt_count=0 AND not_before<=? AND NOT EXISTS(SELECT 1 FROM scheduled_publish_tasks running WHERE running.account_id=t.account_id AND running.status='publishing') ORDER BY not_before,sequence_no`, time.Now().Unix())
			if err != nil {
				continue
			}
			type due struct{ id, account string }
			items := []due{}
			accounts := map[string]bool{}
			for rows.Next() {
				var item due
				if rows.Scan(&item.id, &item.account) == nil && !accounts[item.account] {
					accounts[item.account] = true
					items = append(items, item)
				}
			}
			_ = rows.Close()
			for _, item := range items {
				go s.executeScheduledPublishTask(ctx, item.id)
			}
		}
	}
}

func (s *Server) StartScheduledPublishScheduler(ctx context.Context) {
	go s.RunScheduledPublishScheduler(ctx)
}
