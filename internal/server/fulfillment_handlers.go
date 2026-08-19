package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
	"xianyu-go/internal/pddaddress"
)

type fulfillmentPatch struct {
	PDDOrdered       *bool   `json:"pdd_ordered"`
	PDDPaid          *bool   `json:"pdd_paid"`
	PDDOrderID       *string `json:"pdd_order_id"`
	PDDShipped       *bool   `json:"pdd_shipped"`
	LogisticsCompany *string `json:"logistics_company"`
	TrackingNumber   *string `json:"tracking_number"`
	XianyuShipped    *bool   `json:"xianyu_shipped"`
	Reminded         *bool   `json:"reminded"`
}

func (s *Server) mountFulfillment(r chi.Router) {
	s.mountPDDMessages(r)
	r.Get("/api/fulfillment/orders", s.listFulfillments)
	r.Post("/api/fulfillment/reconcile", s.reconcileFulfillments)
	r.Get("/api/fulfillment/history-repair/preview", s.previewFulfillmentHistoryRepair)
	r.Post("/api/fulfillment/history-repair", s.repairFulfillmentHistory)
	r.Put("/api/fulfillment/orders/{order_id}", s.updateFulfillment)
	r.Post("/api/fulfillment/orders/{order_id}/purchase-request", s.requestPurchase)
	r.Post("/api/fulfillment/orders/{order_id}/address-preview", s.previewFulfillmentAddress)
	r.Post("/api/fulfillment/orders/{order_id}/pdd-address/apply", s.applyPDDAddress)
	r.Delete("/api/fulfillment/orders/{order_id}/pdd-address/lock", s.releasePDDAddressLock)
	r.Get("/api/fulfillment/purchase-tasks", s.listPurchaseTasks)
	r.Post("/api/fulfillment/purchase-tasks/claim", s.claimPurchaseTask)
	r.Post("/api/fulfillment/purchase-tasks/{task_id}/heartbeat", s.heartbeatPurchaseTask)
	r.Post("/api/fulfillment/purchase-tasks/{task_id}/goods-validation", s.validatePurchaseGoods)
	r.Post("/api/fulfillment/purchase-tasks/{task_id}/browser-result", s.completePurchaseTask)
	r.Post("/api/fulfillment/purchase-tasks/{task_id}/unpaid-snapshot", s.parseUnpaidSnapshot)
	r.Post("/api/fulfillment/purchase-tasks/{task_id}/confirm-payment", s.confirmPurchasePayment)
	r.Post("/api/fulfillment/purchase-tasks/{task_id}/confirm-cancelled", s.confirmUnknownPurchaseCancelled)
	r.Post("/api/fulfillment/purchase-tasks/{task_id}/abort", s.abortPurchaseTask)
	r.Get("/api/fulfillment/exceptions", s.listFulfillmentExceptions)
	r.Put("/api/fulfillment/exceptions/read", s.readFulfillmentExceptions)
	r.Delete("/api/fulfillment/exceptions", s.clearFulfillmentExceptions)
	r.Put("/api/fulfillment/exceptions/{event_id}/resolve", s.resolveFulfillmentException)
	r.Post("/api/fulfillment/logistics/snapshot", s.ingestPDDLogistics)
	r.Post("/api/fulfillment/orders/{order_id}/shipping-precheck", s.shippingPrecheck)
	r.Post("/api/fulfillment/orders/{order_id}/ship", s.createShippingOperation)
	r.Get("/api/fulfillment/shipping-accounts", s.listShippingAccounts)
	r.Put("/api/fulfillment/shipping-accounts/{cookie_id}", s.saveShippingAccount)
	r.Post("/api/fulfillment/shipping-accounts/{cookie_id}/sync", s.syncShippingAddresses)
}

