package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
	"xianyu-go/internal/pddaddress"
	"xianyu-go/internal/pddcheckout"
	"xianyu-go/internal/pddproduct"
)

type purchaseTaskView struct {
	ID, OrderID, PDDAccountID, Status, WorkerID, ExpectedGoodsID, ExpectedSKUID string
	ExpectedReceiverName, ExpectedProvince, ExpectedCity, ExpectedDistrict      string
	ExpectedDetailAddress, BeforeOrderSNs, PDDOrderID, PDDOrderJSON, LastError  string
	Attempt, ExpectedQuantity                                                   int
	ExpectedAmountCent, LeaseExpiresAt, CreatedAt, StartedAt, SubmittedAt       int64
	FinishedAt, UpdatedAt                                                       int64
}

func (s *Server) scanPurchaseTask(row interface{ Scan(...any) error }) (purchaseTaskView, error) {
	var v purchaseTaskView
	err := row.Scan(&v.ID, &v.OrderID, &v.PDDAccountID, &v.Attempt, &v.Status, &v.WorkerID, &v.LeaseExpiresAt, &v.ExpectedGoodsID, &v.ExpectedSKUID, &v.ExpectedQuantity, &v.ExpectedAmountCent, &v.ExpectedReceiverName, &v.ExpectedProvince, &v.ExpectedCity, &v.ExpectedDistrict, &v.ExpectedDetailAddress, &v.BeforeOrderSNs, &v.PDDOrderID, &v.PDDOrderJSON, &v.LastError, &v.CreatedAt, &v.StartedAt, &v.SubmittedAt, &v.FinishedAt, &v.UpdatedAt)
	return v, err
}

const purchaseTaskColumns = `id,order_id,pdd_account_id,attempt,status,worker_id,lease_expires_at,expected_goods_id,expected_sku_id,expected_quantity,expected_amount_cent,expected_receiver_name,expected_province,expected_city,expected_district,expected_detail_address,before_order_sns,COALESCE(pdd_order_id,''),pdd_order_json,last_error,created_at,started_at,submitted_at,finished_at,updated_at`

func purchaseTaskJSON(v purchaseTaskView) map[string]any {
	var before []string
	_ = json.Unmarshal([]byte(v.BeforeOrderSNs), &before)
	return map[string]any{"id": v.ID, "order_id": v.OrderID, "pdd_account_id": v.PDDAccountID, "attempt": v.Attempt, "status": v.Status, "worker_id": v.WorkerID, "lease_expires_at": v.LeaseExpiresAt, "source_goods_id": v.ExpectedGoodsID, "source_sku_id": v.ExpectedSKUID, "quantity": v.ExpectedQuantity, "xianyu_amount_cent": v.ExpectedAmountCent, "receiver_name": v.ExpectedReceiverName, "province": v.ExpectedProvince, "city": v.ExpectedCity, "district": v.ExpectedDistrict, "detail_address": v.ExpectedDetailAddress, "before_order_sns": before, "pdd_order_id": v.PDDOrderID, "pdd_order": json.RawMessage(emptyJSONObject(v.PDDOrderJSON)), "last_error": v.LastError, "created_at": v.CreatedAt, "started_at": v.StartedAt, "submitted_at": v.SubmittedAt, "finished_at": v.FinishedAt, "updated_at": v.UpdatedAt}
}

func emptyJSONObject(raw string) []byte {
	if json.Valid([]byte(raw)) {
		return []byte(raw)
	}
	return []byte(`{}`)
}

func (s *Server) listPurchaseTasks(w http.ResponseWriter, r *http.Request) {
	userID := auth.SessionFromContext(r.Context()).UserID
	rows, err := s.Store.DB.QueryContext(r.Context(), `SELECT `+purchaseTaskColumns+` FROM pdd_purchase_tasks WHERE user_id=? ORDER BY created_at DESC LIMIT 200`, userID)
	if err != nil {
		writeErr(w, 500, "读取采购任务失败")
		return
	}
	defer rows.Close()
	result := []map[string]any{}
	for rows.Next() {
		if v, e := s.scanPurchaseTask(rows); e == nil {
			result = append(result, purchaseTaskJSON(v))
		}
	}
	writeJSON(w, 200, result)
}

func parsePurchasePositiveInt(raw string, fallback int) int {
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}

func canSavePurchaseBaseline(status string) bool {
	switch status {
	case "claimed", "validating", "goods_validated", "browser_preparing":
		return true
	default:
		return false
	}
}
func amountCent(raw string) int64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || f < 0 {
		return 0
	}
	return int64(f*100 + 0.5)
}

