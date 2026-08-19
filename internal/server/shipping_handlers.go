package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
	"xianyu-go/internal/pddshipping"
	"xianyu-go/internal/xianyu/mtop"
)

type offlineConsigner interface {
	ConsignOfflineContext(context.Context, string, mtop.ConsignOfflineRequest) (bool, []string, json.RawMessage, string, error)
}

type shippingAddressLister interface {
	FetchShippingAddressesContext(context.Context, string) ([]mtop.ShippingAddress, []string, string, error)
}

type consignAddressResolver interface {
	FetchConsignAddressContext(context.Context, string, string) (int64, []string, string, error)
}

var officialHotCarriers = map[string]string{"邮政快递包裹": "POSTB", "中通快递": "ZTO", "顺丰速运": "SF", "申通快递": "STO", "韵达快递": "YUNDA", "极兔速递": "HTKY", "圆通速递": "YTO", "EMS": "EMS"}

type logisticsSnapshot struct {
	Shipments []pddshipping.Shipment `json:"shipments"`
}

func (s *Server) ingestPDDLogistics(w http.ResponseWriter, r *http.Request) {
	var in logisticsSnapshot
	if decodeJSON(r, &in) != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	userID, now := auth.SessionFromContext(r.Context()).UserID, time.Now().Unix()
	updated, unmatched := 0, []string{}
	bound := map[string]string{}
	for _, shipment := range in.Shipments {
		shipment.OrderID, shipment.Company, shipment.TrackingNumber = strings.TrimSpace(shipment.OrderID), strings.TrimSpace(shipment.Company), strings.TrimSpace(shipment.TrackingNumber)
		if shipment.OrderID == "" || shipment.TrackingNumber == "" {
			continue
		}
		code := exactCarrierCode(shipment.Company)
		status := "needs_carrier_mapping"
		if code != "" {
			status = "pending_xianyu_shipping"
		}
		result, err := s.Store.DB.ExecContext(r.Context(), `UPDATE order_fulfillments SET pdd_shipped=1,pdd_shipped_at=CASE WHEN pdd_shipped_at=0 THEN ? ELSE pdd_shipped_at END,logistics_company=?,logistics_company_code=?,tracking_number=?,logistics_synced_at=?,shipping_status=?,shipping_last_error='',updated_at=? WHERE user_id=? AND pdd_order_id=? AND xianyu_shipped=0`, shipment.ShippedAt, shipment.Company, code, shipment.TrackingNumber, now, status, now, userID, shipment.OrderID)
		if err != nil {
			writeErr(w, 500, "保存拼多多物流失败")
			return
		}
		rows, _ := result.RowsAffected()
		if rows == 0 && shipment.GoodsID != "" && shipment.SKUID != "" {
			// Older purchase runs may have created the PDD order but failed before
			// persisting its number. Recover only from one exact active SKU match;
			// never guess when the same PDD SKU has multiple open Xianyu orders.
			candidates, queryErr := s.Store.DB.QueryContext(r.Context(), `SELECT f.order_id FROM order_fulfillments f JOIN orders o ON o.order_id=f.order_id WHERE f.user_id=? AND f.pdd_order_id='' AND f.source_goods_id=? AND f.source_sku_id=? AND f.mapping_status='mapped' AND f.xianyu_shipped=0 AND o.deleted_at IS NULL AND o.order_status IN ('pending_ship','paid','processing') ORDER BY f.created_at LIMIT 2`, userID, shipment.GoodsID, shipment.SKUID)
			if queryErr == nil {
				ids := []string{}
				for candidates.Next() {
					var id string
					if candidates.Scan(&id) == nil {
						ids = append(ids, id)
					}
				}
				_ = candidates.Close()
				if len(ids) == 1 {
					result, err = s.Store.DB.ExecContext(r.Context(), `UPDATE order_fulfillments SET pdd_ordered=1,pdd_order_id=?,pdd_ordered_at=CASE WHEN pdd_ordered_at=0 THEN ? ELSE pdd_ordered_at END,pdd_shipped=1,pdd_shipped_at=CASE WHEN pdd_shipped_at=0 THEN ? ELSE pdd_shipped_at END,logistics_company=?,logistics_company_code=?,tracking_number=?,logistics_synced_at=?,shipping_status=?,shipping_last_error='',updated_at=? WHERE order_id=? AND user_id=? AND pdd_order_id='' AND xianyu_shipped=0`, shipment.OrderID, now, shipment.ShippedAt, shipment.Company, code, shipment.TrackingNumber, now, status, now, ids[0], userID)
					if err == nil {
						rows, _ = result.RowsAffected()
						if rows == 1 {
							bound[shipment.OrderID] = ids[0]
						}
					}
				}
			}
		}
		if rows > 0 {
			updated += int(rows)
		} else {
			unmatched = append(unmatched, shipment.OrderID)
		}
	}
	writeJSON(w, 200, map[string]any{"success": true, "updated": updated, "recovered_bindings": bound, "unmatched_order_ids": unmatched})
}