func (s *Server) ensureFulfillment(r *http.Request, order db.Order, userID int64) error {
	now := time.Now().Unix()
	insertPrefix := db.DialectInsertIgnorePrefix(s.Store.Dialect)
	insertSuffix := db.DialectInsertIgnore(s.Store.Dialect, []string{"order_id"})
	if _, err := s.Store.DB.ExecContext(r.Context(), insertPrefix+` INTO order_fulfillments(order_id,user_id,cookie_id,item_id,created_at,updated_at) VALUES(?,?,?,?,?,?)`+insertSuffix, order.OrderID, userID, order.CookieID, order.ItemID, now, now); err != nil {
		return err
	}
	// Successful physical shipping is irreversible locally. A later order refresh
	// may advance this flag, but stale platform data must never reset it.
	_, err := s.Store.DB.ExecContext(r.Context(), `UPDATE order_fulfillments SET cookie_id=?,item_id=?,xianyu_shipped=CASE WHEN xianyu_shipped=1 OR ?=1 THEN 1 ELSE 0 END,xianyu_shipped_at=CASE WHEN ?=1 AND xianyu_shipped_at=0 THEN ? ELSE xianyu_shipped_at END,updated_at=? WHERE order_id=? AND user_id=?`, order.CookieID, order.ItemID, boolInt(order.SystemShipped), boolInt(order.SystemShipped), now, now, order.OrderID, userID)
	if err == nil {
		s.resolveFulfillmentSKU(r, order, userID)
	}
	return err
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func (s *Server) resolveFulfillmentSKU(r *http.Request, order db.Order, userID int64) {
	type candidate struct {
		materialID, id                                         int64
		materialSKU, goodsID, sourceSKU, xianyuSKU, properties string
	}
	candidates := []candidate{}
	// 商品管理中的人工绑定是显式覆盖；没有人工绑定时继续使用发布时自动生成的映射。
	manualRows, manualErr := s.Store.DB.QueryContext(r.Context(), `SELECT id,source_goods_id,source_sku_id,xianyu_sku_id,xianyu_properties_json FROM item_pdd_sku_mappings WHERE user_id=? AND cookie_id=? AND item_id=? ORDER BY id`, userID, order.CookieID, order.ItemID)
	if manualErr == nil {
		for manualRows.Next() {
			var row candidate
			if manualRows.Scan(&row.id, &row.goodsID, &row.sourceSKU, &row.xianyuSKU, &row.properties) == nil {
				candidates = append(candidates, row)
			}
		}
		_ = manualRows.Close()
	}
	if len(candidates) == 0 {
		rows, err := s.Store.DB.QueryContext(r.Context(), `SELECT p.material_id,m.id,m.material_sku_id,m.source_goods_id,m.source_sku_id,m.xianyu_sku_id,m.published_properties_json FROM material_publish_records p JOIN material_publish_sku_mappings m ON m.publish_record_id=p.id WHERE p.id=(SELECT MAX(latest.id) FROM material_publish_records latest WHERE latest.user_id=? AND latest.cookie_id=? AND latest.published_item_id=? AND latest.status='success') ORDER BY m.id`, userID, order.CookieID, order.ItemID)
		if err != nil {
			return
		}
		defer rows.Close()
		for rows.Next() {
			var row candidate
			if rows.Scan(&row.materialID, &row.id, &row.materialSKU, &row.goodsID, &row.sourceSKU, &row.xianyuSKU, &row.properties) == nil {
				candidates = append(candidates, row)
			}
		}
	}
	matched := []candidate{}
	for _, candidate := range candidates {
		var properties []materialProperty
		_ = json.Unmarshal([]byte(candidate.properties), &properties)
		if len(candidates) == 1 || fulfillmentPropertiesMatch(properties, order.SpecName, order.SpecValue) {
			matched = append(matched, candidate)
		}
	}
	status := "unmapped"
	var selected candidate
	if len(matched) == 1 {
		status, selected = "mapped", matched[0]
	} else if len(matched) > 1 {
		status = "ambiguous"
	} else if len(candidates) == 0 {
		status = "pending"
	}
	_, _ = s.Store.DB.ExecContext(r.Context(), `UPDATE order_fulfillments SET material_id=?,material_sku_id=?,source_goods_id=?,source_sku_id=?,xianyu_sku_id=?,mapping_status=?,updated_at=? WHERE order_id=? AND user_id=?`, selected.materialID, selected.materialSKU, selected.goodsID, selected.sourceSKU, selected.xianyuSKU, status, time.Now().Unix(), order.OrderID, userID)
}

func fulfillmentPropertiesMatch(properties []materialProperty, specName, specValue string) bool {
	name, value := strings.TrimSpace(specName), strings.TrimSpace(specValue)
	if value == "" {
		return false
	}
	for _, property := range properties {
		if strings.TrimSpace(property.Value) == value && (name == "" || strings.TrimSpace(property.Name) == name) {
			return true
		}
	}
	return false
}

func (s *Server) reconcileFulfillments(w http.ResponseWriter, r *http.Request) {
	userID := auth.SessionFromContext(r.Context()).UserID
	updated, err := s.reconcileUserFulfillments(r, userID)
	if err != nil {
		writeErr(w, 500, "读取订单失败")
		return
	}
	writeJSON(w, 200, map[string]any{"success": true, "updated": updated})
}

func (s *Server) reconcileUserFulfillments(r *http.Request, userID int64) (int, error) {
	rows, err := s.Store.DB.QueryContext(r.Context(), `SELECT o.order_id FROM orders o JOIN cookies c ON c.id=o.cookie_id WHERE c.user_id=? AND o.deleted_at IS NULL AND o.order_status<>'cancelled'`, userID)
	if err != nil {
		return 0, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	_ = rows.Close()
	updated := 0
	for _, id := range ids {
		order, err := s.Store.Orders.Get(r.Context(), id)
		if err != nil {
			return updated, err
		}
		if err := s.ensureFulfillment(r, *order, userID); err != nil {
			return updated, err
		}
		updated++
	}
	return updated, nil
}

func (s *Server) listFulfillments(w http.ResponseWriter, r *http.Request) {
	userID := auth.SessionFromContext(r.Context()).UserID
	if _, err := s.reconcileUserFulfillments(r, userID); err != nil {
		writeErr(w, 500, "同步履约订单失败")
		return
	}
	query := `SELECT f.order_id,f.cookie_id,f.item_id,COALESCE(o.spec_name,''),COALESCE(o.spec_value,''),COALESCE(o.receiver_name,''),COALESCE(o.receiver_phone,''),COALESCE(o.receiver_address,''),COALESCE(o.receiver_city,''),f.material_id,f.material_sku_id,f.source_goods_id,f.source_sku_id,f.xianyu_sku_id,f.mapping_status,f.pdd_ordered,f.pdd_paid,f.pdd_paid_at,f.pdd_paid_source,f.pdd_order_id,COALESCE((SELECT t.pdd_order_json FROM pdd_purchase_tasks t WHERE t.user_id=f.user_id AND t.order_id=f.order_id AND t.pdd_order_id=f.pdd_order_id AND t.pdd_order_json<>'' ORDER BY t.attempt DESC LIMIT 1),''),f.pdd_shipped,f.logistics_company,f.tracking_number,f.xianyu_shipped,f.reminded,f.fulfillment_exempt,f.reminder_exempt,f.manual_modified_at,f.history_repaired_at,f.phone_restore_due_at,f.address_match_status,f.last_error,f.purchase_requested_at,f.updated_at FROM order_fulfillments f JOIN orders o ON o.order_id=f.order_id WHERE f.user_id=? AND o.deleted_at IS NULL AND o.order_status<>'cancelled'`
	args := []any{userID}
	for _, field := range []string{"pdd_ordered", "pdd_paid", "pdd_shipped", "xianyu_shipped", "reminded"} {
		value, present, err := fulfillmentBoolFilter(r.URL.Query().Get(field))
		if err != nil {
			writeErr(w, http.StatusBadRequest, field+" 只支持 true、false、1 或 0")
			return
		}
		if present {
			query += " AND f." + field + "=?"
			args = append(args, value)
			if value == 0 {
				if field == "reminded" {
					query += " AND f.reminder_exempt=0"
				} else {
					query += " AND f.fulfillment_exempt=0"
				}
			}
		}
	}
	if value := strings.TrimSpace(r.URL.Query().Get("mapping_status")); value != "" {
		query += ` AND f.mapping_status=?`
		args = append(args, value)
	}
	query += ` ORDER BY f.updated_at DESC LIMIT 500`
	rows, err := s.Store.DB.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeErr(w, 500, "查询履约订单失败")
		return
	}
	defer rows.Close()
	result := []map[string]any{}
	for rows.Next() {
		var orderID, cookieID, itemID, specName, specValue, receiverName, receiverPhone, receiverAddress, receiverCity, materialSKU, goodsID, sourceSKU, xianyuSKU, mappingStatus, pddPaidSource, pddOrderID, pddOrderJSON, logistics, tracking, addressStatus, lastError string
		var materialID, pddPaidAt, manualModifiedAt, repairedAt, restoreDue, purchaseRequestedAt, updated int64
		var pddOrdered, pddPaid, pddShipped, xianyuShipped, reminded, fulfillmentExempt, reminderExempt int
		if rows.Scan(&orderID, &cookieID, &itemID, &specName, &specValue, &receiverName, &receiverPhone, &receiverAddress, &receiverCity, &materialID, &materialSKU, &goodsID, &sourceSKU, &xianyuSKU, &mappingStatus, &pddOrdered, &pddPaid, &pddPaidAt, &pddPaidSource, &pddOrderID, &pddOrderJSON, &pddShipped, &logistics, &tracking, &xianyuShipped, &reminded, &fulfillmentExempt, &reminderExempt, &manualModifiedAt, &repairedAt, &restoreDue, &addressStatus, &lastError, &purchaseRequestedAt, &updated) == nil {
			result = append(result, map[string]any{"order_id": orderID, "cookie_id": cookieID, "item_id": itemID, "spec_name": specName, "spec_value": specValue, "receiver_name": receiverName, "receiver_phone": receiverPhone, "receiver_address": receiverAddress, "receiver_city": receiverCity, "material_id": materialID, "material_sku_id": materialSKU, "source_goods_id": goodsID, "source_sku_id": sourceSKU, "xianyu_sku_id": xianyuSKU, "mapping_status": mappingStatus, "pdd_ordered": pddOrdered != 0, "pdd_paid": pddPaid != 0, "pdd_paid_at": pddPaidAt, "pdd_paid_source": pddPaidSource, "pdd_order_id": pddOrderID, "pdd_order": json.RawMessage(emptyJSONObject(pddOrderJSON)), "pdd_shipped": pddShipped != 0, "logistics_company": logistics, "tracking_number": tracking, "xianyu_shipped": xianyuShipped != 0, "reminded": reminded != 0, "fulfillment_exempt": fulfillmentExempt != 0, "reminder_exempt": reminderExempt != 0, "manual_modified_at": manualModifiedAt, "history_repaired_at": repairedAt, "phone_restore_due_at": restoreDue, "address_match_status": addressStatus, "last_error": lastError, "purchase_requested_at": purchaseRequestedAt, "updated_at": updated})
		}
	}
	writeJSON(w, 200, result)
}

func fulfillmentBoolFilter(raw string) (value int, present bool, err error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return 0, false, nil
	case "0", "false":
		return 0, true, nil
	case "1", "true":
		return 1, true, nil
	default:
		return 0, false, errors.New("invalid boolean filter")
	}
}