func (s *Server) claimPurchaseTask(w http.ResponseWriter, r *http.Request) {
	var in struct {
		OrderID      string `json:"order_id"`
		WorkerID     string `json:"worker_id"`
		LeaseSeconds int    `json:"lease_seconds"`
	}
	if r.Body != nil {
		_ = decodeJSON(r, &in)
	}
	in.OrderID, in.WorkerID = strings.TrimSpace(in.OrderID), strings.TrimSpace(in.WorkerID)
	if in.WorkerID == "" {
		in.WorkerID = "manual-worker"
	}
	if in.LeaseSeconds < 30 || in.LeaseSeconds > 900 {
		in.LeaseSeconds = 120
	}
	userID := auth.SessionFromContext(r.Context()).UserID
	s.purchaseMu.Lock()
	defer s.purchaseMu.Unlock()
	now := time.Now().Unix()
	// Only pre-submit leases may expire automatically. Preserve the failed attempt
	// and create a new attempt on the next claim; never reclaim a submitting task.
	_, _ = s.Store.DB.ExecContext(r.Context(), `DELETE FROM pdd_account_locks WHERE operation_id IN (SELECT id FROM pdd_purchase_tasks WHERE user_id=? AND status IN ('claimed','loading_goods','validating_goods','goods_validated','validating','browser_preparing') AND lease_expires_at>0 AND lease_expires_at<=?)`, userID, now)
	_, _ = s.Store.DB.ExecContext(r.Context(), `DELETE FROM pdd_account_locks WHERE expires_at<=?`, now)
	_, _ = s.Store.DB.ExecContext(r.Context(), `DELETE FROM pdd_account_locks WHERE operation_id IN (SELECT id FROM pdd_purchase_tasks WHERE user_id=? AND status='result_unknown')`, userID)
	_, _ = s.Store.DB.ExecContext(r.Context(), `UPDATE pdd_purchase_tasks SET status='failed',last_error='任务租约过期，尚未提交拼多多订单',worker_id='',lease_token='',lease_expires_at=0,finished_at=?,updated_at=? WHERE user_id=? AND status IN ('claimed','loading_goods','validating_goods','goods_validated','validating','browser_preparing') AND lease_expires_at>0 AND lease_expires_at<=?`, now, now, userID, now)
	// A result_unknown task may already have created an unpaid order. Recover it
	// before claiming any new purchase attempt, and never click submit again.
	var recoveryID, recoveryAccountID string
	recoveryQuery := `SELECT id,pdd_account_id FROM pdd_purchase_tasks WHERE user_id=? AND status='result_unknown' AND COALESCE(pdd_order_id,'')=''`
	recoveryArgs := []any{userID}
	if in.OrderID != "" {
		recoveryQuery += ` AND order_id=?`
		recoveryArgs = append(recoveryArgs, in.OrderID)
	}
	recoveryQuery += ` ORDER BY started_at LIMIT 1`
	if s.Store.DB.QueryRowContext(r.Context(), recoveryQuery, recoveryArgs...).Scan(&recoveryID, &recoveryAccountID) == nil {
		lease := uuid.NewString()
		prefix := db.DialectInsertIgnorePrefix(s.Store.Dialect)
		suffix := db.DialectInsertIgnore(s.Store.Dialect, []string{"pdd_account_id"})
		lock, lockErr := s.Store.DB.ExecContext(r.Context(), prefix+` INTO pdd_account_locks(pdd_account_id,user_id,order_id,operation_id,locked_at,expires_at) SELECT pdd_account_id,user_id,order_id,id,?,? FROM pdd_purchase_tasks WHERE id=?`+suffix, now, now+int64(in.LeaseSeconds), recoveryID)
		if lockErr != nil {
			writeErr(w, 500, "获取拼多多账号恢复锁失败")
			return
		}
		if affected, _ := lock.RowsAffected(); affected != 1 {
			writeErr(w, http.StatusConflict, "该拼多多账号正在处理其他任务")
			return
		}
		res, updateErr := s.Store.DB.ExecContext(r.Context(), `UPDATE pdd_purchase_tasks SET status='reconciling_result',worker_id=?,lease_token=?,lease_expires_at=?,updated_at=? WHERE id=? AND user_id=? AND status='result_unknown'`, in.WorkerID, lease, now+int64(in.LeaseSeconds), now, recoveryID, userID)
		if updateErr != nil {
			writeErr(w, 500, "领取结果恢复任务失败")
			return
		}
		if affected, _ := res.RowsAffected(); affected != 1 {
			writeErr(w, 409, "结果恢复任务状态已变化")
			return
		}
		v, _ := s.scanPurchaseTask(s.Store.DB.QueryRowContext(r.Context(), `SELECT `+purchaseTaskColumns+` FROM pdd_purchase_tasks WHERE id=?`, recoveryID))
		out := purchaseTaskJSON(v)
		out["lease_token"] = lease
		out["recovery_only"] = true
		writeJSON(w, 200, out)
		return
	}
	query := `SELECT f.order_id,f.source_goods_id,f.source_sku_id,COALESCE(o.quantity,'1'),COALESCE(o.amount,'0'),COALESCE(o.receiver_name,''),COALESCE(o.receiver_city,''),COALESCE(o.receiver_address,'') FROM order_fulfillments f JOIN orders o ON o.order_id=f.order_id WHERE f.user_id=? AND o.deleted_at IS NULL AND o.order_status IN ('processing','pending_ship') AND f.fulfillment_exempt=0 AND f.pdd_ordered=0 AND f.pdd_order_id='' AND f.mapping_status='mapped' AND f.source_goods_id<>'' AND f.source_sku_id<>''`
	args := []any{userID}
	if in.OrderID != "" {
		query += ` AND f.order_id=?`
		args = append(args, in.OrderID)
	}
	query += ` AND NOT EXISTS (SELECT 1 FROM pdd_purchase_tasks t WHERE t.user_id=f.user_id AND t.order_id=f.order_id AND t.status NOT IN ('failed','aborted','completed')) ORDER BY CASE WHEN f.purchase_requested_at>0 THEN 0 ELSE 1 END, f.purchase_requested_at, f.updated_at LIMIT 1`
	var orderID, goodsID, skuID, quantityRaw, amountRaw, receiver, city, address string
	if err := s.Store.DB.QueryRowContext(r.Context(), query, args...).Scan(&orderID, &goodsID, &skuID, &quantityRaw, &amountRaw, &receiver, &city, &address); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeErr(w, http.StatusNotFound, "没有可领取的采购任务")
			return
		}
		writeErr(w, 500, "读取待采购订单失败")
		return
	}
	account, err := s.Store.PDDAccounts.Default(r.Context(), userID)
	if err != nil || !account.Enabled {
		writeErr(w, 422, "请先配置并启用拼多多账号")
		return
	}
	match, err := pddaddress.Resolve(city, address)
	if err != nil {
		s.createFulfillmentException(r, userID, orderID, "", "blocked_address", err.Error(), map[string]any{"receiver_city": city, "receiver_address": address})
		writeErr(w, 422, err.Error())
		return
	}
	var attempt int
	_ = s.Store.DB.QueryRowContext(r.Context(), `SELECT COALESCE(MAX(attempt),0)+1 FROM pdd_purchase_tasks WHERE user_id=? AND order_id=?`, userID, orderID).Scan(&attempt)
	id, lease := uuid.NewString(), uuid.NewString()
	prefix := db.DialectInsertIgnorePrefix(s.Store.Dialect)
	suffix := db.DialectInsertIgnore(s.Store.Dialect, []string{"pdd_account_id"})
	lockResult, lockErr := s.Store.DB.ExecContext(r.Context(), prefix+` INTO pdd_account_locks(pdd_account_id,user_id,order_id,operation_id,locked_at,expires_at) VALUES(?,?,?,?,?,?)`+suffix, account.ID, userID, orderID, id, now, now+int64(in.LeaseSeconds))
	if lockErr != nil {
		writeErr(w, 500, "获取拼多多账号下单锁失败")
		return
	}
	if affected, _ := lockResult.RowsAffected(); affected != 1 {
		var lockedOrder string
		_ = s.Store.DB.QueryRowContext(r.Context(), `SELECT order_id FROM pdd_account_locks WHERE pdd_account_id=?`, account.ID).Scan(&lockedOrder)
		writeErr(w, http.StatusConflict, "该拼多多账号正在处理订单 "+lockedOrder)
		return
	}
	_, err = s.Store.DB.ExecContext(r.Context(), `INSERT INTO pdd_purchase_tasks(id,user_id,order_id,pdd_account_id,attempt,status,worker_id,lease_token,lease_expires_at,expected_goods_id,expected_sku_id,expected_quantity,expected_amount_cent,expected_receiver_name,expected_province,expected_city,expected_district,expected_detail_address,before_order_sns,pdd_order_json,last_error,created_at,started_at,updated_at) VALUES(?,?,?,?,?,'claimed',?,?,?,?,?,?,?,?,?,?,?,?,'[]','','',?,?,?)`, id, userID, orderID, account.ID, attempt, in.WorkerID, lease, now+int64(in.LeaseSeconds), goodsID, skuID, parsePurchasePositiveInt(quantityRaw, 1), amountCent(amountRaw), receiver, match.ProvinceName, match.CityName, match.DistrictName, match.Address, now, now, now)
	if err != nil {
		_, _ = s.Store.DB.ExecContext(r.Context(), `DELETE FROM pdd_account_locks WHERE pdd_account_id=? AND operation_id=?`, account.ID, id)
		writeErr(w, 409, "采购任务已被领取")
		return
	}
	_, _ = s.Store.DB.ExecContext(r.Context(), `UPDATE order_fulfillments SET purchase_requested_at=0,updated_at=? WHERE order_id=? AND user_id=?`, now, orderID, userID)
	v, _ := s.scanPurchaseTask(s.Store.DB.QueryRowContext(r.Context(), `SELECT `+purchaseTaskColumns+` FROM pdd_purchase_tasks WHERE id=?`, id))
	out := purchaseTaskJSON(v)
	out["lease_token"] = lease
	writeJSON(w, 201, out)
}

