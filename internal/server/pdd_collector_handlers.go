package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"xianyu-go/internal/db"
)

var pddNumericID = regexp.MustCompile(`^[0-9]+$`)

type pddSpecInput struct {
	SpecKey     string `json:"spec_key"`
	SpecKeyID   string `json:"spec_key_id"`
	SpecValueID string `json:"spec_value_id"`
	RawValue    string `json:"raw_value"`
}

type pddSKUInput struct {
	SKUID        string         `json:"sku_id"`
	GoodsID      string         `json:"goods_id"`
	ThumbURL     string         `json:"thumb_url"`
	Stock        int64          `json:"stock"`
	IsOnsale     bool           `json:"is_onsale"`
	Prices       map[string]any `json:"prices"`
	SpecValueIDs []string       `json:"spec_value_ids"`
	Specs        []pddSpecInput `json:"specs"`
}

type pddGoodsPropertyInput struct {
	Key         string   `json:"key"`
	Values      []string `json:"values"`
	RefPID      string   `json:"ref_pid,omitempty"`
	ReferenceID string   `json:"reference_id,omitempty"`
}

type pddCollectionInput struct {
	SchemaVersion    int    `json:"schema_version"`
	CollectionID     string `json:"collection_id"`
	CollectionMethod string `json:"collection_method"`
	CollectedAt      string `json:"collected_at"`
	FinalURL         string `json:"final_url"`
	Goods            struct {
		GoodsID       string                  `json:"goods_id"`
		Title         string                  `json:"title"`
		Images        []string                `json:"images"`
		GoodsProperty []pddGoodsPropertyInput `json:"goods_property"`
	} `json:"goods"`
	SKUs []pddSKUInput `json:"skus"`
}

func (s *Server) mountPDDCollectorPublic(r interface {
	Post(string, http.HandlerFunc)
}) {
	r.Post("/api/pdd-collector/products", s.pddCollectorUpload)
}

func (s *Server) mountPDDCollectorAdmin(r interface {
	Post(string, http.HandlerFunc)
	Get(string, http.HandlerFunc)
	Delete(string, http.HandlerFunc)
}) {
	r.Post("/api/pdd-collector/devices", s.pddCreateCollectorDevice)
	r.Get("/api/pdd-collector/devices", s.pddListCollectorDevices)
	r.Get("/api/pdd-collector/catalog", s.pddListProducts)
	r.Get("/api/pdd-collector/catalog/{goodsID}", s.pddGetProduct)
	r.Delete("/api/pdd-collector/catalog/{goodsID}", s.pddDeleteProduct)
}