func (s *Server) requestPurchase(w http.ResponseWriter, r *http.Request) {
	orderID := chi.URLParam(r, "order_id")
	order, ok := s.requireOrderOwner(w, r, orderID)
	if !ok {
		return
	}
	userID := auth.SessionFromContext(r.Context()).UserID
	if err := s.ensureFulfillment(r, *order, userID); err != nil {
		writeErr(w, 500, "初始化履约记录失败")
		return
	}
	if order.OrderStatus != "processing" && order.OrderStatus != "pending_ship" {
		writeErr(w, 422, "闲鱼订单不是可采购状态")
		return
	}
	var mapping, goodsID, skuID, pddOrderID string
	var exempt, pddOrdered int
	if err := s.Store.DB.QueryRowContext(r.Context(), `SELECT mapping_status,source_goods_id,source_sku_id,pdd_order_id,fulfillment_exempt,pdd_ordered FROM order_fulfillments WHERE order_id=? AND user_id=?`, orderID, userID).Scan(&mapping, &goodsID, &skuID, &pddOrderID, &exempt, &pddOrdered); err != nil {
		writeErr(w, 404, "履约记录不存在")
		return
	}
	problems := []string{}
	if exempt != 0 {
		problems = append(problems, "该订单已标记为无需履约")
	}
	if mapping != "mapped" || goodsID == "" || skuID == "" {
		problems = append(problems, "SKU 尚未完成唯一映射")
	}
	if pddOrdered != 0 || pddOrderID != "" {
		problems = append(problems, "该订单已经存在拼多多采购结果")
	}
	account, accountErr := s.Store.PDDAccounts.Default(r.Context(), userID)
	if accountErr != nil || !account.Enabled {
		problems = append(problems, "请先配置并启用默认拼多多账号")
	}
	var active int
	var recovery int
	_ = s.Store.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM pdd_purchase_tasks WHERE user_id=? AND order_id=? AND status NOT IN ('failed','aborted','completed','result_unknown')`, userID, orderID).Scan(&active)
	_ = s.Store.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM pdd_purchase_tasks WHERE user_id=? AND order_id=? AND status='result_unknown' AND COALESCE(pdd_order_id,'')=''`, userID, orderID).Scan(&recovery)
	if active > 0 {
		problems = append(problems, "该订单已有执行中的采购任务")
	}
	if len(problems) > 0 {
		writeJSON(w, 422, map[string]any{"success": false, "detail": strings.Join(problems, "；"), "problems": problems})
		return
	}
	now := time.Now().Unix()
	if _, err := s.Store.DB.ExecContext(r.Context(), `UPDATE order_fulfillments SET purchase_requested_at=?,last_error='',updated_at=? WHERE order_id=? AND user_id=?`, now, now, orderID, userID); err != nil {
		writeErr(w, 500, "加入采购队列失败")
		return
	}
	status := "queued"
	if recovery > 0 {
		status = "recovery_queued"
	}
	writeJSON(w, 200, map[string]any{"success": true, "order_id": orderID, "status": status, "purchase_requested_at": now})
}