func exactCarrierCode(name string) string { return officialHotCarriers[strings.TrimSpace(name)] }

func (s *Server) listShippingAccounts(w http.ResponseWriter, r *http.Request) {
	uid := auth.SessionFromContext(r.Context()).UserID
	rows, err := s.Store.DB.QueryContext(r.Context(), `SELECT c.id,COALESCE(c.remark,''),COALESCE(a.address_id,0),COALESCE(a.address_summary,''),COALESCE(a.verified_at,0) FROM cookies c LEFT JOIN xianyu_shipping_accounts a ON a.cookie_id=c.id AND a.user_id=c.user_id WHERE c.user_id=? ORDER BY c.id`, uid)
	if err != nil {
		writeErr(w, 500, "查询发货地址配置失败")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, remark, summary string
		var address, verified int64
		if rows.Scan(&id, &remark, &address, &summary, &verified) == nil {
			addresses := []map[string]any{}
			addressRows, addressErr := s.Store.DB.QueryContext(r.Context(), `SELECT contact_id,area_id,contact_name,mobile_phone,province_name,city_name,district_name,detail_address,platform_default,last_synced_at FROM xianyu_shipping_addresses WHERE cookie_id=? AND user_id=? ORDER BY platform_default DESC,contact_id`, id, uid)
			if addressErr == nil {
				for addressRows.Next() {
					var contactID, areaID, synced int64
					var name, phone, province, city, district, detail string
					var platformDefault int
					if addressRows.Scan(&contactID, &areaID, &name, &phone, &province, &city, &district, &detail, &platformDefault, &synced) == nil {
						addresses = append(addresses, map[string]any{"contact_id": contactID, "area_id": areaID, "contact_name": name, "mobile_phone": maskPhone(phone), "province_name": province, "city_name": city, "district_name": district, "detail_address": detail, "platform_default": platformDefault != 0, "last_synced_at": synced})
					}
				}
				_ = addressRows.Close()
			}
			out = append(out, map[string]any{"cookie_id": id, "remark": remark, "address_id": address, "address_summary": summary, "verified_at": verified, "addresses": addresses})
		}
	}
	writeJSON(w, 200, out)
}

func maskPhone(value string) string {
	value = strings.TrimSpace(value)
	if len(value) < 7 {
		return value
	}
	return value[:3] + "****" + value[len(value)-4:]
}

func shippingAddressSummary(address mtop.ShippingAddress) string {
	return strings.TrimSpace(address.ContactName + "｜" + address.ProvinceName + address.CityName + address.DistrictName + " " + address.DetailAddress)
}