func (s *Server) pddDeleteProduct(w http.ResponseWriter, r *http.Request) {
	goodsID := chi.URLParam(r, "goodsID")
	if !pddNumericID.MatchString(goodsID) {
		writeErr(w, http.StatusBadRequest, "goods_id 无效")
		return
	}
	var draftCount int64
	_ = s.Store.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM product_materials WHERE source_type='pdd' AND source_id=? AND deleted_at IS NULL`, goodsID).Scan(&draftCount)
	tx, err := s.Store.DB.BeginTx(r.Context(), nil)
	if err != nil {
		writeErr(w, 500, "删除采集商品失败")
		return
	}
	defer tx.Rollback()
	var productID int64
	if err = tx.QueryRowContext(r.Context(), `SELECT id FROM pdd_products WHERE goods_id=?`, goodsID).Scan(&productID); err != nil {
		writeErr(w, 404, "采集商品不存在")
		return
	}
	for _, q := range []string{`DELETE FROM pdd_sku_snapshots WHERE goods_id=?`, `DELETE FROM pdd_products WHERE id=?`} {
		arg := any(goodsID)
		if strings.Contains(q, "pdd_products") {
			arg = productID
		}
		if _, err = tx.ExecContext(r.Context(), q, arg); err != nil {
			writeErr(w, 500, "删除采集商品失败")
			return
		}
	}
	if err = tx.Commit(); err != nil {
		writeErr(w, 500, "删除采集商品失败")
		return
	}
	writeJSON(w, 200, map[string]any{"success": true, "draft_count": draftCount, "message": "采集商品已删除，关联素材草稿和闲鱼商品未受影响"})
}

func jsonValue(raw string, fallback any) any {
	var value any
	if json.Unmarshal([]byte(raw), &value) != nil {
		return fallback
	}
	return value
}

func (s *Server) pddListProducts(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.DB.QueryContext(r.Context(), `SELECT p.id,p.goods_id,p.final_url,p.title,p.images_json,p.first_collected_at,p.last_collected_at,COUNT(s.id),COALESCE(SUM(CASE WHEN s.is_onsale=1 THEN 1 ELSE 0 END),0),COALESCE(MIN(s.price_cent),0),COALESCE(MAX(s.price_cent),0) FROM pdd_products p LEFT JOIN pdd_skus s ON s.product_id=p.id GROUP BY p.id,p.goods_id,p.final_url,p.title,p.images_json,p.first_collected_at,p.last_collected_at ORDER BY p.last_collected_at DESC`)
	if err != nil {
		writeErr(w, 500, "查询拼多多商品失败")
		return
	}
	defer rows.Close()
	out := make([]map[string]any, 0)
	for rows.Next() {
		var id, firstAt, lastAt, skuCount, onSaleCount, minPrice, maxPrice int64
		var goodsID, finalURL, title, images string
		if err := rows.Scan(&id, &goodsID, &finalURL, &title, &images, &firstAt, &lastAt, &skuCount, &onSaleCount, &minPrice, &maxPrice); err != nil {
			writeErr(w, 500, "读取拼多多商品失败")
			return
		}
		out = append(out, map[string]any{"id": id, "goods_id": goodsID, "final_url": finalURL, "title": title, "images": jsonValue(images, []string{}), "first_collected_at": firstAt, "last_collected_at": lastAt, "sku_count": skuCount, "onsale_sku_count": onSaleCount, "min_price_cent": minPrice, "max_price_cent": maxPrice})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) pddGetProduct(w http.ResponseWriter, r *http.Request) {
	goodsID := chi.URLParam(r, "goodsID")
	if !pddNumericID.MatchString(goodsID) {
		writeErr(w, http.StatusBadRequest, "goods_id 无效")
		return
	}
	var id, firstAt, lastAt int64
	var finalURL, title, images, properties string
	if err := s.Store.DB.QueryRowContext(r.Context(), `SELECT id,final_url,title,images_json,properties_json,first_collected_at,last_collected_at FROM pdd_products WHERE goods_id=?`, goodsID).Scan(&id, &finalURL, &title, &images, &properties, &firstAt, &lastAt); err != nil {
		writeErr(w, http.StatusNotFound, "拼多多商品不存在")
		return
	}
	rows, err := s.Store.DB.QueryContext(r.Context(), `SELECT id,sku_id,specs_json,spec_value_ids_json,thumb_url,prices_json,price_cent,stock,is_onsale,last_collected_at FROM pdd_skus WHERE product_id=? ORDER BY is_onsale DESC,stock DESC,id`, id)
	if err != nil {
		writeErr(w, 500, "查询拼多多SKU失败")
		return
	}
	defer rows.Close()
	skus := make([]map[string]any, 0)
	for rows.Next() {
		var skuRecordID, price, stock, onSale, collectedAt int64
		var skuID, specs, specIDs, thumbURL, prices string
		if err := rows.Scan(&skuRecordID, &skuID, &specs, &specIDs, &thumbURL, &prices, &price, &stock, &onSale, &collectedAt); err != nil {
			writeErr(w, 500, "读取拼多多SKU失败")
			return
		}
		skus = append(skus, map[string]any{"id": skuRecordID, "sku_id": skuID, "specs": jsonValue(specs, []any{}), "spec_value_ids": jsonValue(specIDs, []any{}), "thumb_url": thumbURL, "prices": jsonValue(prices, map[string]any{}), "price_cent": price, "stock": stock, "is_onsale": onSale != 0, "last_collected_at": collectedAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "goods_id": goodsID, "final_url": finalURL, "title": title, "images": jsonValue(images, []string{}), "goods_property": jsonValue(properties, []any{}), "first_collected_at": firstAt, "last_collected_at": lastAt, "skus": skus})
}

func tokenDigest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func newCollectorToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "pddc_" + base64.RawURLEncoding.EncodeToString(buf), nil
}

func (s *Server) pddCreateCollectorDevice(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len([]rune(in.Name)) > 100 {
		writeErr(w, http.StatusBadRequest, "设备名称不能为空且不能超过100个字符")
		return
	}
	token, err := newCollectorToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "生成设备凭证失败")
		return
	}
	id, now := uuid.NewString(), time.Now().Unix()
	if _, err = s.Store.DB.ExecContext(r.Context(), `INSERT INTO pdd_collector_devices(id,name,token_hash,enabled,last_seen_at,last_collected_at,created_at) VALUES(?,?,?,1,0,0,?)`, id, in.Name, tokenDigest(token), now); err != nil {
		writeErr(w, http.StatusInternalServerError, "创建设备失败")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "name": in.Name, "device_token": token, "created_at": now})
}

func (s *Server) pddListCollectorDevices(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.DB.QueryContext(r.Context(), `SELECT id,name,enabled,last_seen_at,last_collected_at,created_at FROM pdd_collector_devices ORDER BY created_at DESC`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询设备失败")
		return
	}
	defer rows.Close()
	out := make([]map[string]any, 0)
	for rows.Next() {
		var id, name string
		var enabled int
		var seen, collected, created int64
		if err := rows.Scan(&id, &name, &enabled, &seen, &collected, &created); err != nil {
			writeErr(w, 500, "读取设备失败")
			return
		}
		out = append(out, map[string]any{"id": id, "name": name, "enabled": enabled != 0, "last_seen_at": seen, "last_collected_at": collected, "created_at": created})
	}
	writeJSON(w, http.StatusOK, out)
}

func bearerToken(r *http.Request) string {
	value := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(value) < 8 || !strings.EqualFold(value[:7], "Bearer ") {
		return ""
	}
	return strings.TrimSpace(value[7:])
}

func (s *Server) authenticateCollector(r *http.Request) (string, error) {
	token := bearerToken(r)
	if !strings.HasPrefix(token, "pddc_") {
		return "", errors.New("invalid token")
	}
	var id string
	if err := s.Store.DB.QueryRowContext(r.Context(), `SELECT id FROM pdd_collector_devices WHERE token_hash=? AND enabled=1`, tokenDigest(token)).Scan(&id); err != nil {
		return "", errors.New("invalid token")
	}
	return id, nil
}

func validatePDDCollection(in *pddCollectionInput) error {
	if in.SchemaVersion != 1 {
		return errors.New("不支持的 schema_version")
	}
	if _, err := uuid.Parse(in.CollectionID); err != nil {
		return errors.New("collection_id 必须是 UUID")
	}
	if !pddNumericID.MatchString(in.Goods.GoodsID) {
		return errors.New("goods_id 必须是数字字符串")
	}
	if len(in.SKUs) == 0 || len(in.SKUs) > 5000 {
		return errors.New("SKU 数量必须在 1 到 5000 之间")
	}
	seen := make(map[string]struct{}, len(in.SKUs))
	properties := make([]pddGoodsPropertyInput, 0, len(in.Goods.GoodsProperty))
	for _, property := range in.Goods.GoodsProperty {
		property.Key = strings.TrimSpace(property.Key)
		if property.Key == "" || property.Key == "品牌" || property.Key == "发货地" {
			continue
		}
		values := make([]string, 0, len(property.Values))
		for _, value := range property.Values {
			if value = strings.TrimSpace(value); value != "" {
				values = append(values, value)
			}
		}
		if len(values) == 0 {
			continue
		}
		property.Values = values
		properties = append(properties, property)
	}
	in.Goods.GoodsProperty = properties
	for i, sku := range in.SKUs {
		if !pddNumericID.MatchString(sku.SKUID) || sku.GoodsID != in.Goods.GoodsID {
			return fmt.Errorf("第%d个SKU标识无效", i+1)
		}
		if sku.Stock < 0 {
			return fmt.Errorf("第%d个SKU库存无效", i+1)
		}
		if _, ok := seen[sku.SKUID]; ok {
			return fmt.Errorf("sku_id %s 重复", sku.SKUID)
		}
		seen[sku.SKUID] = struct{}{}
	}
	return nil
}

func pddPriceCent(prices map[string]any) int64 {
	// group_price is the customer-facing group-buy price in yuan. sku_price is
	// the same effective price in cents on the page payloads we collect. Keep
	// normal_price (single-buy price) and old_group_price (reference/old group
	// price) only as fallbacks so newly collected materials do not start at the
	// crossed-out price.
	for _, key := range []string{"group_price"} {
		var yuan float64
		switch v := prices[key].(type) {
		case float64:
			yuan = v
		case string:
			_, _ = fmt.Sscan(v, &yuan)
		}
		if yuan >= 0 && yuan != 0 {
			return int64(yuan*100 + .5)
		}
	}
	if n, ok := prices["sku_price"].(float64); ok && n > 0 {
		return int64(n + .5)
	}
	for _, key := range []string{"normal_price"} {
		var yuan float64
		switch v := prices[key].(type) {
		case float64:
			yuan = v
		case string:
			_, _ = fmt.Sscan(v, &yuan)
		}
		if yuan > 0 {
			return int64(yuan*100 + .5)
		}
	}
	if n, ok := prices["old_group_price"].(float64); ok && n > 0 {
		return int64(n + .5)
	}
	return 0
}

func (s *Server) pddCollectorUpload(w http.ResponseWriter, r *http.Request) {
	deviceID, err := s.authenticateCollector(r)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "设备 Token 无效或已停用")
		return
	}
	var in pddCollectionInput
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if err := validatePDDCollection(&in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	payload, _ := json.Marshal(in)
	images, _ := json.Marshal(in.Goods.Images)
	properties, _ := json.Marshal(in.Goods.GoodsProperty)
	collectedAt := time.Now().Unix()
	if parsed, parseErr := time.Parse(time.RFC3339, in.CollectedAt); parseErr == nil {
		collectedAt = parsed.Unix()
	}
	receivedAt := time.Now().Unix()
	tx, err := s.Store.DB.BeginTx(r.Context(), nil)
	if err != nil {
		writeErr(w, 500, "启动数据库事务失败")
		return
	}
	defer tx.Rollback()

	var existingProduct int64
	err = tx.QueryRowContext(r.Context(), `SELECT product_id FROM pdd_collection_snapshots WHERE collection_id=?`, in.CollectionID).Scan(&existingProduct)
	if err == nil {
		_ = tx.Rollback()
		writeJSON(w, http.StatusOK, map[string]any{"status": "already_collected", "collection_id": in.CollectionID, "product_id": existingProduct})
		return
	}

	productUpsert := db.DialectUpsert(s.Store.Dialect, []string{"goods_id"}, map[string]string{"final_url": "EXCLUDED.final_url", "title": "EXCLUDED.title", "images_json": "EXCLUDED.images_json", "properties_json": "EXCLUDED.properties_json", "last_collected_at": "EXCLUDED.last_collected_at"})
	_, err = tx.ExecContext(r.Context(), `INSERT INTO pdd_products(goods_id,final_url,title,images_json,properties_json,first_collected_at,last_collected_at) VALUES(?,?,?,?,?,?,?)`+productUpsert, in.Goods.GoodsID, in.FinalURL, in.Goods.Title, string(images), string(properties), collectedAt, collectedAt)
	if err != nil {
		writeErr(w, 500, "保存拼多多商品失败")
		return
	}
	var productID int64
	if err = tx.QueryRowContext(r.Context(), `SELECT id FROM pdd_products WHERE goods_id=?`, in.Goods.GoodsID).Scan(&productID); err != nil {
		writeErr(w, 500, "读取商品记录失败")
		return
	}
	_, err = tx.ExecContext(r.Context(), `INSERT INTO pdd_collection_snapshots(collection_id,device_id,product_id,goods_id,final_url,payload_json,collected_at,received_at) VALUES(?,?,?,?,?,?,?,?)`, in.CollectionID, deviceID, productID, in.Goods.GoodsID, in.FinalURL, string(payload), collectedAt, receivedAt)
	if err != nil {
		writeErr(w, 500, "保存采集快照失败")
		return
	}
	var collectionSnapshotID int64
	if err = tx.QueryRowContext(r.Context(), `SELECT id FROM pdd_collection_snapshots WHERE collection_id=?`, in.CollectionID).Scan(&collectionSnapshotID); err != nil {
		writeErr(w, 500, "读取采集快照失败")
		return
	}

	skuUpsert := db.DialectUpsert(s.Store.Dialect, []string{"goods_id", "sku_id"}, map[string]string{"product_id": "EXCLUDED.product_id", "specs_json": "EXCLUDED.specs_json", "spec_value_ids_json": "EXCLUDED.spec_value_ids_json", "thumb_url": "EXCLUDED.thumb_url", "prices_json": "EXCLUDED.prices_json", "price_cent": "EXCLUDED.price_cent", "stock": "EXCLUDED.stock", "is_onsale": "EXCLUDED.is_onsale", "raw_snapshot_json": "EXCLUDED.raw_snapshot_json", "last_collected_at": "EXCLUDED.last_collected_at"})
	for _, sku := range in.SKUs {
		specs, _ := json.Marshal(sku.Specs)
		specIDs, _ := json.Marshal(sku.SpecValueIDs)
		prices, _ := json.Marshal(sku.Prices)
		raw, _ := json.Marshal(sku)
		onSale := 0
		if sku.IsOnsale {
			onSale = 1
		}
		priceCent := pddPriceCent(sku.Prices)
		_, err = tx.ExecContext(r.Context(), `INSERT INTO pdd_skus(product_id,goods_id,sku_id,specs_json,spec_value_ids_json,thumb_url,prices_json,price_cent,stock,is_onsale,raw_snapshot_json,last_collected_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`+skuUpsert, productID, sku.GoodsID, sku.SKUID, string(specs), string(specIDs), sku.ThumbURL, string(prices), priceCent, sku.Stock, onSale, string(raw), collectedAt)
		if err != nil {
			writeErr(w, 500, "保存SKU失败")
			return
		}
		var skuRecordID int64
		if err = tx.QueryRowContext(r.Context(), `SELECT id FROM pdd_skus WHERE goods_id=? AND sku_id=?`, sku.GoodsID, sku.SKUID).Scan(&skuRecordID); err != nil {
			writeErr(w, 500, "读取SKU失败")
			return
		}
		_, err = tx.ExecContext(r.Context(), `INSERT INTO pdd_sku_snapshots(collection_snapshot_id,pdd_sku_id,goods_id,sku_id,specs_json,spec_value_ids_json,thumb_url,prices_json,price_cent,stock,is_onsale,raw_snapshot_json,collected_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, collectionSnapshotID, skuRecordID, sku.GoodsID, sku.SKUID, string(specs), string(specIDs), sku.ThumbURL, string(prices), priceCent, sku.Stock, onSale, string(raw), collectedAt)
		if err != nil {
			writeErr(w, 500, "保存SKU快照失败")
			return
		}
	}
	_, _ = tx.ExecContext(r.Context(), `UPDATE pdd_collector_devices SET last_seen_at=?,last_collected_at=? WHERE id=?`, receivedAt, receivedAt, deviceID)
	if err = tx.Commit(); err != nil {
		writeErr(w, 500, "提交采集数据失败")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"status": "collected", "collection_id": in.CollectionID, "product_id": productID, "goods_id": in.Goods.GoodsID, "sku_count": len(in.SKUs)})
}