func (s *Server) updateFulfillment(w http.ResponseWriter, r *http.Request) {
	orderID := chi.URLParam(r, "order_id")
	order, ok := s.requireOrderOwner(w, r, orderID)
	if !ok {
		return
	}
	userID := auth.SessionFromContext(r.Context()).UserID
	if err := s.ensureFulfillment(r, *order, userID); err != nil {
		writeErr(w, 500, "初始化履约记录失败")
		return
	}
	var in fulfillmentPatch
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in) != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	set, args := []string{}, []any{}
	addBool := func(column string, value *bool, atColumn string) {
		if value == nil {
			return
		}
		set, args = append(set, column+"=?"), append(args, boolInt(*value))
		if atColumn != "" {
			set = append(set, atColumn+"=?")
			if *value {
				args = append(args, time.Now().Unix())
			} else {
				args = append(args, 0)
			}
		}
	}
	addString := func(column string, value *string) {
		if value != nil {
			set, args = append(set, column+"=?"), append(args, strings.TrimSpace(*value))
		}
	}
	addBool("pdd_ordered", in.PDDOrdered, "pdd_ordered_at")
	addBool("pdd_paid", in.PDDPaid, "pdd_paid_at")
	if in.PDDPaid != nil {
		set = append(set, "pdd_paid_source=?")
		if *in.PDDPaid {
			args = append(args, "manual")
		} else {
			args = append(args, "")
		}
	}
	addString("pdd_order_id", in.PDDOrderID)
	addBool("pdd_shipped", in.PDDShipped, "pdd_shipped_at")
	addString("logistics_company", in.LogisticsCompany)
	addString("tracking_number", in.TrackingNumber)
	addBool("xianyu_shipped", in.XianyuShipped, "xianyu_shipped_at")
	addBool("reminded", in.Reminded, "reminded_at")
	if len(set) == 0 {
		writeErr(w, 400, "没有可更新字段")
		return
	}
	manualFields := make([]string, 0, len(set))
	for _, assignment := range set {
		manualFields = append(manualFields, strings.TrimSuffix(assignment, "=?"))
	}
	set, args = append(set, "manual_modified_at=?", "manual_modified_fields=?", "updated_at=?"), append(args, time.Now().Unix(), strings.Join(manualFields, ","), time.Now().Unix(), orderID, userID)
	if _, err := s.Store.DB.ExecContext(r.Context(), `UPDATE order_fulfillments SET `+strings.Join(set, ",")+` WHERE order_id=? AND user_id=?`, args...); err != nil {
		writeErr(w, 500, "更新履约状态失败")
		return
	}
	if in.PDDOrderID != nil && strings.TrimSpace(*in.PDDOrderID) != "" {
		_, _ = s.Store.DB.ExecContext(r.Context(), `DELETE FROM pdd_account_locks WHERE user_id=? AND order_id=?`, userID, orderID)
	}
	writeJSON(w, 200, map[string]any{"success": true})
}

