package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type itemPDDMapping struct {
	XianyuSKUID, GoodsID, SourceSKUID, Properties, Source string
	Title, SpecsJSON, ThumbURL                            string
	PriceCent, Stock                                      int64
	OnSale                                                bool
}

func (s *Server) loadItemPDDMappings(ctx context.Context, userID int64, cookieID, itemID string) []itemPDDMapping {
	effective := map[string]itemPDDMapping{}
	publishRows, publishErr := s.Store.DB.QueryContext(ctx, `SELECT m.xianyu_sku_id,m.source_goods_id,m.source_sku_id,m.published_properties_json,'publish',COALESCE(g.title,''),COALESCE(k.specs_json,'[]'),COALESCE(k.thumb_url,''),COALESCE(k.price_cent,0),COALESCE(k.stock,0),COALESCE(k.is_onsale,0) FROM material_publish_records p JOIN material_publish_sku_mappings m ON m.publish_record_id=p.id LEFT JOIN pdd_products g ON g.goods_id=m.source_goods_id LEFT JOIN pdd_skus k ON k.goods_id=m.source_goods_id AND k.sku_id=m.source_sku_id WHERE p.id=(SELECT MAX(latest.id) FROM material_publish_records latest WHERE latest.user_id=? AND latest.cookie_id=? AND latest.published_item_id=? AND latest.status='success') AND m.mapping_status='mapped' ORDER BY m.id`, userID, cookieID, itemID)
	if publishErr == nil {
		for publishRows.Next() {
			var row itemPDDMapping
			var onSale int
			if publishRows.Scan(&row.XianyuSKUID, &row.GoodsID, &row.SourceSKUID, &row.Properties, &row.Source, &row.Title, &row.SpecsJSON, &row.ThumbURL, &row.PriceCent, &row.Stock, &onSale) == nil {
				row.OnSale = onSale != 0
				effective[row.XianyuSKUID] = row
			}
		}
		_ = publishRows.Close()
	}
	rows, err := s.Store.DB.QueryContext(ctx, `SELECT m.xianyu_sku_id,m.source_goods_id,m.source_sku_id,m.xianyu_properties_json,m.mapping_source,COALESCE(p.title,''),COALESCE(k.specs_json,'[]'),COALESCE(k.thumb_url,''),COALESCE(k.price_cent,0),COALESCE(k.stock,0),COALESCE(k.is_onsale,0) FROM item_pdd_sku_mappings m LEFT JOIN pdd_products p ON p.goods_id=m.source_goods_id LEFT JOIN pdd_skus k ON k.goods_id=m.source_goods_id AND k.sku_id=m.source_sku_id WHERE m.user_id=? AND m.cookie_id=? AND m.item_id=? ORDER BY m.id`, userID, cookieID, itemID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var row itemPDDMapping
			var onSale int
			if rows.Scan(&row.XianyuSKUID, &row.GoodsID, &row.SourceSKUID, &row.Properties, &row.Source, &row.Title, &row.SpecsJSON, &row.ThumbURL, &row.PriceCent, &row.Stock, &onSale) == nil {
				row.OnSale = onSale != 0
				effective[row.XianyuSKUID] = row
			}
		}
	}
	result := make([]itemPDDMapping, 0, len(effective))
	for _, row := range effective {
		result = append(result, row)
	}
	return result
}

func itemPDDMappingRows(rows []itemPDDMapping) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, m := range rows {
		out = append(out, map[string]any{"xianyu_sku_id": m.XianyuSKUID, "source_goods_id": m.GoodsID, "source_sku_id": m.SourceSKUID, "mapping_source": m.Source, "pdd_title": m.Title, "pdd_specs": jsonValue(m.SpecsJSON, []any{}), "pdd_thumb_url": m.ThumbURL, "pdd_price_cent": m.PriceCent, "pdd_stock": m.Stock, "pdd_onsale": m.OnSale, "source_exists": m.Title != ""})
	}
	return out
}