func (s *Server) syncShippingAddresses(w http.ResponseWriter, r *http.Request) {
	uid, cid := auth.SessionFromContext(r.Context()).UserID, chi.URLParam(r, "cookie_id")
	if _, ok := s.cookieForUser(r, uid, cid); !ok {
		writeErr(w, 403, "无权限操作该账号")
		return
	}
	cookie, err := s.Store.Cookies.GetValue(r.Context(), cid)
	if err != nil {
		writeErr(w, 500, "读取闲鱼账号 Cookie 失败")
		return
	}
	client, ok := s.MTop.(shippingAddressLister)
	if !ok {
		writeErr(w, 500, "闲鱼地址客户端不可用")
		return
	}
	addresses, ret, updatedCookie, err := client.FetchShippingAddressesContext(r.Context(), cookie)
	if err != nil {
		writeErr(w, 502, err.Error())
		return
	}
	if updatedCookie != "" && updatedCookie != cookie {
		_ = s.Store.Cookies.UpdateValueOwned(r.Context(), cid, updatedCookie, uid)
	}
	if len(addresses) == 0 {
		writeJSON(w, 422, map[string]any{"detail": "闲鱼账号没有可用的卖家发货地址", "ret": ret})
		return
	}
	now := time.Now().Unix()
	tx, err := s.Store.DB.BeginTx(r.Context(), nil)
	if err != nil {
		writeErr(w, 500, "保存地址失败")
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(r.Context(), `DELETE FROM xianyu_shipping_addresses WHERE cookie_id=? AND user_id=?`, cid, uid); err != nil {
		writeErr(w, 500, "清理旧地址失败")
		return
	}
	for _, address := range addresses {
		if address.ContactID <= 0 {
			continue
		}
		_, err = tx.ExecContext(r.Context(), `INSERT INTO xianyu_shipping_addresses(cookie_id,user_id,contact_id,area_id,contact_name,mobile_phone,province_name,city_name,district_name,detail_address,platform_default,last_synced_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, cid, uid, address.ContactID, address.AreaID, address.ContactName, address.MobilePhone, address.ProvinceName, address.CityName, address.DistrictName, address.DetailAddress, boolInt(address.DefaultAddr), now)
		if err != nil {
			writeErr(w, 500, "保存地址失败")
			return
		}
	}
	var selected int64
	_ = tx.QueryRowContext(r.Context(), `SELECT address_id FROM xianyu_shipping_accounts WHERE cookie_id=? AND user_id=?`, cid, uid).Scan(&selected)
	// The address-list API returns contactId, while the consign API requires a
	// different addressId. Keep the manually verified addressId unchanged.
	// Contact rows are reference data only and must never overwrite it.
	if err != nil || tx.Commit() != nil {
		writeErr(w, 500, "保存地址失败")
		return
	}
	writeJSON(w, 200, map[string]any{"success": true, "count": len(addresses), "selected_address_id": selected})
}
func (s *Server) saveShippingAccount(w http.ResponseWriter, r *http.Request) {
	uid := auth.SessionFromContext(r.Context()).UserID
	cid := chi.URLParam(r, "cookie_id")
	if _, ok := s.cookieForUser(r, uid, cid); !ok {
		writeErr(w, 403, "无权限操作该账号")
		return
	}
	var in struct {
		AddressID int64  `json:"address_id"`
		Summary   string `json:"address_summary"`
	}
	if decodeJSON(r, &in) != nil || in.AddressID <= 0 {
		writeErr(w, 400, "发货地址 ID 无效")
		return
	}
	now := time.Now().Unix()
	prefix := db.DialectInsertIgnorePrefix(s.Store.Dialect)
	suffix := db.DialectInsertIgnore(s.Store.Dialect, []string{"cookie_id"})
	_, err := s.Store.DB.ExecContext(r.Context(), prefix+` INTO xianyu_shipping_accounts(cookie_id,user_id,address_id,address_summary,verified_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`+suffix, cid, uid, in.AddressID, strings.TrimSpace(in.Summary), now, now, now)
	if err == nil {
		_, err = s.Store.DB.ExecContext(r.Context(), `UPDATE xianyu_shipping_accounts SET address_id=?,address_summary=?,verified_at=?,updated_at=? WHERE cookie_id=? AND user_id=?`, in.AddressID, strings.TrimSpace(in.Summary), now, now, cid, uid)
	}
	if err != nil {
		writeErr(w, 500, "保存发货地址失败")
		return
	}
	writeJSON(w, 200, map[string]any{"success": true})
}

func (s *Server) shippingPrecheck(w http.ResponseWriter, r *http.Request) {
	orderID := chi.URLParam(r, "order_id")
	order, ok := s.requireOrderOwner(w, r, orderID)
	if !ok {
		return
	}
	userID := auth.SessionFromContext(r.Context()).UserID
	var pddID, company, code, tracking, status string
	var pddShipped, xianyuShipped int
	err := s.Store.DB.QueryRowContext(r.Context(), `SELECT pdd_order_id,logistics_company,logistics_company_code,tracking_number,shipping_status,pdd_shipped,xianyu_shipped FROM order_fulfillments WHERE order_id=? AND user_id=?`, orderID, userID).Scan(&pddID, &company, &code, &tracking, &status, &pddShipped, &xianyuShipped)
	if err != nil {
		writeErr(w, 404, "履约记录不存在")
		return
	}
	problems := []string{}
	if db.NormalizeOrderStatus(strings.TrimSpace(order.OrderStatus)) != "pending_ship" {
		problems = append(problems, "闲鱼订单不是待发货状态")
	}
	if pddID == "" {
		problems = append(problems, "缺少拼多多订单号")
	}
	if pddShipped == 0 || tracking == "" {
		problems = append(problems, "尚未取得拼多多物流")
	}
	if code == "" {
		problems = append(problems, "快递公司尚未映射")
	}
	if xianyuShipped != 0 {
		problems = append(problems, "闲鱼订单已经发货")
	}
	addressID, addressErr := s.resolveConsignAddressID(r.Context(), order.CookieID, orderID, userID)
	if addressErr != nil {
		problems = append(problems, addressErr.Error())
	}
	if addressID <= 0 {
		problems = append(problems, "未取得闲鱼发货 addressId")
	}
	writeJSON(w, 200, map[string]any{"ready": len(problems) == 0, "problems": problems, "order_id": orderID, "pdd_order_id": pddID, "logistics_company": company, "logistics_company_code": code, "tracking_number": tracking, "shipping_status": status, "address_id": addressID})
}

func (s *Server) createShippingOperation(w http.ResponseWriter, r *http.Request) {
	orderID := chi.URLParam(r, "order_id")
	order, ok := s.requireOrderOwner(w, r, orderID)
	if !ok {
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		writeErr(w, 400, "缺少 Idempotency-Key")
		return
	}
	userID, now := auth.SessionFromContext(r.Context()).UserID, time.Now().Unix()
	var existingID, status, response, errMsg string
	if s.Store.DB.QueryRowContext(r.Context(), `SELECT id,status,response_json,error_message FROM fulfillment_ship_operations WHERE user_id=? AND idempotency_key=?`, userID, key).Scan(&existingID, &status, &response, &errMsg) == nil {
		writeJSON(w, 200, map[string]any{"operation_id": existingID, "status": status, "error": errMsg, "replayed": true})
		return
	}
	var pddID, company, code, tracking string
	var pddShipped, xianyuShipped int
	if s.Store.DB.QueryRowContext(r.Context(), `SELECT pdd_order_id,logistics_company,logistics_company_code,tracking_number,pdd_shipped,xianyu_shipped FROM order_fulfillments WHERE order_id=? AND user_id=?`, orderID, userID).Scan(&pddID, &company, &code, &tracking, &pddShipped, &xianyuShipped) != nil {
		writeErr(w, 404, "履约记录不存在")
		return
	}
	problems := []string{}
	if pddID == "" {
		problems = append(problems, "缺少拼多多订单号")
	}
	if pddShipped == 0 || tracking == "" {
		problems = append(problems, "缺少物流信息")
	}
	if code == "" {
		problems = append(problems, "缺少闲鱼快递代码")
	}
	if xianyuShipped != 0 {
		problems = append(problems, "订单已经发货")
	}
	if db.NormalizeOrderStatus(strings.TrimSpace(order.OrderStatus)) != "pending_ship" {
		problems = append(problems, "闲鱼订单不是待发货状态")
	}
	addressID, addressErr := s.resolveConsignAddressID(r.Context(), order.CookieID, orderID, userID)
	if addressErr != nil {
		problems = append(problems, addressErr.Error())
	}
	if addressID <= 0 {
		problems = append(problems, "未取得闲鱼发货 addressId")
	}
	request, _ := json.Marshal(map[string]any{"order_id": orderID, "company": company, "cp_code": code, "tracking_number": tracking, "address_id": addressID})
	opID := uuid.NewString()
	if len(problems) > 0 {
		errMessage := strings.Join(problems, "；")
		_, _ = s.Store.DB.ExecContext(r.Context(), `INSERT INTO fulfillment_ship_operations(id,user_id,order_id,idempotency_key,status,request_json,response_json,error_message,created_at,finished_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, opID, userID, orderID, key, "blocked", string(request), "", errMessage, now, now)
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"operation_id": opID, "status": "blocked", "error": errMessage, "problems": problems})
		return
	}
	result, err := s.Store.DB.ExecContext(r.Context(), `INSERT INTO fulfillment_ship_operations(id,user_id,order_id,idempotency_key,status,request_json,response_json,error_message,created_at) VALUES(?,?,?,?,? ,?,?,?,?)`, opID, userID, orderID, key, "submitting", string(request), "", "", now)
	if err != nil {
		if errors.Is(err, db.ErrAlreadyExists) {
			writeErr(w, 409, "发货任务已存在")
		} else {
			writeErr(w, 500, "创建发货任务失败")
		}
		return
	}
	_ = result
	cookie, err := s.Store.Cookies.GetValue(r.Context(), order.CookieID)
	if err != nil {
		writeErr(w, 500, "读取闲鱼账号 Cookie 失败")
		return
	}
	client, ok := s.MTop.(offlineConsigner)
	if !ok {
		writeErr(w, 500, "闲鱼实物发货客户端不可用")
		return
	}
	s.purchaseMu.Lock()
	okResult, ret, responseData, updated, callErr := client.ConsignOfflineContext(r.Context(), cookie, mtop.ConsignOfflineRequest{TradeID: orderID, MailNo: tracking, CPCode: code, AddressID: addressID})
	s.purchaseMu.Unlock()
	finished := time.Now().Unix()
	responseJSON, _ := json.Marshal(map[string]any{"ret": ret, "data": json.RawMessage(responseData)})
	status, errMessage, httpStatus := "failed", "", http.StatusBadGateway
	if callErr != nil {
		status = "result_unknown"
		errMessage = callErr.Error()
		httpStatus = http.StatusAccepted
	} else if okResult {
		status = "success"
		httpStatus = http.StatusOK
	} else {
		errMessage = strings.Join(ret, "；")
	}
	if updated != "" && updated != cookie {
		_ = s.Store.Cookies.UpdateValueOwned(r.Context(), order.CookieID, updated, userID)
	}
	_, _ = s.Store.DB.ExecContext(r.Context(), `UPDATE fulfillment_ship_operations SET status=?,response_json=?,error_message=?,finished_at=? WHERE id=?`, status, string(responseJSON), errMessage, finished, opID)
	if status == "success" {
		s.markPhysicalShipmentSuccess(r.Context(), order, userID, finished)
	} else {
		_, _ = s.Store.DB.ExecContext(r.Context(), `UPDATE order_fulfillments SET shipping_status=?,shipping_last_error=?,shipping_attempts=shipping_attempts+1,updated_at=? WHERE order_id=? AND user_id=?`, status, errMessage, finished, orderID, userID)
	}
	writeJSON(w, httpStatus, map[string]any{"operation_id": opID, "status": status, "success": status == "success", "ret": ret, "error": errMessage})
}

func (s *Server) markPhysicalShipmentSuccess(ctx context.Context, order *db.Order, userID, finished int64) {
	_, _ = s.Store.DB.ExecContext(ctx, `UPDATE order_fulfillments SET xianyu_shipped=1,xianyu_shipped_at=CASE WHEN xianyu_shipped_at=0 THEN ? ELSE xianyu_shipped_at END,shipping_status='xianyu_shipped',shipping_last_error='',shipping_attempts=shipping_attempts+1,updated_at=? WHERE order_id=? AND user_id=?`, finished, finished, order.OrderID, userID)
	shipped := true
	if err := s.Store.Orders.Upsert(ctx, order.OrderID, db.OrderUpsertOpts{
		CookieID: order.CookieID, OrderStatus: "shipped", SystemShipped: &shipped,
		ItemID: order.ItemID, BuyerID: order.BuyerID, ReceiverName: order.ReceiverName,
		ReceiverPhone: order.ReceiverPhone, ReceiverAddr: order.ReceiverAddr,
		ReceiverCity: order.ReceiverCity, ChatID: order.ChatID, SpecName: order.SpecName,
		SpecValue: order.SpecValue, Quantity: order.Quantity, Amount: order.Amount,
	}); err != nil {
		s.Logger.Error("保存闲鱼实物发货状态失败", "order_id", order.OrderID, "err", err)
	}
}

func (s *Server) resolveConsignAddressID(ctx context.Context, cookieID, orderID string, userID int64) (int64, error) {
	cookie, err := s.Store.Cookies.GetValue(ctx, cookieID)
	if err != nil {
		return 0, fmt.Errorf("读取闲鱼账号 Cookie 失败")
	}
	resolver, ok := s.MTop.(consignAddressResolver)
	if !ok {
		return 0, fmt.Errorf("闲鱼发货页地址解析不可用")
	}
	addressID, _, updated, err := resolver.FetchConsignAddressContext(ctx, cookie, orderID)
	if updated != "" && updated != cookie {
		_ = s.Store.Cookies.UpdateValueOwned(ctx, cookieID, updated, userID)
	}
	if err != nil {
		return 0, err
	}
	now := time.Now().Unix()
	prefix, suffix := db.DialectInsertIgnorePrefix(s.Store.Dialect), db.DialectInsertIgnore(s.Store.Dialect, []string{"cookie_id"})
	_, err = s.Store.DB.ExecContext(ctx, prefix+` INTO xianyu_shipping_accounts(cookie_id,user_id,address_id,address_summary,verified_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`+suffix, cookieID, userID, addressID, "闲鱼发货页自动获取", now, now, now)
	if err == nil {
		_, err = s.Store.DB.ExecContext(ctx, `UPDATE xianyu_shipping_accounts SET address_id=?,address_summary=?,verified_at=?,updated_at=? WHERE cookie_id=? AND user_id=?`, addressID, "闲鱼发货页自动获取", now, now, cookieID, userID)
	}
	if err != nil {
		return 0, fmt.Errorf("保存闲鱼发货 addressId 失败")
	}
	return addressID, nil
}