type fulfillmentHistoryRepairPreview struct {
	Eligible       int `json:"eligible"`
	ActiveExcluded int `json:"active_excluded"`
	ManualExcluded int `json:"manual_excluded"`
	PDDExcluded    int `json:"pdd_excluded"`
}

func (s *Server) fulfillmentHistoryRepairPreview(r *http.Request, userID int64) (fulfillmentHistoryRepairPreview, error) {
	var result fulfillmentHistoryRepairPreview
	err := s.Store.DB.QueryRowContext(r.Context(), `SELECT
		COALESCE(SUM(CASE WHEN o.order_status IN ('completed','cancelled') AND f.pdd_order_id='' AND f.manual_modified_at=0 AND f.pdd_ordered=0 AND f.pdd_shipped=0 AND f.xianyu_shipped=0 AND f.reminded=0 AND f.logistics_company='' AND f.tracking_number='' AND f.history_repaired_at=0 THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN o.order_status IN ('processing','pending_ship','refunding','shipped') THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN o.order_status IN ('completed','cancelled') AND (f.manual_modified_at>0 OR (f.pdd_order_id='' AND (f.pdd_ordered<>0 OR f.pdd_shipped<>0 OR f.xianyu_shipped<>0 OR f.reminded<>0 OR f.logistics_company<>'' OR f.tracking_number<>''))) THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN o.order_status IN ('completed','cancelled') AND f.pdd_order_id<>'' THEN 1 ELSE 0 END),0)
		FROM order_fulfillments f JOIN orders o ON o.order_id=f.order_id WHERE f.user_id=? AND o.deleted_at IS NULL`, userID).
		Scan(&result.Eligible, &result.ActiveExcluded, &result.ManualExcluded, &result.PDDExcluded)
	return result, err
}