func (s *Server) heartbeatPurchaseTask(w http.ResponseWriter, r *http.Request) {
	var in struct {
		LeaseToken   string `json:"lease_token"`
		Status       string `json:"status"`
		LeaseSeconds int    `json:"lease_seconds"`
	}
	if decodeJSON(r, &in) != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	if in.LeaseSeconds < 30 || in.LeaseSeconds > 900 {
		in.LeaseSeconds = 120
	}
	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = "validating"
	}
	if status != "claimed" && status != "loading_goods" && status != "validating_goods" && status != "goods_validated" && status != "validating" && status != "browser_preparing" && status != "submitting_unpaid_order" {
		writeErr(w, 400, "任务状态无效")
		return
	}
	now, userID := time.Now().Unix(), auth.SessionFromContext(r.Context()).UserID
	res, err := s.Store.DB.ExecContext(r.Context(), `UPDATE pdd_purchase_tasks SET status=?,lease_expires_at=?,updated_at=? WHERE id=? AND user_id=? AND lease_token=? AND lease_expires_at>? AND status NOT IN ('completed','failed','aborted')`, status, now+int64(in.LeaseSeconds), now, chi.URLParam(r, "task_id"), userID, strings.TrimSpace(in.LeaseToken), now)
	if err != nil {
		writeErr(w, 500, "续租失败")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeErr(w, 409, "任务租约无效或已过期")
		return
	}
	_, _ = s.Store.DB.ExecContext(r.Context(), `UPDATE pdd_account_locks SET expires_at=? WHERE user_id=? AND operation_id=?`, now+int64(in.LeaseSeconds), userID, chi.URLParam(r, "task_id"))
	writeJSON(w, 200, map[string]any{"success": true, "lease_expires_at": now + int64(in.LeaseSeconds)})
}