func (s *Server) itemPDDMappingDetail(ctx context.Context, userID int64, cookieID, itemID string) map[string]any {
	rows := s.loadItemPDDMappings(ctx, userID, cookieID, itemID)
	var total int
	_ = s.Store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM item_skus WHERE cookie_id=? AND item_id=?`, cookieID, itemID).Scan(&total)
	if total == 0 {
		total = 1
	}
	status := "unmapped"
	if len(rows) >= total {
		status = "mapped"
	} else if len(rows) > 0 {
		status = "partial"
	}
	return map[string]any{"status": status, "mapped": len(rows), "total": total, "rows": itemPDDMappingRows(rows)}
}

func (s *Server) itemPDDMappingSummary(ctx context.Context, userID int64, cookieID, itemID string) map[string]any {
	detail := s.itemPDDMappingDetail(ctx, userID, cookieID, itemID)
	return map[string]any{"status": detail["status"], "mapped": detail["mapped"], "total": detail["total"]}
}

func itemSKUProperties(raw string) string {
	var values []map[string]any
	if json.Unmarshal([]byte(raw), &values) != nil {
		return "[]"
	}
	props := []materialProperty{}
	for _, v := range values {
		name := strings.TrimSpace(fmt.Sprint(v["propertyText"]))
		value := strings.TrimSpace(fmt.Sprint(v["actualValueText"]))
		if value == "" || value == "<nil>" {
			value = strings.TrimSpace(fmt.Sprint(v["valueText"]))
		}
		if name == "<nil>" {
			name = ""
		}
		if value == "<nil>" {
			value = ""
		}
		if name != "" || value != "" {
			props = append(props, materialProperty{Name: name, Value: value})
		}
	}
	b, _ := json.Marshal(props)
	return string(b)
}

func (s *Server) upsertItemPDDMapping(w http.ResponseWriter, r *http.Request) {
	cid, itemID := chi.URLParam(r, "cookie_id"), chi.URLParam(r, "item_id")
	owner, ok := s.requireCookieOwner(w, r, cid)
	if !ok {
		return
	}
	if _, err := s.Store.Items.Get(r.Context(), cid, itemID); err != nil {
		writeErr(w, 404, "商品不存在")
		return
	}
	var req struct {
		XianyuSKUID   string `json:"xianyu_sku_id"`
		SourceGoodsID string `json:"source_goods_id"`
		SourceSKUID   string `json:"source_sku_id"`
	}
	if decodeJSON(r, &req) != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	req.XianyuSKUID = strings.TrimSpace(req.XianyuSKUID)
	req.SourceGoodsID = strings.TrimSpace(req.SourceGoodsID)
	req.SourceSKUID = strings.TrimSpace(req.SourceSKUID)
	if req.SourceGoodsID == "" || req.SourceSKUID == "" {
		writeErr(w, 400, "请选择拼多多商品和 SKU")
		return
	}
	var exists int
	if s.Store.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM pdd_skus WHERE goods_id=? AND sku_id=?`, req.SourceGoodsID, req.SourceSKUID).Scan(&exists) != nil || exists != 1 {
		writeErr(w, 400, "拼多多 SKU 不存在或不属于该商品")
		return
	}
	var skuCount int
	_ = s.Store.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM item_skus WHERE cookie_id=? AND item_id=?`, cid, itemID).Scan(&skuCount)
	properties := "[]"
	if req.XianyuSKUID != "" {
		var raw string
		if s.Store.DB.QueryRowContext(r.Context(), `SELECT properties_json FROM item_skus WHERE cookie_id=? AND item_id=? AND sku_id=?`, cid, itemID, req.XianyuSKUID).Scan(&raw) != nil {
			writeErr(w, 400, "闲鱼 SKU 不属于该商品")
			return
		}
		properties = itemSKUProperties(raw)
	} else if skuCount > 1 {
		writeErr(w, 400, "多规格商品必须选择闲鱼 SKU")
		return
	}
	old := map[string]any{}
	var oldGoods, oldSKU string
	if err := s.Store.DB.QueryRowContext(r.Context(), `SELECT source_goods_id,source_sku_id FROM item_pdd_sku_mappings WHERE user_id=? AND cookie_id=? AND item_id=? AND xianyu_sku_id=?`, owner.UserID, cid, itemID, req.XianyuSKUID).Scan(&oldGoods, &oldSKU); err == nil {
		old = map[string]any{"source_goods_id": oldGoods, "source_sku_id": oldSKU}
	}
	now := time.Now().Unix()
	res, err := s.Store.DB.ExecContext(r.Context(), `UPDATE item_pdd_sku_mappings SET source_goods_id=?,source_sku_id=?,xianyu_properties_json=?,mapping_source='manual',updated_at=? WHERE user_id=? AND cookie_id=? AND item_id=? AND xianyu_sku_id=?`, req.SourceGoodsID, req.SourceSKUID, properties, now, owner.UserID, cid, itemID, req.XianyuSKUID)
	if err == nil {
		if n, _ := res.RowsAffected(); n == 0 {
			_, err = s.Store.DB.ExecContext(r.Context(), `INSERT INTO item_pdd_sku_mappings(user_id,cookie_id,item_id,xianyu_sku_id,source_goods_id,source_sku_id,xianyu_properties_json,mapping_source,created_at,updated_at) VALUES(?,?,?,?,?,?,?,'manual',?,?)`, owner.UserID, cid, itemID, req.XianyuSKUID, req.SourceGoodsID, req.SourceSKUID, properties, now, now)
		}
	}
	if err != nil {
		writeErr(w, 500, "保存 SKU 映射失败")
		return
	}
	oldJSON, _ := json.Marshal(old)
	newJSON, _ := json.Marshal(map[string]any{"source_goods_id": req.SourceGoodsID, "source_sku_id": req.SourceSKUID})
	_, _ = s.Store.DB.ExecContext(r.Context(), `INSERT INTO item_pdd_sku_mapping_audits(id,user_id,cookie_id,item_id,xianyu_sku_id,action,old_mapping_json,new_mapping_json,created_at) VALUES(?,?,?,?,?,'upsert',?,?,?)`, uuid.NewString(), owner.UserID, cid, itemID, req.XianyuSKUID, string(oldJSON), string(newJSON), now)
	reconciled := s.reconcileItemMappingOrders(r, owner.UserID, cid, itemID)
	writeJSON(w, 200, map[string]any{"success": true, "reconciled_orders": reconciled, "pdd_mapping": s.itemPDDMappingDetail(r.Context(), owner.UserID, cid, itemID)})
}

func (s *Server) deleteItemPDDMapping(w http.ResponseWriter, r *http.Request) {
	cid, itemID, skuID := chi.URLParam(r, "cookie_id"), chi.URLParam(r, "item_id"), chi.URLParam(r, "xianyu_sku_id")
	if skuID == "_default" {
		skuID = ""
	}
	owner, ok := s.requireCookieOwner(w, r, cid)
	if !ok {
		return
	}
	res, err := s.Store.DB.ExecContext(r.Context(), `DELETE FROM item_pdd_sku_mappings WHERE user_id=? AND cookie_id=? AND item_id=? AND xianyu_sku_id=?`, owner.UserID, cid, itemID, skuID)
	if err != nil {
		writeErr(w, 500, "删除 SKU 映射失败")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeErr(w, 404, "映射不存在")
		return
	}
	now := time.Now().Unix()
	_, _ = s.Store.DB.ExecContext(r.Context(), `INSERT INTO item_pdd_sku_mapping_audits(id,user_id,cookie_id,item_id,xianyu_sku_id,action,old_mapping_json,new_mapping_json,created_at) VALUES(?,?,?,?,?,'delete','','',?)`, uuid.NewString(), owner.UserID, cid, itemID, skuID, now)
	writeJSON(w, 200, map[string]any{"success": true, "reconciled_orders": s.reconcileItemMappingOrders(r, owner.UserID, cid, itemID)})
}

func (s *Server) reconcileItemMappingOrders(r *http.Request, userID int64, cookieID, itemID string) int {
	rows, err := s.Store.DB.QueryContext(r.Context(), `SELECT o.order_id FROM orders o JOIN order_fulfillments f ON f.order_id=o.order_id AND f.user_id=? WHERE o.cookie_id=? AND o.item_id=? AND o.deleted_at IS NULL AND o.order_status<>'cancelled' AND f.pdd_order_id='' AND f.manual_modified_at=0`, userID, cookieID, itemID)
	if err != nil {
		return 0
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	for _, id := range ids {
		if order, e := s.Store.Orders.Get(r.Context(), id); e == nil {
			s.resolveFulfillmentSKU(r, *order, userID)
		}
	}
	return len(ids)
}