func (s *Server) previewFulfillmentHistoryRepair(w http.ResponseWriter, r *http.Request) {
	userID := auth.SessionFromContext(r.Context()).UserID
	if _, err := s.reconcileUserFulfillments(r, userID); err != nil {
		writeErr(w, http.StatusInternalServerError, "生成修复预览失败")
		return
	}
	preview, err := s.fulfillmentHistoryRepairPreview(r, userID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "生成修复预览失败")
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (s *Server) repairFulfillmentHistory(w http.ResponseWriter, r *http.Request) {
	userID := auth.SessionFromContext(r.Context()).UserID
	now := time.Now().Unix()
	result, err := s.Store.DB.ExecContext(r.Context(), `UPDATE order_fulfillments SET fulfillment_exempt=1,reminder_exempt=1,history_repaired_at=?,updated_at=? WHERE user_id=? AND order_id IN (SELECT o.order_id FROM orders o WHERE o.order_id=order_fulfillments.order_id AND o.deleted_at IS NULL AND o.order_status IN ('completed','cancelled')) AND pdd_order_id='' AND manual_modified_at=0 AND pdd_ordered=0 AND pdd_shipped=0 AND xianyu_shipped=0 AND reminded=0 AND logistics_company='' AND tracking_number='' AND history_repaired_at=0`, now, now, userID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "修复历史订单失败")
		return
	}
	updated, _ := result.RowsAffected()
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "updated": updated})
}

func (s *Server) previewFulfillmentAddress(w http.ResponseWriter, r *http.Request) {
	orderID := chi.URLParam(r, "order_id")
	order, ok := s.requireOrderOwner(w, r, orderID)
	if !ok {
		return
	}
	userID := auth.SessionFromContext(r.Context()).UserID
	if err := s.ensureFulfillment(r, *order, userID); err != nil {
		writeErr(w, 500, "初始化履约记录失败")
		return
	}
	match, addressErr := pddaddress.Resolve(order.ReceiverCity, order.ReceiverAddr)
	temporaryPhone, phoneErr := pddaddress.TemporaryPhone(order.ReceiverPhone)
	if err := errors.Join(addressErr, phoneErr); err != nil {
		_, _ = s.Store.DB.ExecContext(r.Context(), `UPDATE order_fulfillments SET address_match_status='failed',last_error=?,updated_at=? WHERE order_id=? AND user_id=?`, err.Error(), time.Now().Unix(), orderID, userID)
		writeErr(w, 422, err.Error())
		return
	}
	now := time.Now().Unix()
	_, err := s.Store.DB.ExecContext(r.Context(), `UPDATE order_fulfillments SET province_id=?,city_id=?,district_id=?,temporary_phone=?,address_match_status='matched',last_error='',updated_at=? WHERE order_id=? AND user_id=?`, match.ProvinceID, match.CityID, match.DistrictID, temporaryPhone, now, orderID, userID)
	if err != nil {
		writeErr(w, 500, "保存地址预览失败")
		return
	}
	writeJSON(w, 200, map[string]any{"success": true, "name": order.ReceiverName, "original_phone": order.ReceiverPhone, "temporary_phone": temporaryPhone, "province_id": strconv.FormatInt(match.ProvinceID, 10), "province_name": match.ProvinceName, "city_id": strconv.FormatInt(match.CityID, 10), "city_name": match.CityName, "district_id": strconv.FormatInt(match.DistrictID, 10), "district_name": match.DistrictName, "address": match.Address, "check_region": true})
}

type pddAddressOperation struct {
	ID, OrderID, Status, PDDAddressID, PDDAccountID, ErrorMessage string
	HTTPStatus, CreatedAt, FinishedAt                             int64
}