func (s *Server) validatePurchaseGoods(w http.ResponseWriter, r *http.Request) {
	var in struct {
		LeaseToken string              `json:"lease_token"`
		Source     string              `json:"source"`
		CacheHit   bool                `json:"cache_hit"`
		Snapshot   pddproduct.Snapshot `json:"snapshot"`
	}
	if decodeJSON(r, &in) != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	userID, taskID, now := auth.SessionFromContext(r.Context()).UserID, chi.URLParam(r, "task_id"), time.Now().Unix()
	v, err := s.scanPurchaseTask(s.Store.DB.QueryRowContext(r.Context(), `SELECT `+purchaseTaskColumns+` FROM pdd_purchase_tasks WHERE id=? AND user_id=?`, taskID, userID))
	if err != nil {
		writeErr(w, 404, "采购任务不存在")
		return
	}
	var token string
	if s.Store.DB.QueryRowContext(r.Context(), `SELECT lease_token FROM pdd_purchase_tasks WHERE id=? AND lease_expires_at>?`, taskID, now).Scan(&token) != nil || token == "" || token != strings.TrimSpace(in.LeaseToken) {
		writeErr(w, 409, "任务租约无效")
		return
	}
	blocking, warnings := []string{}, []string{}
	if in.Snapshot.GoodsID != v.ExpectedGoodsID {
		blocking = append(blocking, "商品 goods_id 与映射不匹配")
	}
	var selected *pddproduct.SKU
	for i := range in.Snapshot.SKUs {
		if in.Snapshot.SKUs[i].SKUID == v.ExpectedSKUID {
			selected = &in.Snapshot.SKUs[i]
			break
		}
	}
	if selected == nil {
		blocking = append(blocking, "商品页不存在映射的 SKU")
	} else {
		if selected.GoodsID != v.ExpectedGoodsID {
			blocking = append(blocking, "SKU 所属商品不匹配")
		}
		if !selected.IsOnsale {
			blocking = append(blocking, "映射 SKU 已下架")
		}
		if selected.Stock < int64(v.ExpectedQuantity) {
			blocking = append(blocking, "映射 SKU 库存不足")
		}
		if selected.PriceCent <= 0 {
			blocking = append(blocking, "无法读取映射 SKU 实时价格")
		}
		if selected.PriceCent*int64(v.ExpectedQuantity) > v.ExpectedAmountCent-50 {
			blocking = append(blocking, "商品页预计利润低于 0.5 元")
		}
		var oldPrice, oldStock int64
		var oldOnsale int
		if s.Store.DB.QueryRowContext(r.Context(), `SELECT price_cent,stock,is_onsale FROM pdd_skus WHERE goods_id=? AND sku_id=?`, v.ExpectedGoodsID, v.ExpectedSKUID).Scan(&oldPrice, &oldStock, &oldOnsale) == nil {
			if oldPrice != selected.PriceCent {
				warnings = append(warnings, fmt.Sprintf("采购价变化：%d→%d 分", oldPrice, selected.PriceCent))
			}
			if oldStock != selected.Stock {
				warnings = append(warnings, fmt.Sprintf("库存变化：%d→%d", oldStock, selected.Stock))
			}
			if (oldOnsale == 1) != selected.IsOnsale {
				warnings = append(warnings, "上下架状态发生变化")
			}
		}
	}
	snapshotRaw, _ := json.Marshal(in.Snapshot)
	blockingRaw, _ := json.Marshal(blocking)
	warningRaw, _ := json.Marshal(warnings)
	_, err = s.Store.DB.ExecContext(r.Context(), `INSERT INTO pdd_purchase_goods_snapshots(id,user_id,task_id,pdd_account_id,goods_id,source,cache_hit,snapshot_json,blocking_errors_json,warnings_json,captured_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, uuid.NewString(), userID, taskID, v.PDDAccountID, v.ExpectedGoodsID, strings.TrimSpace(in.Source), boolInt(in.CacheHit), string(snapshotRaw), string(blockingRaw), string(warningRaw), now)
	if err != nil {
		writeErr(w, 500, "保存商品校验快照失败")
		return
	}
	if len(blocking) > 0 {
		msg := strings.Join(blocking, "；")
		s.createFulfillmentException(r, userID, v.OrderID, taskID, "purchase_goods_blocked", msg, map[string]any{"blocking": blocking, "warnings": warnings, "snapshot": in.Snapshot})
		writeErr(w, 422, msg)
		return
	}
	_, _ = s.Store.DB.ExecContext(r.Context(), `UPDATE pdd_purchase_tasks SET status='goods_validated',updated_at=? WHERE id=?`, now, taskID)
	writeJSON(w, 200, map[string]any{"success": true, "warnings": warnings, "snapshot": in.Snapshot})
}

type purchaseBrowserResult struct {
	LeaseToken     string   `json:"lease_token"`
	Status         string   `json:"status"`
	Error          string   `json:"error"`
	BeforeOrderSNs []string `json:"before_order_sns"`
	PDDOrder       struct {
		OrderID         string `json:"order_id"`
		GroupOrderID    string `json:"group_order_id"`
		GoodsID         string `json:"goods_id"`
		SKUID           string `json:"sku_id"`
		Quantity        int    `json:"quantity"`
		AddressID       string `json:"address_id"`
		AmountCent      int64  `json:"amount_cent"`
		OrderTime       int64  `json:"order_time"`
		PaymentDeadline int64  `json:"payment_deadline"`
		ReceiverName    string `json:"receiver_name"`
		Province        string `json:"province"`
		City            string `json:"city"`
		District        string `json:"district"`
		DetailAddress   string `json:"detail_address"`
	} `json:"pdd_order"`
}

func (s *Server) completePurchaseTask(w http.ResponseWriter, r *http.Request) {
	var in purchaseBrowserResult
	if decodeJSON(r, &in) != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	userID, taskID := auth.SessionFromContext(r.Context()).UserID, chi.URLParam(r, "task_id")
	v, err := s.scanPurchaseTask(s.Store.DB.QueryRowContext(r.Context(), `SELECT `+purchaseTaskColumns+` FROM pdd_purchase_tasks WHERE id=? AND user_id=?`, taskID, userID))
	if err != nil {
		writeErr(w, 404, "采购任务不存在")
		return
	}
	var storedToken string
	var leaseExpiresAt int64
	_ = s.Store.DB.QueryRowContext(r.Context(), `SELECT lease_token,lease_expires_at FROM pdd_purchase_tasks WHERE id=?`, taskID).Scan(&storedToken, &leaseExpiresAt)
	if storedToken == "" || storedToken != strings.TrimSpace(in.LeaseToken) || leaseExpiresAt <= time.Now().Unix() {
		writeErr(w, 409, "任务租约无效")
		return
	}
	if in.Status != "unpaid_order_created" {
		status := "failed"
		if in.Status == "result_unknown" {
			status = "result_unknown"
		}
		detail, _ := json.Marshal(in)
		_, _ = s.Store.DB.ExecContext(r.Context(), `UPDATE pdd_purchase_tasks SET status=?,last_error=?,pdd_order_json=?,lease_token='',lease_expires_at=0,finished_at=?,updated_at=? WHERE id=?`, status, strings.TrimSpace(in.Error), string(detail), time.Now().Unix(), time.Now().Unix(), taskID)
		_, _ = s.Store.DB.ExecContext(r.Context(), `DELETE FROM pdd_account_locks WHERE user_id=? AND operation_id=?`, userID, taskID)
		s.createFulfillmentException(r, userID, v.OrderID, taskID, status, strings.TrimSpace(in.Error), in)
		writeJSON(w, 200, map[string]any{"success": false, "status": status})
		return
	}
	problems := []string{}
	if v.Status != "submitting_unpaid_order" && v.Status != "reconciling_result" {
		problems = append(problems, "任务未进入提交待付款订单步骤")
	}
	if in.PDDOrder.OrderID == "" {
		problems = append(problems, "拼多多订单号为空")
	}
	if in.PDDOrder.GoodsID != v.ExpectedGoodsID {
		problems = append(problems, "goods_id 不匹配")
	}
	if in.PDDOrder.SKUID != v.ExpectedSKUID {
		problems = append(problems, "sku_id 不匹配")
	}
	if in.PDDOrder.Quantity != v.ExpectedQuantity {
		problems = append(problems, "数量不匹配")
	}
	if in.PDDOrder.AmountCent <= 0 {
		problems = append(problems, "拼多多实付金额无效")
	}
	if in.PDDOrder.OrderTime < v.StartedAt-5 || in.PDDOrder.OrderTime > time.Now().Unix()+300 {
		problems = append(problems, "拼多多订单创建时间不在本次任务窗口内")
	}
	var storedBefore []string
	_ = json.Unmarshal([]byte(v.BeforeOrderSNs), &storedBefore)
	for _, orderSN := range storedBefore {
		if orderSN == in.PDDOrder.OrderID {
			problems = append(problems, "拼多多订单已存在于创单前基线")
			break
		}
	}
	if pddcheckout.NormalizeAddress(in.PDDOrder.ReceiverName) != pddcheckout.NormalizeAddress(v.ExpectedReceiverName) || pddcheckout.NormalizeAddress(in.PDDOrder.Province) != pddcheckout.NormalizeAddress(v.ExpectedProvince) || pddcheckout.NormalizeAddress(in.PDDOrder.City) != pddcheckout.NormalizeAddress(v.ExpectedCity) || pddcheckout.NormalizeAddress(in.PDDOrder.District) != pddcheckout.NormalizeAddress(v.ExpectedDistrict) || pddcheckout.NormalizeAddress(in.PDDOrder.DetailAddress) != pddcheckout.NormalizeAddress(v.ExpectedDetailAddress) {
		problems = append(problems, "收货姓名或地址不匹配")
	}
	if in.PDDOrder.AmountCent > v.ExpectedAmountCent-50 {
		problems = append(problems, "预计利润低于 0.5 元")
	}
	detail, _ := json.Marshal(in.PDDOrder)
	before := []byte(v.BeforeOrderSNs)
	now := time.Now().Unix()
	if len(problems) > 0 {
		msg := strings.Join(problems, "；")
		_, _ = s.Store.DB.ExecContext(r.Context(), `UPDATE pdd_purchase_tasks SET status='failed',before_order_sns=?,pdd_order_json=?,last_error=?,lease_token='',lease_expires_at=0,finished_at=?,updated_at=? WHERE id=?`, string(before), string(detail), msg, now, now, taskID)
		s.createFulfillmentException(r, userID, v.OrderID, taskID, "purchase_mismatch", msg, in.PDDOrder)
		writeErr(w, 422, msg)
		return
	}
	s.purchaseMu.Lock()
	defer s.purchaseMu.Unlock()
	tx, err := s.Store.DB.BeginTx(r.Context(), nil)
	if err != nil {
		writeErr(w, 500, "开始回填事务失败")
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(r.Context(), `UPDATE pdd_purchase_tasks SET status='completed',before_order_sns=?,pdd_order_id=?,pdd_order_json=?,lease_token='',lease_expires_at=0,submitted_at=?,finished_at=?,updated_at=? WHERE id=? AND status NOT IN ('completed','aborted')`, string(before), in.PDDOrder.OrderID, string(detail), in.PDDOrder.OrderTime, now, now, taskID); err != nil {
		writeErr(w, 409, "拼多多订单号已经绑定")
		return
	}
	res, err := tx.ExecContext(r.Context(), `UPDATE order_fulfillments SET pdd_order_id=?,pdd_ordered=1,pdd_ordered_at=?,pdd_paid=0,pdd_paid_at=0,pdd_paid_source='',purchase_requested_at=0,updated_at=? WHERE order_id=? AND user_id=? AND pdd_order_id=''`, in.PDDOrder.OrderID, now, now, v.OrderID, userID)
	if err != nil {
		writeErr(w, 500, "回填拼多多订单号失败")
		return
	}
	if affected, _ := res.RowsAffected(); affected != 1 {
		writeErr(w, 409, "闲鱼订单已被其他采购任务绑定")
		return
	}
	_, _ = tx.ExecContext(r.Context(), `DELETE FROM pdd_account_locks WHERE user_id=? AND order_id=?`, userID, v.OrderID)
	if err = tx.Commit(); err != nil {
		writeErr(w, 500, "提交回填事务失败")
		return
	}
	writeJSON(w, 200, map[string]any{"success": true, "order_id": v.OrderID, "pdd_order_id": in.PDDOrder.OrderID, "pdd_ordered": true, "pdd_paid": false})
}

func (s *Server) abortPurchaseTask(w http.ResponseWriter, r *http.Request) {
	var in struct {
		LeaseToken string `json:"lease_token"`
		Reason     string `json:"reason"`
	}
	_ = decodeJSON(r, &in)
	userID := auth.SessionFromContext(r.Context()).UserID
	now := time.Now().Unix()
	res, err := s.Store.DB.ExecContext(r.Context(), `UPDATE pdd_purchase_tasks SET status='aborted',last_error=?,lease_token='',lease_expires_at=0,finished_at=?,updated_at=? WHERE id=? AND user_id=? AND lease_token=? AND status NOT IN ('completed','aborted')`, strings.TrimSpace(in.Reason), now, now, chi.URLParam(r, "task_id"), userID, strings.TrimSpace(in.LeaseToken))
	if err != nil {
		writeErr(w, 500, "中止任务失败")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeErr(w, 409, "任务租约无效或任务已结束")
		return
	}
	var orderID string
	_ = s.Store.DB.QueryRowContext(r.Context(), `SELECT order_id FROM pdd_purchase_tasks WHERE id=? AND user_id=?`, chi.URLParam(r, "task_id"), userID).Scan(&orderID)
	if orderID != "" {
		_, _ = s.Store.DB.ExecContext(r.Context(), `DELETE FROM pdd_account_locks WHERE user_id=? AND order_id=?`, userID, orderID)
	}
	writeJSON(w, 200, map[string]any{"success": true})
}

func (s *Server) confirmUnknownPurchaseCancelled(w http.ResponseWriter, r *http.Request) {
	userID, taskID, now := auth.SessionFromContext(r.Context()).UserID, chi.URLParam(r, "task_id"), time.Now().Unix()
	var orderID, pddOrderID, status string
	if err := s.Store.DB.QueryRowContext(r.Context(), `SELECT order_id,COALESCE(pdd_order_id,''),status FROM pdd_purchase_tasks WHERE id=? AND user_id=?`, taskID, userID).Scan(&orderID, &pddOrderID, &status); err != nil {
		writeErr(w, 404, "采购任务不存在")
		return
	}
	if status != "result_unknown" || pddOrderID != "" {
		writeErr(w, 409, "只有未回填订单号的未知结果任务可以确认取消")
		return
	}
	result, err := s.Store.DB.ExecContext(r.Context(), `UPDATE pdd_purchase_tasks SET status='aborted',last_error='人工确认旧拼多多订单已取消，允许重新采购',worker_id='',lease_token='',lease_expires_at=0,finished_at=?,updated_at=? WHERE id=? AND user_id=? AND status='result_unknown' AND COALESCE(pdd_order_id,'')=''`, now, now, taskID, userID)
	if err != nil {
		writeErr(w, 500, "更新采购任务失败")
		return
	}
	updated, _ := result.RowsAffected()
	if updated != 1 {
		writeErr(w, 409, "采购任务状态已变化")
		return
	}
	_, _ = s.Store.DB.ExecContext(r.Context(), `DELETE FROM pdd_account_locks WHERE user_id=? AND operation_id=?`, userID, taskID)
	_, _ = s.Store.DB.ExecContext(r.Context(), `UPDATE order_fulfillments SET purchase_requested_at=?,last_error='',updated_at=? WHERE order_id=? AND user_id=? AND pdd_order_id='' AND pdd_ordered=0`, now, now, orderID, userID)
	writeJSON(w, 200, map[string]any{"success": true, "order_id": orderID, "status": "queued"})
}

func (s *Server) parseUnpaidSnapshot(w http.ResponseWriter, r *http.Request) {
	userID, taskID := auth.SessionFromContext(r.Context()).UserID, chi.URLParam(r, "task_id")
	v, err := s.scanPurchaseTask(s.Store.DB.QueryRowContext(r.Context(), `SELECT `+purchaseTaskColumns+` FROM pdd_purchase_tasks WHERE id=? AND user_id=?`, taskID, userID))
	if err != nil {
		writeErr(w, 404, "采购任务不存在")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, 400, "读取待付款页面失败")
		return
	}
	orders, err := pddcheckout.ParseUnpaidHTML(body)
	if err != nil {
		writeErr(w, 422, err.Error())
		return
	}
	before := map[string]bool{}
	var beforeIDs []string
	_ = json.Unmarshal([]byte(v.BeforeOrderSNs), &beforeIDs)
	for _, id := range beforeIDs {
		before[id] = true
	}
	if r.URL.Query().Get("phase") == "before" {
		leaseToken := strings.TrimSpace(r.URL.Query().Get("lease_token"))
		if !canSavePurchaseBaseline(v.Status) {
			writeErr(w, 409, "当前任务状态不能保存待付款基线")
			return
		}
		ids := make([]string, 0, len(orders))
		for _, order := range orders {
			ids = append(ids, order.OrderID)
		}
		raw, _ := json.Marshal(ids)
		res, updateErr := s.Store.DB.ExecContext(r.Context(), `UPDATE pdd_purchase_tasks SET before_order_sns=?,status='browser_preparing',updated_at=? WHERE id=? AND user_id=? AND lease_token=? AND lease_expires_at>? AND status=?`, string(raw), time.Now().Unix(), taskID, userID, leaseToken, time.Now().Unix(), v.Status)
		if updateErr != nil {
			writeErr(w, 500, "保存待付款基线失败")
			return
		}
		if affected, _ := res.RowsAffected(); affected != 1 {
			writeErr(w, 409, "任务租约无效或已过期")
			return
		}
		writeJSON(w, 200, map[string]any{"orders": orders, "before_order_sns": ids, "saved": true})
		return
	}
	candidates := []pddcheckout.Order{}
	for _, order := range orders {
		if !before[order.OrderID] && order.GoodsID == v.ExpectedGoodsID && order.SKUID == v.ExpectedSKUID && int(order.Quantity) == v.ExpectedQuantity {
			candidates = append(candidates, order)
		}
	}
	writeJSON(w, 200, map[string]any{"orders": orders, "candidates": candidates, "candidate_count": len(candidates)})
}

func (s *Server) confirmPurchasePayment(w http.ResponseWriter, r *http.Request) {
	userID, taskID := auth.SessionFromContext(r.Context()).UserID, chi.URLParam(r, "task_id")
	var in struct {
		PDDOrderID string `json:"pdd_order_id"`
	}
	_ = decodeJSON(r, &in)
	var orderID, pddOrderID, status string
	err := s.Store.DB.QueryRowContext(r.Context(), `SELECT order_id,COALESCE(pdd_order_id,''),status FROM pdd_purchase_tasks WHERE id=? AND user_id=?`, taskID, userID).Scan(&orderID, &pddOrderID, &status)
	if err != nil {
		writeErr(w, 404, "采购任务不存在")
		return
	}
	if status != "completed" || pddOrderID == "" {
		writeErr(w, 409, "采购任务尚未唯一回填待付款订单")
		return
	}
	if strings.TrimSpace(in.PDDOrderID) != "" && strings.TrimSpace(in.PDDOrderID) != pddOrderID {
		writeErr(w, 409, "拼多多订单号不匹配")
		return
	}
	now := time.Now().Unix()
	res, err := s.Store.DB.ExecContext(r.Context(), `UPDATE order_fulfillments SET pdd_ordered=1,pdd_paid=1,pdd_paid_at=?,pdd_paid_source='manual',updated_at=? WHERE user_id=? AND order_id=? AND pdd_order_id=?`, now, now, userID, orderID, pddOrderID)
	if err != nil {
		writeErr(w, 500, "确认付款失败")
		return
	}
	if affected, _ := res.RowsAffected(); affected != 1 {
		writeErr(w, 409, "履约记录或拼多多订单号已变化")
		return
	}
	_, _ = s.Store.DB.ExecContext(r.Context(), `UPDATE pdd_purchase_tasks SET status='paid_confirmed',updated_at=? WHERE id=? AND user_id=?`, now, taskID, userID)
	_, _ = s.Store.DB.ExecContext(r.Context(), `DELETE FROM pdd_account_locks WHERE user_id=? AND order_id=?`, userID, orderID)
	writeJSON(w, 200, map[string]any{"success": true, "order_id": orderID, "pdd_order_id": pddOrderID, "pdd_ordered": true, "pdd_paid": true, "pdd_paid_source": "manual"})
}

func (s *Server) createFulfillmentException(r *http.Request, userID int64, orderID, taskID, eventType, summary string, detail any) {
	raw, _ := json.Marshal(detail)
	_, _ = s.Store.DB.ExecContext(r.Context(), `INSERT INTO fulfillment_exception_events(id,user_id,order_id,task_id,event_type,summary,detail_json,status,notification_status,created_at) VALUES(?,?,?,?,?,?,?,'open','pending',?)`, uuid.NewString(), userID, orderID, taskID, eventType, summary, string(raw), time.Now().Unix())
}

func (s *Server) listFulfillmentExceptions(w http.ResponseWriter, r *http.Request) {
	userID := auth.SessionFromContext(r.Context()).UserID
	query, args := `SELECT id,order_id,task_id,event_type,summary,detail_json,status,notification_status,created_at,resolved_at,read_at FROM fulfillment_exception_events WHERE user_id=?`, []any{userID}
	if status := strings.TrimSpace(r.URL.Query().Get("status")); status == "open" || status == "resolved" {
		query += ` AND status=?`
		args = append(args, status)
	}
	query += ` ORDER BY created_at DESC LIMIT 200`
	rows, err := s.Store.DB.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeErr(w, 500, "读取履约异常失败")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, oid, tid, typ, sum, detail, status, notify string
		var created, resolved, readAt int64
		if rows.Scan(&id, &oid, &tid, &typ, &sum, &detail, &status, &notify, &created, &resolved, &readAt) == nil {
			out = append(out, map[string]any{"id": id, "order_id": oid, "task_id": tid, "event_type": typ, "summary": sum, "detail": json.RawMessage(emptyJSONObject(detail)), "status": status, "notification_status": notify, "created_at": created, "resolved_at": resolved, "read_at": readAt})
		}
	}
	writeJSON(w, 200, out)
}

func (s *Server) readFulfillmentExceptions(w http.ResponseWriter, r *http.Request) {
	userID, now := auth.SessionFromContext(r.Context()).UserID, time.Now().Unix()
	result, err := s.Store.DB.ExecContext(r.Context(), `UPDATE fulfillment_exception_events SET read_at=? WHERE user_id=? AND read_at=0`, now, userID)
	if err != nil {
		writeErr(w, 500, "标记履约异常已读失败")
		return
	}
	updated, _ := result.RowsAffected()
	writeJSON(w, 200, map[string]any{"success": true, "updated": updated, "read_at": now})
}

func (s *Server) resolveFulfillmentException(w http.ResponseWriter, r *http.Request) {
	userID, now := auth.SessionFromContext(r.Context()).UserID, time.Now().Unix()
	result, err := s.Store.DB.ExecContext(r.Context(), `UPDATE fulfillment_exception_events SET status='resolved',resolved_at=? WHERE id=? AND user_id=? AND status='open'`, now, chi.URLParam(r, "event_id"), userID)
	if err != nil {
		writeErr(w, 500, "处理履约异常失败")
		return
	}
	updated, _ := result.RowsAffected()
	if updated == 0 {
		writeErr(w, 404, "履约异常不存在或已解决")
		return
	}
	writeJSON(w, 200, map[string]any{"success": true})
}

// clearFulfillmentExceptions clears operational exception logs only. Shipping
// operations, address operations and SKU mapping audits live in separate audit
// tables and are deliberately never touched here.
func (s *Server) clearFulfillmentExceptions(w http.ResponseWriter, r *http.Request) {
	userID := auth.SessionFromContext(r.Context()).UserID
	query := `DELETE FROM fulfillment_exception_events WHERE user_id=?`
	args := []any{userID}
	if r.URL.Query().Get("scope") == "resolved" {
		query += ` AND status='resolved'`
	}
	result, err := s.Store.DB.ExecContext(r.Context(), query, args...)
	if err != nil {
		writeErr(w, 500, "清理履约异常日志失败")
		return
	}
	deleted, _ := result.RowsAffected()
	writeJSON(w, 200, map[string]any{"success": true, "deleted": deleted})
}