func (s *Server) applyPDDAddress(w http.ResponseWriter, r *http.Request) {
	orderID := chi.URLParam(r, "order_id")
	order, ok := s.requireOrderOwner(w, r, orderID)
	if !ok {
		return
	}
	status := db.NormalizeOrderStatus(order.OrderStatus)
	if status != "processing" && status != "pending_ship" {
		writeErr(w, http.StatusConflict, "只有处理中或待发货订单可以修改拼多多地址")
		return
	}
	userID := auth.SessionFromContext(r.Context()).UserID
	if err := s.ensureFulfillment(r, *order, userID); err != nil {
		writeErr(w, http.StatusInternalServerError, "初始化履约记录失败")
		return
	}
	var pddOrderID string
	if err := s.Store.DB.QueryRowContext(r.Context(), `SELECT pdd_order_id FROM order_fulfillments WHERE order_id=? AND user_id=?`, orderID, userID).Scan(&pddOrderID); err != nil {
		writeErr(w, http.StatusInternalServerError, "读取履约记录失败")
		return
	}
	if strings.TrimSpace(pddOrderID) != "" {
		writeErr(w, http.StatusConflict, "已回填拼多多订单号，禁止再次修改下单地址")
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" || len(key) > 128 {
		writeErr(w, http.StatusBadRequest, "请提供长度不超过 128 的 Idempotency-Key")
		return
	}
	if previous, found := s.findPDDAddressOperation(r, userID, key); found {
		if previous.OrderID != orderID {
			writeErr(w, http.StatusConflict, "Idempotency-Key 已用于其他订单")
			return
		}
		s.writePDDAddressOperation(w, previous, true)
		return
	}
	match, addressErr := pddaddress.Resolve(order.ReceiverCity, order.ReceiverAddr)
	temporaryPhone, phoneErr := pddaddress.TemporaryPhone(order.ReceiverPhone)
	if err := errors.Join(addressErr, phoneErr); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if strings.TrimSpace(order.ReceiverName) == "" {
		writeErr(w, http.StatusUnprocessableEntity, "收货人姓名为空")
		return
	}
	updater, addressID, accountID, err := s.pddAddressUpdater(r, userID)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	target, _ := json.Marshal(map[string]any{"name": order.ReceiverName, "mobile": temporaryPhone, "province_id": match.ProvinceID, "city_id": match.CityID, "district_id": match.DistrictID, "address": match.Address})
	now, operationID := time.Now().Unix(), uuid.NewString()
	if err := s.claimPDDAddressLock(r, userID, accountID, orderID, operationID, now); err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	_, err = s.Store.DB.ExecContext(r.Context(), `INSERT INTO pdd_address_operations(id,user_id,order_id,idempotency_key,status,pdd_address_id,pdd_account_id,target_json,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, operationID, userID, orderID, key, "processing", addressID, accountID, string(target), now)
	if err != nil {
		if previous, found := s.findPDDAddressOperation(r, userID, key); found {
			if previous.OrderID != orderID {
				writeErr(w, http.StatusConflict, "Idempotency-Key 已用于其他订单")
				return
			}
			s.writePDDAddressOperation(w, previous, true)
			return
		}
		writeErr(w, http.StatusInternalServerError, "创建地址修改操作失败")
		_, _ = s.Store.DB.ExecContext(r.Context(), `DELETE FROM pdd_account_locks WHERE pdd_account_id=? AND operation_id=?`, accountID, operationID)
		return
	}
	_, _ = s.Store.DB.ExecContext(r.Context(), `UPDATE order_fulfillments SET province_id=?,city_id=?,district_id=?,temporary_phone=?,pdd_account_id=?,address_match_status='applying',last_error='',updated_at=? WHERE order_id=? AND user_id=?`, match.ProvinceID, match.CityID, match.DistrictID, temporaryPhone, accountID, now, orderID, userID)
	result, callErr := updater.Update(r.Context(), pddaddress.UpdateRequest{Name: order.ReceiverName, Mobile: temporaryPhone, Match: match})
	finalStatus, errorMessage := result.Status, ""
	if callErr != nil {
		finalStatus, errorMessage = "failed", callErr.Error()
	}
	if finalStatus == "" {
		finalStatus = "result_unknown"
	}
	finished := time.Now().Unix()
	_, _ = s.Store.DB.ExecContext(r.Context(), `UPDATE pdd_address_operations SET status=?,http_status=?,response_json=?,error_message=?,finished_at=? WHERE id=?`, finalStatus, result.HTTPStatus, result.ResponseBody, errorMessage, finished, operationID)
	_, _ = s.Store.DB.ExecContext(r.Context(), `UPDATE order_fulfillments SET address_match_status=?,last_error=?,updated_at=? WHERE order_id=? AND user_id=?`, finalStatus, errorMessage, finished, orderID, userID)
	if finalStatus == "failed" {
		_, _ = s.Store.DB.ExecContext(r.Context(), `DELETE FROM pdd_account_locks WHERE pdd_account_id=? AND operation_id=?`, accountID, operationID)
	}
	s.writePDDAddressOperation(w, pddAddressOperation{ID: operationID, OrderID: orderID, Status: finalStatus, PDDAddressID: addressID, PDDAccountID: accountID, ErrorMessage: errorMessage, HTTPStatus: int64(result.HTTPStatus), CreatedAt: now, FinishedAt: finished}, false)
}

func (s *Server) claimPDDAddressLock(r *http.Request, userID int64, accountID, orderID, operationID string, now int64) error {
	_, _ = s.Store.DB.ExecContext(r.Context(), `DELETE FROM pdd_account_locks WHERE expires_at<=?`, now)
	var lockedUser int64
	var lockedOrder string
	if err := s.Store.DB.QueryRowContext(r.Context(), `SELECT user_id,order_id FROM pdd_account_locks WHERE pdd_account_id=?`, accountID).Scan(&lockedUser, &lockedOrder); err == nil {
		if lockedUser == userID && lockedOrder == orderID {
			_, err = s.Store.DB.ExecContext(r.Context(), `UPDATE pdd_account_locks SET expires_at=? WHERE pdd_account_id=? AND user_id=? AND order_id=?`, now+30*60, accountID, userID, orderID)
			return err
		}
		return errors.New("拼多多账号正在处理订单 " + lockedOrder)
	}
	prefix := db.DialectInsertIgnorePrefix(s.Store.Dialect)
	suffix := db.DialectInsertIgnore(s.Store.Dialect, []string{"pdd_account_id"})
	result, err := s.Store.DB.ExecContext(r.Context(), prefix+` INTO pdd_account_locks(pdd_account_id,user_id,order_id,operation_id,locked_at,expires_at) VALUES(?,?,?,?,?,?)`+suffix, accountID, userID, orderID, operationID, now, now+30*60)
	if err != nil {
		return errors.New("获取拼多多账号操作锁失败")
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		_ = s.Store.DB.QueryRowContext(r.Context(), `SELECT order_id FROM pdd_account_locks WHERE pdd_account_id=?`, accountID).Scan(&lockedOrder)
		return errors.New("拼多多账号正在处理订单 " + lockedOrder)
	}
	return nil
}

func (s *Server) releasePDDAddressLock(w http.ResponseWriter, r *http.Request) {
	orderID := chi.URLParam(r, "order_id")
	if _, ok := s.requireOrderOwner(w, r, orderID); !ok {
		return
	}
	userID := auth.SessionFromContext(r.Context()).UserID
	result, err := s.Store.DB.ExecContext(r.Context(), `DELETE FROM pdd_account_locks WHERE user_id=? AND order_id=?`, userID, orderID)
	if err != nil {
		writeErr(w, 500, "释放账号锁失败")
		return
	}
	affected, _ := result.RowsAffected()
	writeJSON(w, 200, map[string]any{"success": true, "released": affected > 0})
}

func (s *Server) pddAddressUpdater(r *http.Request, userID int64) (pddaddress.Updater, string, string, error) {
	if s.PDDAddressUpdater != nil {
		return s.PDDAddressUpdater, "", "", nil
	}
	account, accountErr := s.Store.PDDAccounts.Default(r.Context(), userID)
	if accountErr == nil {
		if !account.Enabled {
			return nil, "", "", errors.New("拼多多账号已禁用")
		}
		config := pddaddress.Config{BaseURL: "https://mobile.pinduoduo.com", PDDUID: account.PDDUID, AddressID: account.DefaultAddressID, Cookie: account.Cookie, UserAgent: account.UserAgent}
		return pddaddress.NewClient(config), config.AddressID, account.ID, nil
	}
	if !errors.Is(accountErr, sql.ErrNoRows) {
		return nil, "", "", errors.New("读取拼多多账号失败")
	}
	return nil, "", "", errors.New("请先在设置中配置并启用拼多多账号")
}

func (s *Server) findPDDAddressOperation(r *http.Request, userID int64, key string) (pddAddressOperation, bool) {
	var operation pddAddressOperation
	err := s.Store.DB.QueryRowContext(r.Context(), `SELECT id,order_id,status,pdd_address_id,pdd_account_id,http_status,error_message,created_at,finished_at FROM pdd_address_operations WHERE user_id=? AND idempotency_key=?`, userID, key).Scan(&operation.ID, &operation.OrderID, &operation.Status, &operation.PDDAddressID, &operation.PDDAccountID, &operation.HTTPStatus, &operation.ErrorMessage, &operation.CreatedAt, &operation.FinishedAt)
	return operation, err == nil
}

func (s *Server) writePDDAddressOperation(w http.ResponseWriter, operation pddAddressOperation, replayed bool) {
	code := http.StatusOK
	if operation.Status == "processing" {
		code = http.StatusConflict
	}
	if operation.Status == "failed" {
		code = http.StatusBadGateway
	}
	if operation.Status == "result_unknown" {
		code = http.StatusAccepted
	}
	writeJSON(w, code, map[string]any{"success": operation.Status == "applied", "replayed": replayed, "operation_id": operation.ID, "order_id": operation.OrderID, "status": operation.Status, "pdd_address_id": operation.PDDAddressID, "pdd_account_id": operation.PDDAccountID, "http_status": operation.HTTPStatus, "error": operation.ErrorMessage, "created_at": operation.CreatedAt, "finished_at": operation.FinishedAt})
}
