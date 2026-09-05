package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

type materialSKU struct {
	MaterialSKUID          string             `json:"material_sku_id"`
	SKUType                string             `json:"sku_type,omitempty"`
	SourceGoodsID          string             `json:"source_goods_id,omitempty"`
	SourceSKUID            string             `json:"source_sku_id,omitempty"`
	SourceProperties       []materialProperty `json:"source_properties,omitempty"`
	SourceImageURL         string             `json:"source_image_url,omitempty"`
	SourcePriceCents       int64              `json:"source_price_cent,omitempty"`
	SourceNormalPriceCents int64              `json:"source_normal_price_cent,omitempty"`
	SourcePriceUpdatedAt   int64              `json:"source_price_updated_at,omitempty"`
	SourcePriceOrigin      string             `json:"source_price_origin,omitempty"`
	PriceCents             int64              `json:"price_cent"`
	Quantity               int64              `json:"quantity"`
	Enabled                bool               `json:"enabled"`
	Properties             []materialProperty `json:"properties"`
	ImageURL               string             `json:"image_url,omitempty"`
}

const (
	materialSKUTypeSource      = "source"
	materialSKUTypePlaceholder = "placeholder"
	materialSKUTypeManual      = "manual"
	materialSKUGroupLimit      = 252
)

func normalizeMaterialSKUIdentity(sourceType, primarySourceID string, sku *materialSKU) {
	sku.MaterialSKUID = strings.TrimSpace(sku.MaterialSKUID)
	sku.SourceGoodsID = strings.TrimSpace(sku.SourceGoodsID)
	sku.SourceSKUID = strings.TrimSpace(sku.SourceSKUID)
	sku.SKUType = strings.TrimSpace(sku.SKUType)
	if sku.SourceGoodsID == "" && sku.SourceSKUID != "" {
		sku.SourceGoodsID = primarySourceID
	}
	if sku.SKUType == "" {
		switch {
		case sku.SourceSKUID != "":
			sku.SKUType = materialSKUTypeSource
		case sourceType == "pdd" && sku.Quantity == 0:
			sku.SKUType = materialSKUTypePlaceholder
		default:
			sku.SKUType = materialSKUTypeManual
		}
	}
}

func validateMaterialSKUIdentity(sourceType, primarySourceID string, sku *materialSKU) error {
	normalizeMaterialSKUIdentity(sourceType, primarySourceID, sku)
	switch sku.SKUType {
	case materialSKUTypeSource:
		if sku.SourceGoodsID == "" || sku.SourceSKUID == "" {
			return errors.New("来源 SKU 缺少拼多多商品或 SKU 绑定")
		}
	case materialSKUTypePlaceholder:
		if sku.SourceGoodsID != "" || sku.SourceSKUID != "" {
			return errors.New("占位 SKU 不能绑定拼多多来源")
		}
		sku.Quantity = 0
	case materialSKUTypeManual:
		if sku.SourceGoodsID != "" || sku.SourceSKUID != "" {
			return errors.New("手工 SKU 不能携带拼多多来源绑定")
		}
	default:
		return fmt.Errorf("不支持的 SKU 类型: %s", sku.SKUType)
	}
	return nil
}

// protectMaterialSKUIdentities 使普通编辑只能更改发布属性，不能隐式更换或清空来源身份。
func protectMaterialSKUIdentities(sourceType, primarySourceID string, oldSKUs, incoming []materialSKU) error {
	oldByID := make(map[string]materialSKU, len(oldSKUs))
	oldSourceIDs := make(map[string]bool, len(oldSKUs))
	for index := range oldSKUs {
		normalizeMaterialSKUIdentity(sourceType, primarySourceID, &oldSKUs[index])
		if oldSKUs[index].MaterialSKUID != "" {
			oldByID[oldSKUs[index].MaterialSKUID] = oldSKUs[index]
			if oldSKUs[index].SKUType == materialSKUTypeSource {
				oldSourceIDs[oldSKUs[index].MaterialSKUID] = true
			}
		}
	}
	seen := make(map[string]bool, len(incoming))
	for index := range incoming {
		if strings.TrimSpace(incoming[index].MaterialSKUID) == "" {
			incoming[index].MaterialSKUID = uuid.NewString()
		}
		id := incoming[index].MaterialSKUID
		if seen[id] {
			return fmt.Errorf("素材 SKU 稳定 ID 重复: %s", id)
		}
		seen[id] = true
		if old, exists := oldByID[id]; exists {
			incoming[index].SKUType = old.SKUType
			incoming[index].SourceGoodsID = old.SourceGoodsID
			incoming[index].SourceSKUID = old.SourceSKUID
			incoming[index].SourceProperties = old.SourceProperties
			incoming[index].SourceImageURL = old.SourceImageURL
			incoming[index].SourcePriceCents = old.SourcePriceCents
			incoming[index].SourceNormalPriceCents = old.SourceNormalPriceCents
			incoming[index].SourcePriceUpdatedAt = old.SourcePriceUpdatedAt
			incoming[index].SourcePriceOrigin = old.SourcePriceOrigin
		}
		if err := validateMaterialSKUIdentity(sourceType, primarySourceID, &incoming[index]); err != nil {
			return fmt.Errorf("SKU %s: %w", id, err)
		}
	}
	for id := range oldSourceIDs {
		if !seen[id] {
			return fmt.Errorf("来源 SKU %s 不能通过普通素材编辑删除或重建，请先使用来源映射操作", id)
		}
	}
	return validateDistinctMaterialSources(incoming)
}

func validateDistinctMaterialSources(skus []materialSKU) error {
	seen := make(map[string]string, len(skus))
	for index := range skus {
		sku := &skus[index]
		if sku.SKUType != materialSKUTypeSource {
			continue
		}
		key := materialSourceKey(sku.SourceGoodsID, sku.SourceSKUID)
		if previous, exists := seen[key]; exists {
			return fmt.Errorf("拼多多来源 SKU 重复绑定: %s 与 %s", previous, sku.MaterialSKUID)
		}
		seen[key] = sku.MaterialSKUID
	}
	return nil
}

func pddNormalPriceCent(raw string) int64 {
	var prices map[string]any
	if json.Unmarshal([]byte(raw), &prices) != nil {
		return 0
	}
	var yuan float64
	switch value := prices["normal_price"].(type) {
	case float64:
		yuan = value
	case string:
		_, _ = fmt.Sscan(value, &yuan)
	}
	if yuan <= 0 {
		return 0
	}
	return int64(yuan*100 + .5)
}

type materialProperty struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	ImageURL string `json:"image_url,omitempty"`
}
type materialInput struct {
	Title              string                   `json:"title"`
	Description        string                   `json:"description"`
	Images             []string                 `json:"images"`
	Category           map[string]any           `json:"category"`
	SKUs               []materialSKU            `json:"skus"`
	PostageMode        string                   `json:"postage_mode"`
	PostageCents       int64                    `json:"postage_cent"`
	ImagePropertyName  string                   `json:"image_property_name"`
	VideoEnabled       *bool                    `json:"video_enabled"`
	Videos             []materialVideo          `json:"videos"`
	OriginalPriceCents int64                    `json:"original_price_cent"`
	SourceProperties   []materialSourceProperty `json:"source_properties"`
	ImageMetadata      []materialImageMetadata  `json:"image_metadata"`
	PriceStrategy      materialPriceStrategy    `json:"price_strategy"`
	StockStrategy      materialStockStrategy    `json:"stock_strategy"`
	Revision           int64                    `json:"revision"`
}

type materialSourceProperty struct {
	Name   string   `json:"name"`
	Values []string `json:"values"`
}

type materialImageMetadata struct {
	URL           string `json:"url"`
	Source        string `json:"source"`
	SourceGoodsID string `json:"source_goods_id,omitempty"`
	SourceSKUID   string `json:"source_sku_id,omitempty"`
	LocalPath     string `json:"local_path,omitempty"`
	Hash          string `json:"hash,omitempty"`
	Status        string `json:"status"`
}

type materialPriceStrategy struct {
	Mode              string  `json:"mode"`
	Value             float64 `json:"value"`
	MinimumProfitCent int64   `json:"minimum_profit_cent"`
}

type materialStockStrategy struct {
	Mode           string `json:"mode"`
	Cap            int64  `json:"cap"`
	Reserve        int64  `json:"reserve"`
	FixedQuantity  int64  `json:"fixed_quantity"`
	DisableWhenOOS bool   `json:"disable_when_oos"`
}

type materialPublishParameters struct {
	OriginalPriceCents int64                    `json:"original_price_cent"`
	SourceProperties   []materialSourceProperty `json:"source_properties"`
	ImageMetadata      []materialImageMetadata  `json:"image_metadata"`
	PriceStrategy      materialPriceStrategy    `json:"price_strategy"`
	StockStrategy      materialStockStrategy    `json:"stock_strategy"`
}

func publishParametersFromInput(in materialInput) materialPublishParameters {
	return materialPublishParameters{OriginalPriceCents: in.OriginalPriceCents, SourceProperties: in.SourceProperties, ImageMetadata: in.ImageMetadata, PriceStrategy: in.PriceStrategy, StockStrategy: in.StockStrategy}
}

func normalizePublishParameters(parameters *materialPublishParameters, images []string) {
	if parameters.SourceProperties == nil {
		parameters.SourceProperties = []materialSourceProperty{}
	}
	if parameters.ImageMetadata == nil {
		parameters.ImageMetadata = []materialImageMetadata{}
	}
	if parameters.PriceStrategy.Mode == "" {
		parameters.PriceStrategy.Mode = "manual"
	}
	if parameters.StockStrategy.Mode == "" {
		parameters.StockStrategy.Mode = "manual"
	}
	current := make(map[string]bool, len(images))
	for _, image := range images {
		if image = strings.TrimSpace(image); image != "" {
			current[image] = true
		}
	}
	known := make(map[string]bool, len(parameters.ImageMetadata))
	filtered := parameters.ImageMetadata[:0]
	for index := range parameters.ImageMetadata {
		metadata := parameters.ImageMetadata[index]
		metadata.URL = strings.TrimSpace(metadata.URL)
		if metadata.URL == "" || !current[metadata.URL] || known[metadata.URL] {
			continue
		}
		if metadata.Status == "" {
			metadata.Status = "valid"
		}
		known[metadata.URL] = true
		filtered = append(filtered, metadata)
	}
	parameters.ImageMetadata = filtered
	for _, image := range images {
		if image = strings.TrimSpace(image); image != "" && !known[image] {
			parameters.ImageMetadata = append(parameters.ImageMetadata, materialImageMetadata{URL: image, Source: "legacy", Status: "valid"})
		}
	}
}

type materialVideo struct {
	Source        string `json:"source"`
	SourceGoodsID string `json:"source_goods_id,omitempty"`
	ReviewID      string `json:"review_id,omitempty"`
	SKUID         string `json:"sku_id,omitempty"`
	URL           string `json:"url"`
	CoverURL      string `json:"cover_url,omitempty"`
	DurationMS    int64  `json:"duration_ms,omitempty"`
}

type materialDBTX interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func materialInsertReturningID(ctx context.Context, exec materialDBTX, dialect db.Dialect, query string, args ...any) (int64, error) {
	if dialect == db.DialectPostgres {
		var id int64
		err := exec.QueryRowContext(ctx, query+" RETURNING id", args...).Scan(&id)
		return id, err
	}
	result, err := exec.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// normalizeCollectedMaterialSpecifications removes property dimensions that
// have only one value across the collected SKU set. Xianyu requires every
// submitted SKU property to contain 2-150 distinct values. The complete PDD
// properties remain in SourceProperties, so purchasing mappings do not lose
// any source identity.
func normalizeCollectedMaterialSpecifications(skus []materialSKU) {
	values := map[string]map[string]bool{}
	for _, sku := range skus {
		if !sku.Enabled {
			continue
		}
		for _, property := range sku.Properties {
			name, value := strings.TrimSpace(property.Name), strings.TrimSpace(property.Value)
			if name == "" || value == "" {
				continue
			}
			if values[name] == nil {
				values[name] = map[string]bool{}
			}
			values[name][value] = true
		}
	}
	variable := map[string]bool{}
	for name, set := range values {
		variable[name] = len(set) >= 2
	}
	if len(variable) == 0 {
		return
	}
	for index := range skus {
		properties := make([]materialProperty, 0, len(skus[index].Properties))
		for _, property := range skus[index].Properties {
			if variable[strings.TrimSpace(property.Name)] {
				properties = append(properties, property)
			}
		}
		// A true single-SKU product still needs one local display property. It is
		// published through the normal single-SKU price/quantity path.
		if len(properties) > 0 {
			skus[index].Properties = properties
		}
	}
}

func (s *Server) mountMaterials(r chi.Router) {
	r.Get("/materials", s.listMaterials)
	r.Get("/materials/source-anomalies", s.listMaterialSourceAnomalies)
	r.Post("/materials", s.createMaterial)
	r.Post("/materials/from-pdd/{goodsID}", s.createMaterialFromPDD)
	r.Get("/materials/{id}", s.getMaterial)
	r.Put("/materials/{id}", s.updateMaterial)
	r.Delete("/materials/{id}", s.deleteMaterial)
	r.Post("/materials/images", s.uploadMaterialImage)
	r.Get("/materials/images/{name}", s.getMaterialImage)
	r.Post("/materials/{id}/publish", s.publishMaterial)
	r.Get("/materials/{id}/publish-records", s.listMaterialPublishRecords)
	r.Get("/materials/{id}/source-diff", s.materialSourceDiff)
	r.Post("/materials/{id}/sync-source", s.syncMaterialSource)
	r.Put("/materials/{id}/skus/{skuID}/source", s.updateMaterialSKUSource)
	r.Post("/materials/{id}/split", s.splitMaterial)
}

type materialSKUSourceInput struct {
	Action        string `json:"action"`
	SourceGoodsID string `json:"source_goods_id"`
	SourceSKUID   string `json:"source_sku_id"`
}

func (s *Server) updateMaterialSKUSource(w http.ResponseWriter, r *http.Request) {
	uid := auth.SessionFromContext(r.Context()).UserID
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	stableID := strings.TrimSpace(chi.URLParam(r, "skuID"))
	var input materialSKUSourceInput
	if stableID == "" || decodeJSON(r, &input) != nil {
		writeErr(w, 400, "SKU 来源操作无效")
		return
	}
	var sourceType, primarySourceID, rawSKUs string
	if err := s.Store.DB.QueryRowContext(r.Context(), `SELECT source_type,source_id,skus_json FROM product_materials WHERE id=? AND user_id=? AND deleted_at IS NULL`, id, uid).Scan(&sourceType, &primarySourceID, &rawSKUs); err != nil {
		writeErr(w, 404, "素材不存在")
		return
	}
	var skus []materialSKU
	if json.Unmarshal([]byte(rawSKUs), &skus) != nil {
		writeErr(w, 500, "素材 SKU 数据损坏")
		return
	}
	index := -1
	for row := range skus {
		if skus[row].MaterialSKUID == stableID {
			index = row
			break
		}
	}
	if index < 0 {
		writeErr(w, 404, "素材 SKU 不存在")
		return
	}
	sku := &skus[index]
	switch strings.TrimSpace(input.Action) {
	case "bind":
		goodsID, sourceSKUID := strings.TrimSpace(input.SourceGoodsID), strings.TrimSpace(input.SourceSKUID)
		if goodsID == "" || sourceSKUID == "" {
			writeErr(w, 422, "绑定来源时必须选择拼多多商品和 SKU")
			return
		}
		var specsRaw, image, pricesRaw string
		var price, stock, collectedAt int64
		var onSale int
		if err := s.Store.DB.QueryRowContext(r.Context(), `SELECT specs_json,thumb_url,prices_json,price_cent,stock,is_onsale,last_collected_at FROM pdd_skus WHERE goods_id=? AND sku_id=?`, goodsID, sourceSKUID).Scan(&specsRaw, &image, &pricesRaw, &price, &stock, &onSale, &collectedAt); err != nil {
			writeErr(w, 422, "选择的拼多多 SKU 不存在")
			return
		}
		var specs []pddSpecInput
		_ = json.Unmarshal([]byte(specsRaw), &specs)
		properties := make([]materialProperty, 0, len(specs))
		for _, spec := range specs {
			properties = append(properties, materialProperty{Name: spec.SpecKey, Value: spec.RawValue})
		}
		sku.SKUType, sku.SourceGoodsID, sku.SourceSKUID = materialSKUTypeSource, goodsID, sourceSKUID
		sku.SourceProperties, sku.SourceImageURL = properties, image
		sku.SourcePriceCents, sku.SourceNormalPriceCents = price, pddNormalPriceCent(pricesRaw)
		sku.SourcePriceUpdatedAt, sku.SourcePriceOrigin = collectedAt, "bound"
		if sku.Quantity == 0 && onSale != 0 {
			sku.Quantity = stock
		}
	case "convert_manual":
		clearMaterialSKUSource(sku)
		sku.SKUType = materialSKUTypeManual
	case "convert_placeholder":
		clearMaterialSKUSource(sku)
		sku.SKUType, sku.Quantity = materialSKUTypePlaceholder, 0
	default:
		writeErr(w, 400, "不支持的 SKU 来源操作")
		return
	}
	if err := validateMaterialSKUIdentity(sourceType, primarySourceID, sku); err != nil {
		writeErr(w, 422, err.Error())
		return
	}
	if err := validateDistinctMaterialSources(skus); err != nil {
		writeErr(w, 422, err.Error())
		return
	}
	encoded, _ := json.Marshal(skus)
	if _, err := s.Store.DB.ExecContext(r.Context(), `UPDATE product_materials SET skus_json=?,updated_at=? WHERE id=? AND user_id=?`, string(encoded), time.Now().Unix(), id, uid); err != nil {
		writeErr(w, 500, "保存 SKU 来源失败")
		return
	}
	writeJSON(w, 200, map[string]any{"success": true, "sku": sku})
}

func clearMaterialSKUSource(sku *materialSKU) {
	sku.SourceGoodsID, sku.SourceSKUID, sku.SourceImageURL = "", "", ""
	sku.SourceProperties = nil
	sku.SourcePriceCents, sku.SourceNormalPriceCents, sku.SourcePriceUpdatedAt = 0, 0, 0
	sku.SourcePriceOrigin = ""
}

func (s *Server) listMaterialSourceAnomalies(w http.ResponseWriter, r *http.Request) {
	uid := auth.SessionFromContext(r.Context()).UserID
	rows, err := s.Store.DB.QueryContext(r.Context(), `SELECT id,source_type,source_id,title,skus_json FROM product_materials WHERE user_id=? AND deleted_at IS NULL ORDER BY id`, uid)
	if err != nil {
		writeErr(w, 500, "查询 SKU 来源异常失败")
		return
	}
	defer rows.Close()
	result := []map[string]any{}
	for rows.Next() {
		var materialID int64
		var sourceType, primarySourceID, title, rawSKUs string
		if rows.Scan(&materialID, &sourceType, &primarySourceID, &title, &rawSKUs) != nil {
			continue
		}
		var skus []materialSKU
		if json.Unmarshal([]byte(rawSKUs), &skus) != nil {
			result = append(result, map[string]any{"material_id": materialID, "title": title, "issue": "invalid_json"})
			continue
		}
		seenIDs, seenSources := map[string]bool{}, map[string]bool{}
		for index := range skus {
			sku := &skus[index]
			originalType := sku.SKUType
			normalizeMaterialSKUIdentity(sourceType, primarySourceID, sku)
			issue := ""
			switch {
			case sku.MaterialSKUID == "":
				issue = "missing_material_sku_id"
			case seenIDs[sku.MaterialSKUID]:
				issue = "duplicate_material_sku_id"
			case originalType == "" && sourceType == "pdd" && sku.SourceSKUID == "":
				issue = "legacy_unclassified"
			case sku.SKUType == materialSKUTypeSource && (sku.SourceGoodsID == "" || sku.SourceSKUID == ""):
				issue = "source_binding_missing"
			case sku.SKUType == materialSKUTypePlaceholder && sku.Quantity != 0:
				issue = "placeholder_stock_nonzero"
			}
			seenIDs[sku.MaterialSKUID] = true
			sourceKey := materialSourceKey(sku.SourceGoodsID, sku.SourceSKUID)
			if issue == "" && sku.SKUType == materialSKUTypeSource && seenSources[sourceKey] {
				issue = "duplicate_source_binding"
			}
			if sku.SKUType == materialSKUTypeSource {
				seenSources[sourceKey] = true
			}
			if issue != "" {
				result = append(result, map[string]any{"material_id": materialID, "title": title, "material_sku_id": sku.MaterialSKUID, "sku_type": sku.SKUType, "source_goods_id": sku.SourceGoodsID, "source_sku_id": sku.SourceSKUID, "issue": issue})
			}
		}
	}
	writeJSON(w, 200, map[string]any{"count": len(result), "items": result})
}

type publishMaterialInput struct {
	CookieID string                `json:"cookie_id"`
	Location *mtop.PublishLocation `json:"location,omitempty"`
}

func normalizePublishedMaterialSKU(sourceType string, row map[string]any) {
	skuType := materialText(row["sku_type"])
	if skuType == materialSKUTypePlaceholder || (skuType == "" && sourceType == "pdd" && materialText(row["source_sku_id"]) == "") {
		row["quantity"] = int64(0)
	}
}

// publishMaterial turns the saved draft into the same multipart contract used by
// the normal publisher. Keeping one publish handler prevents the material and
// manual publishing paths from drifting apart.
func (s *Server) publishMaterial(w http.ResponseWriter, r *http.Request) {
	uid := auth.SessionFromContext(r.Context()).UserID
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var input publishMaterialInput
	if decodeJSON(r, &input) != nil || strings.TrimSpace(input.CookieID) == "" {
		writeErr(w, http.StatusBadRequest, "请选择发布账号")
		return
	}
	material, err := scanMaterial(s.Store.DB.QueryRowContext(r.Context(), `SELECT id,user_id,source_type,source_id,title,description,images_json,category_json,skus_json,postage_mode,postage_cent,status,created_at,updated_at,image_property_name,video_enabled,videos_json,parent_material_id,split_batch_id,split_group_name,is_split_source,publish_parameters_json,revision FROM product_materials WHERE id=? AND user_id=? AND deleted_at IS NULL`, id, uid))
	if err != nil {
		writeErr(w, http.StatusNotFound, "素材不存在")
		return
	}
	images := stringSlice(material["images"])
	if len(images) == 0 || len(images) > 9 {
		writeErr(w, http.StatusBadRequest, "素材图片必须为 1 到 9 张")
		return
	}
	if material["video_enabled"] == true && len(material["videos"].([]any)) > 0 {
		writeErr(w, http.StatusUnprocessableEntity, "素材已启用视频，但当前闲鱼视频发布协议尚未接入；请取消“发布视频”后重试")
		return
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fields := map[string]string{
		"cookie_id": input.CookieID, "title": fmt.Sprint(material["title"]),
		"description": fmt.Sprint(material["description"]), "postage_mode": fmt.Sprint(material["postage_mode"]),
		"postage": fmt.Sprintf("%.2f", float64(material["postage_cent"].(int64))/100),
	}
	if input.Location != nil {
		rawLocation, marshalErr := json.Marshal(input.Location)
		if marshalErr != nil {
			writeErr(w, http.StatusBadRequest, "发货地数据无效")
			return
		}
		fields["location"] = string(rawLocation)
	}
	skus, _ := material["skus"].([]any)
	enabledSKUs := make([]any, 0, len(skus))
	var minPrice int64
	var totalQuantity int64
	for _, raw := range skus {
		row, _ := raw.(map[string]any)
		if enabled, exists := row["enabled"]; exists && enabled == false {
			continue
		}
		// 闲鱼的两维规格必须发布完整笛卡尔组合。采集素材中没有
		// source_sku_id 的行是拼多多不存在的占位组合，保留发布但库存
		// 必须为 0，不能继承任一真实 SKU 的库存。
		normalizePublishedMaterialSKU(fmt.Sprint(material["source_type"]), row)
		enabledSKUs = append(enabledSKUs, raw)
		price := jsonInt64(row["price_cent"])
		quantity := jsonInt64(row["quantity"])
		if minPrice == 0 || price < minPrice {
			minPrice = price
		}
		totalQuantity += quantity
	}
	if len(enabledSKUs) == 0 {
		writeErr(w, http.StatusBadRequest, "素材至少需要一个启用的 SKU")
		return
	}
	if len(enabledSKUs) > 252 {
		writeErr(w, http.StatusUnprocessableEntity, "闲鱼单个商品最多发布 252 个启用的 SKU，请拆分素材或停用部分 SKU")
		return
	}
	// 采集源可能把同一张 SKU 缩略图重复写到每个规格属性。闲鱼只允许
	// 一个规格类型带图，因此在生成 multipart 和 SKU JSON 前统一清理。
	imagePropertyName := strings.TrimSpace(fmt.Sprint(material["image_property_name"]))
	for _, raw := range enabledSKUs {
		row, _ := raw.(map[string]any)
		properties, _ := row["properties"].([]any)
		for _, rawProperty := range properties {
			property, _ := rawProperty.(map[string]any)
			if imagePropertyName == "" && materialImageURL(property) != "" {
				imagePropertyName = strings.TrimSpace(fmt.Sprint(property["name"]))
			}
		}
		if imagePropertyName != "" {
			break
		}
	}
	for _, raw := range enabledSKUs {
		row, _ := raw.(map[string]any)
		properties, _ := row["properties"].([]any)
		for _, rawProperty := range properties {
			property, _ := rawProperty.(map[string]any)
			if materialImageURL(property) == "" {
				continue
			}
			name := strings.TrimSpace(fmt.Sprint(property["name"]))
			if name != imagePropertyName {
				delete(property, "image_url")
			}
		}
	}
	// A single SKU is represented by the normal price/quantity fields. The
	// seller backend only accepts itemSkuList for actual multi-SKU products.
	if len(enabledSKUs) > 1 {
		rawSKUs, _ := json.Marshal(enabledSKUs)
		fields["skus"] = string(rawSKUs)
	}
	fields["price"] = fmt.Sprintf("%.2f", float64(minPrice)/100)
	if originalPrice := jsonInt64(material["original_price_cent"]); originalPrice > 0 {
		fields["original_price"] = fmt.Sprintf("%.2f", float64(originalPrice)/100)
	}
	if totalQuantity < 1 {
		totalQuantity = 1
	}
	fields["quantity"] = strconv.FormatInt(totalQuantity, 10)
	for key, value := range fields {
		_ = mw.WriteField(key, value)
	}
	for index, source := range images {
		data, contentType, err := s.readMaterialImage(r, source)
		if err != nil {
			writeErr(w, http.StatusBadRequest, fmt.Sprintf("第 %d 张素材图片读取失败: %v", index+1, err))
			return
		}
		header, _ := createMaterialImagePart(mw, "images", fmt.Sprintf("material-%d%s", index+1, imageExtension(contentType)), contentType)
		_, _ = header.Write(data)
	}
	if len(enabledSKUs) > 1 {
		seenSpecImages := map[string]bool{}
		for skuIndex, raw := range enabledSKUs {
			row, _ := raw.(map[string]any)
			properties, _ := row["properties"].([]any)
			for propertyIndex, rawProperty := range properties {
				property, _ := rawProperty.(map[string]any)
				source := materialImageURL(property)
				key := fmt.Sprint(property["name"]) + "\x00" + fmt.Sprint(property["value"])
				if source == "" || seenSpecImages[key] {
					continue
				}
				seenSpecImages[key] = true
				data, contentType, imageErr := s.readMaterialImage(r, source)
				if imageErr != nil {
					writeErr(w, http.StatusBadRequest, fmt.Sprintf("规格 %s=%s 图片读取失败: %v", property["name"], property["value"], imageErr))
					return
				}
				part, _ := createMaterialImagePart(mw, fmt.Sprintf("spec_image_%d_%d", skuIndex, propertyIndex), "spec"+imageExtension(contentType), contentType)
				_, _ = part.Write(data)
			}
		}
	}
	_ = mw.Close()
	proxy := r.Clone(r.Context())
	proxy.Body = io.NopCloser(bytes.NewReader(body.Bytes()))
	proxy.ContentLength = int64(body.Len())
	proxy.Header = r.Header.Clone()
	proxy.Header.Set("Content-Type", mw.FormDataContentType())
	requestID := uuid.NewString()
	now := time.Now().Unix()
	snapshot, _ := json.Marshal(enabledSKUs)
	_, recordErr := s.Store.DB.ExecContext(r.Context(), `INSERT INTO material_publish_records(request_id,material_id,user_id,source_type,source_id,cookie_id,status,sku_snapshot_json,created_at) VALUES(?,?,?,?,?,?,'publishing',?,?)`, requestID, id, uid, fmt.Sprint(material["source_type"]), fmt.Sprint(material["source_id"]), input.CookieID, string(snapshot), now)
	if recordErr != nil {
		writeErr(w, http.StatusInternalServerError, "创建发布记录失败")
		return
	}
	var recordID int64
	if err := s.Store.DB.QueryRowContext(r.Context(), `SELECT id FROM material_publish_records WHERE request_id=?`, requestID).Scan(&recordID); err != nil {
		writeErr(w, http.StatusInternalServerError, "读取发布记录失败")
		return
	}
	recorder := httptest.NewRecorder()
	s.publishItem(recorder, proxy)
	resultBody := recorder.Body.Bytes()
	var publishResult map[string]any
	_ = json.Unmarshal(resultBody, &publishResult)
	status := "failed"
	itemID := ""
	if recorder.Code >= 200 && recorder.Code < 300 {
		status = "success"
		itemID = strings.TrimSpace(fmt.Sprint(publishResult["item_id"]))
	}
	errorCode, errorMessage := "", ""
	if status == "failed" {
		errorCode = strings.TrimSpace(fmt.Sprint(publishResult["code"]))
		errorMessage = strings.TrimSpace(fmt.Sprint(publishResult["message"]))
	}
	_, _ = s.Store.DB.ExecContext(r.Context(), `UPDATE material_publish_records SET published_item_id=?,status=?,error_code=?,error_message=?,finished_at=? WHERE id=?`, itemID, status, errorCode, errorMessage, time.Now().Unix(), recordID)
	for _, raw := range enabledSKUs {
		row, _ := raw.(map[string]any)
		properties, _ := json.Marshal(row["properties"])
		_, _ = s.Store.DB.ExecContext(r.Context(), `INSERT INTO material_publish_sku_mappings(publish_record_id,material_sku_id,source_goods_id,source_sku_id,published_properties_json,published_price_cent,published_quantity,mapping_status) VALUES(?,?,?,?,?,?,?,'pending')`, recordID, materialText(row["material_sku_id"]), materialText(row["source_goods_id"]), materialText(row["source_sku_id"]), string(properties), jsonInt64(row["price_cent"]), jsonInt64(row["quantity"]))
	}
	if status == "success" && itemID != "" {
		s.backfillPublishedSKUMappings(r, recordID, input.CookieID, itemID)
	}
	for key, values := range recorder.Header() {
		w.Header()[key] = values
	}
	w.WriteHeader(recorder.Code)
	_, _ = w.Write(resultBody)
}

func (s *Server) backfillPublishedSKUMappings(r *http.Request, recordID int64, cookieID, itemID string) {
	var detail string
	if err := s.Store.DB.QueryRowContext(r.Context(), `SELECT item_detail FROM item_info WHERE cookie_id=? AND item_id=?`, cookieID, itemID).Scan(&detail); err != nil {
		return
	}
	var item map[string]any
	if json.Unmarshal([]byte(detail), &item) != nil {
		return
	}
	publishRaw, _ := item["publish_raw"].(map[string]any)
	remoteSKUs, _ := publishRaw["itemSkuList"].([]any)
	remoteByProperties := map[string][]string{}
	for _, raw := range remoteSKUs {
		sku, _ := raw.(map[string]any)
		properties, _ := sku["propertyList"].([]any)
		pairs := make([]materialProperty, 0, len(properties))
		for _, rawProperty := range properties {
			property, _ := rawProperty.(map[string]any)
			pairs = append(pairs, materialProperty{
				Name:  strings.TrimSpace(fmt.Sprint(property["propertyText"])),
				Value: strings.TrimSpace(fmt.Sprint(property["actualValueText"])),
			})
		}
		if skuID := strings.TrimSpace(fmt.Sprint(sku["skuId"])); skuID != "" {
			key := materialPropertiesKey(pairs)
			remoteByProperties[key] = append(remoteByProperties[key], skuID)
		}
	}
	if len(remoteByProperties) == 0 {
		return
	}
	rows, err := s.Store.DB.QueryContext(r.Context(), `SELECT id,source_sku_id,published_properties_json FROM material_publish_sku_mappings WHERE publish_record_id=?`, recordID)
	if err != nil {
		return
	}
	defer rows.Close()
	type mappingUpdate struct {
		id     int64
		skuID  string
		status string
	}
	updates := []mappingUpdate{}
	for rows.Next() {
		var id int64
		var sourceSKUID, raw string
		if rows.Scan(&id, &sourceSKUID, &raw) != nil {
			continue
		}
		var properties []materialProperty
		_ = json.Unmarshal([]byte(raw), &properties)
		matches := remoteByProperties[materialPropertiesKey(properties)]
		if strings.TrimSpace(sourceSKUID) == "" {
			if len(matches) == 1 {
				updates = append(updates, mappingUpdate{id: id, skuID: matches[0], status: "unmapped"})
			} else {
				updates = append(updates, mappingUpdate{id: id, status: "unmapped"})
			}
			continue
		}
		switch len(matches) {
		case 1:
			updates = append(updates, mappingUpdate{id: id, skuID: matches[0], status: "mapped"})
		case 0:
			updates = append(updates, mappingUpdate{id: id, status: "unmapped"})
		default:
			updates = append(updates, mappingUpdate{id: id, status: "ambiguous"})
		}
	}
	for _, update := range updates {
		_, _ = s.Store.DB.ExecContext(r.Context(), `UPDATE material_publish_sku_mappings SET xianyu_sku_id=?,mapping_status=? WHERE id=?`, update.skuID, update.status, update.id)
	}
}

func materialPropertiesKey(properties []materialProperty) string {
	pairs := make([]string, 0, len(properties))
	for _, property := range properties {
		pairs = append(pairs, strings.TrimSpace(property.Name)+"\x00"+strings.TrimSpace(property.Value))
	}
	return strings.Join(pairs, "\x01")
}

func materialSourceKey(goodsID, skuID string) string {
	return strings.TrimSpace(goodsID) + "\x00" + strings.TrimSpace(skuID)
}

func materialText(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func materialSourceGoodsIDs(material map[string]any) []string {
	seen := map[string]bool{}
	result := []string{}
	primary := materialText(material["source_id"])
	for _, raw := range material["skus"].([]any) {
		row, _ := raw.(map[string]any)
		goodsID := materialText(row["source_goods_id"])
		if goodsID == "" && materialText(row["source_sku_id"]) != "" {
			goodsID = primary
		}
		if goodsID != "" && !seen[goodsID] {
			seen[goodsID] = true
			result = append(result, goodsID)
		}
	}
	if len(result) == 0 && primary != "" {
		result = append(result, primary)
	}
	return result
}

func materialImageURL(property map[string]any) string {
	value, ok := property["image_url"].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func createMaterialImagePart(writer *multipart.Writer, field, filename, contentType string) (io.Writer, error) {
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, field, filename))
	header.Set("Content-Type", contentType)
	return writer.CreatePart(header)
}

func stringSlice(value any) []string {
	if values, ok := value.([]string); ok {
		return values
	}
	values, _ := value.([]any)
	result := make([]string, 0, len(values))
	for _, value := range values {
		if text := strings.TrimSpace(fmt.Sprint(value)); text != "" {
			result = append(result, text)
		}
	}
	return result
}

func jsonInt64(value any) int64 {
	switch v := value.(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case json.Number:
		n, _ := v.Int64()
		return n
	default:
		n, _ := strconv.ParseInt(fmt.Sprint(v), 10, 64)
		return n
	}
}

func imageExtension(contentType string) string {
	if ext := map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/gif": ".gif", "image/webp": ".webp"}[contentType]; ext != "" {
		return ext
	}
	return ".jpg"
}

func (s *Server) readMaterialImage(r *http.Request, source string) ([]byte, string, error) {
	if strings.HasPrefix(source, "/materials/images/") {
		name := filepath.Base(source)
		data, err := os.ReadFile(filepath.Join(materialImageRoot(), name))
		if err != nil {
			return nil, "", err
		}
		return data, http.DetectContentType(data), nil
	}
	u, err := url.Parse(source)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" {
		return nil, "", errors.New("图片地址无效")
	}
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, u.String(), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, tooLarge, err := readLimitedBytes(resp.Body, 10<<20)
	if err != nil || tooLarge || len(data) == 0 {
		return nil, "", errors.New("图片为空或超过 10 MiB")
	}
	contentType := http.DetectContentType(data)
	if imageExtension(contentType) == ".jpg" && contentType != "image/jpeg" {
		return nil, "", errors.New("不是支持的图片格式")
	}
	return data, contentType, nil
}

func materialImageRoot() string { return filepath.Join(defaultPublishUploadRoot(), "material-images") }

func (s *Server) uploadMaterialImage(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		writeErr(w, 400, "图片不能超过 10 MiB")
		return
	}
	f, _, err := r.FormFile("image")
	if err != nil {
		writeErr(w, 400, "请选择图片")
		return
	}
	defer f.Close()
	data, tooLarge, err := readLimitedBytes(f, 10<<20)
	if err != nil || tooLarge || len(data) == 0 {
		writeErr(w, 400, "图片无效或超过 10 MiB")
		return
	}
	ext := map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/gif": ".gif", "image/webp": ".webp"}[http.DetectContentType(data)]
	if ext == "" {
		writeErr(w, 400, "仅支持 JPG、PNG、GIF、WebP")
		return
	}
	if err = os.MkdirAll(materialImageRoot(), 0o700); err != nil {
		writeErr(w, 500, "创建图片目录失败")
		return
	}
	name := strconv.FormatInt(time.Now().UnixNano(), 36) + ext
	if err = os.WriteFile(filepath.Join(materialImageRoot(), name), data, 0o600); err != nil {
		writeErr(w, 500, "保存图片失败")
		return
	}
	writeJSON(w, 201, map[string]any{"url": "/materials/images/" + name})
}
func (s *Server) getMaterialImage(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(chi.URLParam(r, "name"))
	if name == "." || name == "" {
		writeErr(w, 404, "图片不存在")
		return
	}
	http.ServeFile(w, r, filepath.Join(materialImageRoot(), name))
}

func validateMaterial(in *materialInput) error {
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		return errors.New("素材标题不能为空")
	}
	if len(in.Images) == 0 || len(in.Images) > 9 {
		return errors.New("素材图片必须为 1 到 9 张")
	}
	if in.OriginalPriceCents < 0 {
		return errors.New("闲鱼原价不能小于 0")
	}
	minPrice := int64(0)
	for _, sku := range in.SKUs {
		if sku.Enabled && (minPrice == 0 || sku.PriceCents < minPrice) {
			minPrice = sku.PriceCents
		}
	}
	if in.OriginalPriceCents > 0 && in.OriginalPriceCents < minPrice {
		return errors.New("闲鱼原价不能低于最低闲鱼售价")
	}
	validPriceModes := map[string]bool{"": true, "manual": true, "fixed_add": true, "percent_add": true, "margin": true}
	if !validPriceModes[in.PriceStrategy.Mode] {
		return errors.New("售价策略无效")
	}
	validStockModes := map[string]bool{"": true, "manual": true, "mirror": true, "cap": true, "fixed": true}
	if !validStockModes[in.StockStrategy.Mode] {
		return errors.New("库存策略无效")
	}
	if in.PriceStrategy.MinimumProfitCent < 0 || in.StockStrategy.Cap < 0 || in.StockStrategy.Reserve < 0 || in.StockStrategy.FixedQuantity < 0 {
		return errors.New("售价或库存策略参数不能小于 0")
	}
	if in.VideoEnabled == nil {
		enabled := true
		in.VideoEnabled = &enabled
	}
	if len(in.Videos) > 100 {
		return errors.New("素材视频不能超过 100 个")
	}
	for i := range in.Videos {
		if strings.TrimSpace(in.Videos[i].URL) == "" {
			return errors.New("素材视频地址不能为空")
		}
	}
	if len(in.SKUs) == 0 || len(in.SKUs) > 5000 {
		return errors.New("素材必须包含 1 到 5000 个 SKU")
	}
	seen := map[string]bool{}
	for idx := range in.SKUs {
		sku := &in.SKUs[idx]
		sku.SourceGoodsID = strings.TrimSpace(sku.SourceGoodsID)
		sku.SourceSKUID = strings.TrimSpace(sku.SourceSKUID)
		if strings.TrimSpace(sku.MaterialSKUID) == "" {
			sku.MaterialSKUID = uuid.NewString()
		}
		if sku.PriceCents <= 0 || sku.Quantity < 0 || len(sku.Properties) == 0 {
			return errors.New("SKU 售价、库存或规格无效")
		}
		parts := []string{}
		for j := range sku.Properties {
			sku.Properties[j].Name = strings.TrimSpace(sku.Properties[j].Name)
			sku.Properties[j].Value = strings.TrimSpace(sku.Properties[j].Value)
			if sku.Properties[j].Name == "" || sku.Properties[j].Value == "" {
				return errors.New("SKU 规格不能为空")
			}
			parts = append(parts, sku.Properties[j].Name+"="+sku.Properties[j].Value)
		}
		key := strings.Join(parts, "\x00")
		if seen[key] {
			return errors.New("SKU 规格组合不能重复")
		}
		seen[key] = true
	}
	if in.PostageMode == "" {
		in.PostageMode = "free"
	}
	return nil
}

const materialSelectColumns = `id,user_id,source_type,source_id,title,description,images_json,category_json,skus_json,postage_mode,postage_cent,status,created_at,updated_at,image_property_name,video_enabled,videos_json,parent_material_id,split_batch_id,split_group_name,is_split_source,publish_parameters_json,revision`

func materialMap(id, userID int64, sourceType, sourceID, title, description, images, category, skus, postageMode, status, imagePropertyName, videos string, postage, created, updated int64, videoEnabled int, parentMaterialID int64, splitBatchID, splitGroupName string, isSplitSource int, parametersRaw string, revision int64) map[string]any {
	material := map[string]any{"id": id, "user_id": userID, "source_type": sourceType, "source_id": sourceID, "title": title, "description": description, "images": jsonValue(images, []string{}), "category": jsonValue(category, map[string]any{}), "skus": jsonValue(skus, []any{}), "postage_mode": postageMode, "postage_cent": postage, "status": status, "image_property_name": imagePropertyName, "video_enabled": videoEnabled != 0, "videos": jsonValue(videos, []any{}), "created_at": created, "updated_at": updated}
	var parameters materialPublishParameters
	_ = json.Unmarshal([]byte(parametersRaw), &parameters)
	normalizePublishParameters(&parameters, stringSlice(material["images"]))
	material["original_price_cent"], material["source_properties"], material["image_metadata"] = parameters.OriginalPriceCents, parameters.SourceProperties, parameters.ImageMetadata
	material["price_strategy"], material["stock_strategy"], material["revision"] = parameters.PriceStrategy, parameters.StockStrategy, revision
	material["parent_material_id"], material["split_batch_id"], material["split_group_name"], material["is_split_source"] = parentMaterialID, splitBatchID, splitGroupName, isSplitSource != 0
	material["source_ids"] = materialSourceGoodsIDs(material)
	digest := sha256.Sum256([]byte(strings.Join([]string{title, description, images, category, skus, postageMode, strconv.FormatInt(postage, 10), imagePropertyName, strconv.Itoa(videoEnabled), videos}, "\x00")))
	material["split_version"] = hex.EncodeToString(digest[:])
	return material
}
func scanMaterial(row interface{ Scan(...any) error }) (map[string]any, error) {
	var id, userID, postage, created, updated, parentMaterialID, revision int64
	var videoEnabled, isSplitSource int
	var sourceType, sourceID, title, description, images, category, skus, postageMode, status, imagePropertyName, videos, splitBatchID, splitGroupName, parametersRaw string
	err := row.Scan(&id, &userID, &sourceType, &sourceID, &title, &description, &images, &category, &skus, &postageMode, &postage, &status, &created, &updated, &imagePropertyName, &videoEnabled, &videos, &parentMaterialID, &splitBatchID, &splitGroupName, &isSplitSource, &parametersRaw, &revision)
	return materialMap(id, userID, sourceType, sourceID, title, description, images, category, skus, postageMode, status, imagePropertyName, videos, postage, created, updated, videoEnabled, parentMaterialID, splitBatchID, splitGroupName, isSplitSource, parametersRaw, revision), err
}

// backfillMaterialSourcePrices upgrades old JSON rows without treating the
// current PDD price as the historical purchase price. The Xianyu sale price in
// price_cent is never changed.
func (s *Server) backfillMaterialSourcePrices(ctx context.Context, material map[string]any) bool {
	if materialText(material["source_type"]) != "pdd" {
		return false
	}
	var skus []materialSKU
	raw, _ := json.Marshal(material["skus"])
	if json.Unmarshal(raw, &skus) != nil {
		return false
	}
	changed := false
	for index := range skus {
		if skus[index].SourceSKUID == "" || skus[index].SourcePriceCents > 0 {
			continue
		}
		goodsID := skus[index].SourceGoodsID
		if goodsID == "" {
			goodsID = materialText(material["source_id"])
		}
		var pricesRaw string
		var price, collectedAt int64
		if s.Store.DB.QueryRowContext(ctx, `SELECT prices_json,price_cent,last_collected_at FROM pdd_skus WHERE goods_id=? AND sku_id=?`, goodsID, skus[index].SourceSKUID).Scan(&pricesRaw, &price, &collectedAt) != nil || price <= 0 {
			continue
		}
		skus[index].SourceGoodsID = goodsID
		skus[index].SourcePriceCents = price
		skus[index].SourceNormalPriceCents = pddNormalPriceCent(pricesRaw)
		skus[index].SourcePriceUpdatedAt = collectedAt
		skus[index].SourcePriceOrigin = "backfilled_current"
		changed = true
	}
	if !changed {
		return false
	}
	encoded, _ := json.Marshal(skus)
	material["skus"] = jsonValue(string(encoded), []any{})
	_, _ = s.Store.DB.ExecContext(ctx, `UPDATE product_materials SET skus_json=? WHERE id=?`, string(encoded), jsonInt64(material["id"]))
	return true
}

func (s *Server) listMaterials(w http.ResponseWriter, r *http.Request) {
	uid := auth.SessionFromContext(r.Context()).UserID
	query := `SELECT id,user_id,source_type,source_id,title,description,images_json,category_json,skus_json,postage_mode,postage_cent,status,created_at,updated_at,image_property_name,video_enabled,videos_json,parent_material_id,split_batch_id,split_group_name,is_split_source,publish_parameters_json,revision FROM product_materials WHERE user_id=? AND deleted_at IS NULL`
	args := []any{uid}
	if keyword := strings.TrimSpace(r.URL.Query().Get("q")); keyword != "" {
		query += ` AND (title LIKE ? OR source_id LIKE ? OR skus_json LIKE ?)`
		pattern := "%" + keyword + "%"
		args = append(args, pattern, pattern, pattern)
	}
	query += ` ORDER BY updated_at DESC`
	rows, err := s.Store.DB.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeErr(w, 500, "查询素材失败")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		m, e := scanMaterial(rows)
		if e != nil {
			writeErr(w, 500, "读取素材失败")
			return
		}
		out = append(out, m)
	}
	_ = rows.Close()
	for _, material := range out {
		if s.backfillMaterialSourcePrices(r.Context(), material) {
			if refreshed, refreshErr := scanMaterial(s.Store.DB.QueryRowContext(r.Context(), `SELECT id,user_id,source_type,source_id,title,description,images_json,category_json,skus_json,postage_mode,postage_cent,status,created_at,updated_at,image_property_name,video_enabled,videos_json,parent_material_id,split_batch_id,split_group_name,is_split_source,publish_parameters_json,revision FROM product_materials WHERE id=? AND user_id=? AND deleted_at IS NULL`, jsonInt64(material["id"]), uid)); refreshErr == nil {
				for key := range material {
					delete(material, key)
				}
				for key, value := range refreshed {
					material[key] = value
				}
			}
		}
		s.attachMaterialSplitOccupancy(r.Context(), uid, material)
	}
	writeJSON(w, 200, out)
}
func (s *Server) getMaterial(w http.ResponseWriter, r *http.Request) {
	uid := auth.SessionFromContext(r.Context()).UserID
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	m, err := scanMaterial(s.Store.DB.QueryRowContext(r.Context(), `SELECT id,user_id,source_type,source_id,title,description,images_json,category_json,skus_json,postage_mode,postage_cent,status,created_at,updated_at,image_property_name,video_enabled,videos_json,parent_material_id,split_batch_id,split_group_name,is_split_source,publish_parameters_json,revision FROM product_materials WHERE id=? AND user_id=? AND deleted_at IS NULL`, id, uid))
	if err != nil {
		writeErr(w, 404, "素材不存在")
		return
	}
	if s.backfillMaterialSourcePrices(r.Context(), m) {
		if refreshed, refreshErr := scanMaterial(s.Store.DB.QueryRowContext(r.Context(), `SELECT id,user_id,source_type,source_id,title,description,images_json,category_json,skus_json,postage_mode,postage_cent,status,created_at,updated_at,image_property_name,video_enabled,videos_json,parent_material_id,split_batch_id,split_group_name,is_split_source,publish_parameters_json,revision FROM product_materials WHERE id=? AND user_id=? AND deleted_at IS NULL`, id, uid)); refreshErr == nil {
			m = refreshed
		}
	}
	s.attachMaterialSplitOccupancy(r.Context(), uid, m)
	writeJSON(w, 200, m)
}

func (s *Server) attachMaterialSplitOccupancy(ctx context.Context, uid int64, material map[string]any) {
	rootID := jsonInt64(material["parent_material_id"])
	if rootID == 0 {
		rootID = jsonInt64(material["id"])
	}
	rows, err := s.Store.DB.QueryContext(ctx, `SELECT material_sku_id FROM material_split_assignments WHERE user_id=? AND root_material_id=? ORDER BY material_sku_id`, uid, rootID)
	if err != nil {
		material["split_occupied_sku_ids"] = []string{}
		return
	}
	defer rows.Close()
	occupied := []string{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			occupied = append(occupied, id)
		}
	}
	material["split_occupied_sku_ids"] = occupied
}
func (s *Server) createMaterial(w http.ResponseWriter, r *http.Request) {
	var in materialInput
	if decodeJSON(r, &in) != nil || protectMaterialSKUIdentities("manual", "", nil, in.SKUs) != nil || validateMaterial(&in) != nil {
		writeErr(w, 400, "素材数据无效")
		return
	}
	s.insertMaterial(w, r, "manual", "", in)
}
func (s *Server) insertMaterial(w http.ResponseWriter, r *http.Request, sourceType, sourceID string, in materialInput) {
	uid := auth.SessionFromContext(r.Context()).UserID
	images, _ := json.Marshal(in.Images)
	cat, _ := json.Marshal(in.Category)
	skus, _ := json.Marshal(in.SKUs)
	videos, _ := json.Marshal(in.Videos)
	parameters := publishParametersFromInput(in)
	normalizePublishParameters(&parameters, in.Images)
	parametersJSON, _ := json.Marshal(parameters)
	videoEnabled := 0
	if in.VideoEnabled != nil && *in.VideoEnabled {
		videoEnabled = 1
	}
	now := time.Now().Unix()
	id, err := materialInsertReturningID(r.Context(), s.Store.DB, s.Store.Dialect, `INSERT INTO product_materials(user_id,source_type,source_id,title,description,images_json,category_json,skus_json,postage_mode,postage_cent,status,created_at,updated_at,image_property_name,video_enabled,videos_json,is_split_source,publish_parameters_json,revision) VALUES(?,?,?,?,?,?,?,?,?,?,'draft',?,?,?,?,?,?,?,1)`, uid, sourceType, sourceID, in.Title, in.Description, string(images), string(cat), string(skus), in.PostageMode, in.PostageCents, now, now, in.ImagePropertyName, videoEnabled, string(videos), 0, string(parametersJSON))
	if err != nil {
		writeErr(w, 500, "创建素材失败")
		return
	}
	writeJSON(w, 201, map[string]any{"success": true, "id": id})
}
func (s *Server) createMaterialFromPDD(w http.ResponseWriter, r *http.Request) {
	goodsID := chi.URLParam(r, "goodsID")
	var title, images, productVideos, productProperties string
	var productID int64
	if err := s.Store.DB.QueryRowContext(r.Context(), `SELECT id,title,images_json,videos_json,properties_json FROM pdd_products WHERE goods_id=?`, goodsID).Scan(&productID, &title, &images, &productVideos, &productProperties); err != nil {
		writeErr(w, 404, "采集商品不存在")
		return
	}
	rows, err := s.Store.DB.QueryContext(r.Context(), `SELECT sku_id,specs_json,thumb_url,prices_json,price_cent,stock,is_onsale,last_collected_at FROM pdd_skus WHERE product_id=? ORDER BY id`, productID)
	if err != nil {
		writeErr(w, 500, "读取采集 SKU 失败")
		return
	}
	defer rows.Close()
	skus := []materialSKU{}
	for rows.Next() {
		var skuID, specRaw, thumb, pricesRaw string
		var price, stock, collectedAt int64
		var enabled int
		_ = rows.Scan(&skuID, &specRaw, &thumb, &pricesRaw, &price, &stock, &enabled, &collectedAt)
		var specs []pddSpecInput
		_ = json.Unmarshal([]byte(specRaw), &specs)
		props := []materialProperty{}
		for _, p := range specs {
			props = append(props, materialProperty{Name: p.SpecKey, Value: p.RawValue})
		}
		skus = append(skus, materialSKU{MaterialSKUID: uuid.NewString(), SKUType: materialSKUTypeSource, SourceGoodsID: goodsID, SourceSKUID: skuID, SourceProperties: append([]materialProperty(nil), props...), SourceImageURL: thumb, SourcePriceCents: price, SourceNormalPriceCents: pddNormalPriceCent(pricesRaw), SourcePriceUpdatedAt: collectedAt, SourcePriceOrigin: "collected", PriceCents: price, Quantity: stock, Enabled: enabled != 0, Properties: props, ImageURL: thumb})
	}
	normalizeCollectedMaterialSpecifications(skus)
	var imageList []string
	_ = json.Unmarshal([]byte(images), &imageList)
	cleanImages := []string{}
	for _, u := range imageList {
		if strings.TrimSpace(u) != "" {
			cleanImages = append(cleanImages, u)
			if len(cleanImages) == 9 {
				break
			}
		}
	}
	if len(cleanImages) == 0 {
		for _, sku := range skus {
			if strings.TrimSpace(sku.ImageURL) != "" {
				cleanImages = append(cleanImages, sku.ImageURL)
				break
			}
		}
	}
	var collectedVideos []pddProductVideoInput
	_ = json.Unmarshal([]byte(productVideos), &collectedVideos)
	videos := make([]materialVideo, 0, len(collectedVideos))
	for _, video := range collectedVideos {
		if strings.TrimSpace(video.URL) != "" {
			videos = append(videos, materialVideo{Source: "product", SourceGoodsID: goodsID, URL: video.URL, CoverURL: video.CoverURL, DurationMS: video.DurationMS})
		}
	}
	videoEnabled := true
	var collectedProperties []pddGoodsPropertyInput
	_ = json.Unmarshal([]byte(productProperties), &collectedProperties)
	sourceProperties := make([]materialSourceProperty, 0, len(collectedProperties))
	for _, property := range collectedProperties {
		sourceProperties = append(sourceProperties, materialSourceProperty{Name: property.Key, Values: property.Values})
	}
	imageMetadata := make([]materialImageMetadata, 0, len(cleanImages))
	for _, image := range cleanImages {
		imageMetadata = append(imageMetadata, materialImageMetadata{URL: image, Source: "pdd_product", SourceGoodsID: goodsID, Status: "valid"})
	}
	in := materialInput{Title: title, Description: title, Images: cleanImages, Category: map[string]any{}, SKUs: skus, PostageMode: "free", VideoEnabled: &videoEnabled, Videos: videos, SourceProperties: sourceProperties, ImageMetadata: imageMetadata, PriceStrategy: materialPriceStrategy{Mode: "manual", MinimumProfitCent: 50}, StockStrategy: materialStockStrategy{Mode: "manual", DisableWhenOOS: true}}
	if err := protectMaterialSKUIdentities("pdd", goodsID, nil, in.SKUs); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := validateMaterial(&in); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	s.insertMaterial(w, r, "pdd", goodsID, in)
}
func (s *Server) updateMaterial(w http.ResponseWriter, r *http.Request) {
	uid := auth.SessionFromContext(r.Context()).UserID
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var in materialInput
	if decodeJSON(r, &in) != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	var sourceType, primarySourceID, oldSKUsJSON string
	var parentMaterialID, currentRevision int64
	var existingSplitSource int
	if err := s.Store.DB.QueryRowContext(r.Context(), `SELECT source_type,source_id,skus_json,parent_material_id,is_split_source,revision FROM product_materials WHERE id=? AND user_id=? AND deleted_at IS NULL`, id, uid).Scan(&sourceType, &primarySourceID, &oldSKUsJSON, &parentMaterialID, &existingSplitSource, &currentRevision); err != nil {
		writeErr(w, 404, "素材不存在")
		return
	}
	var oldSKUs []materialSKU
	if json.Unmarshal([]byte(oldSKUsJSON), &oldSKUs) != nil {
		writeErr(w, 500, "素材 SKU 数据损坏")
		return
	}
	if err := protectMaterialSKUIdentities(sourceType, primarySourceID, oldSKUs, in.SKUs); err != nil {
		writeErr(w, 422, err.Error())
		return
	}
	if err := validateMaterial(&in); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	images, _ := json.Marshal(in.Images)
	cat, _ := json.Marshal(in.Category)
	skus, _ := json.Marshal(in.SKUs)
	videos, _ := json.Marshal(in.Videos)
	parameters := publishParametersFromInput(in)
	normalizePublishParameters(&parameters, in.Images)
	parametersJSON, _ := json.Marshal(parameters)
	videoEnabled := 0
	if in.VideoEnabled != nil && *in.VideoEnabled {
		videoEnabled = 1
	}
	expectedRevision := in.Revision
	if expectedRevision <= 0 {
		expectedRevision = currentRevision
	}
	res, err := s.Store.DB.ExecContext(r.Context(), `UPDATE product_materials SET title=?,description=?,images_json=?,category_json=?,skus_json=?,postage_mode=?,postage_cent=?,image_property_name=?,video_enabled=?,videos_json=?,is_split_source=?,publish_parameters_json=?,revision=revision+1,updated_at=? WHERE id=? AND user_id=? AND deleted_at IS NULL AND revision=?`, in.Title, in.Description, string(images), string(cat), string(skus), in.PostageMode, in.PostageCents, in.ImagePropertyName, videoEnabled, string(videos), existingSplitSource, string(parametersJSON), time.Now().Unix(), id, uid, expectedRevision)
	if err != nil {
		writeErr(w, 500, "更新素材失败")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeErr(w, http.StatusConflict, "素材已被其他页面修改，请刷新后重试")
		return
	}
	writeJSON(w, 200, map[string]any{"success": true, "revision": expectedRevision + 1})
}
func (s *Server) deleteMaterial(w http.ResponseWriter, r *http.Request) {
	uid := auth.SessionFromContext(r.Context()).UserID
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	tx, err := s.Store.DB.BeginTx(r.Context(), nil)
	if err != nil {
		writeErr(w, 500, "开始删除事务失败")
		return
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	res, err := tx.ExecContext(r.Context(), `UPDATE product_materials SET deleted_at=?,updated_at=? WHERE id=? AND user_id=? AND deleted_at IS NULL`, now, now, id, uid)
	if err != nil {
		writeErr(w, 500, "删除素材失败")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeErr(w, 404, "素材不存在")
		return
	}
	if _, err = tx.ExecContext(r.Context(), `DELETE FROM material_split_assignments WHERE user_id=? AND child_material_id=?`, uid, id); err != nil {
		writeErr(w, 500, "释放拆分 SKU 占用失败")
		return
	}
	if err = tx.Commit(); err != nil {
		writeErr(w, 500, "提交删除事务失败")
		return
	}
	writeJSON(w, 200, map[string]any{"success": true})
}

type materialSplitGroupInput struct {
	Name           string   `json:"name"`
	Title          string   `json:"title"`
	MaterialSKUIDs []string `json:"material_sku_ids"`
}

type materialSplitInput struct {
	Groups          []materialSplitGroupInput `json:"groups"`
	ExpectedVersion string                    `json:"expected_version"`
	IdempotencyKey  string                    `json:"idempotency_key"`
}

// splitMaterial only creates local child drafts. It deliberately preserves the
// stable SKU and source fields byte-for-byte so publishing a child cannot sever
// fulfillment mappings from its collected PDD SKU.
func (s *Server) splitMaterial(w http.ResponseWriter, r *http.Request) {
	uid := auth.SessionFromContext(r.Context()).UserID
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var input materialSplitInput
	if id <= 0 || decodeJSON(r, &input) != nil || len(input.Groups) == 0 || len(input.Groups) > 20 {
		writeErr(w, 400, "拆分请求无效；每批需要 1 到 20 个分组")
		return
	}
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.IdempotencyKey == "" {
		input.IdempotencyKey = strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	}
	if input.IdempotencyKey == "" {
		input.IdempotencyKey = uuid.NewString()
	}
	if len(input.IdempotencyKey) > 191 {
		writeErr(w, 400, "幂等键过长")
		return
	}
	if replayMaterialSplit(w, r, s, uid, id, input.IdempotencyKey) {
		return
	}

	s.materialSplitMu.Lock()
	defer s.materialSplitMu.Unlock()
	if replayMaterialSplit(w, r, s, uid, id, input.IdempotencyKey) {
		return
	}
	tx, err := s.Store.DB.BeginTx(r.Context(), nil)
	if err != nil {
		writeErr(w, 500, "开始拆分事务失败")
		return
	}
	defer tx.Rollback()
	batchID, now := uuid.NewString(), time.Now().Unix()
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO material_split_batches(id,user_id,source_material_id,idempotency_key,response_json,created_at) VALUES(?,?,?,?,?,?)`, batchID, uid, id, input.IdempotencyKey, "", now); err != nil {
		_ = tx.Rollback()
		if replayMaterialSplit(w, r, s, uid, id, input.IdempotencyKey) {
			return
		}
		writeErr(w, 409, "相同拆分请求正在执行，请稍后重试")
		return
	}
	var originalTitle, originalDescription, originalImages, originalCategory, sourceSKUsJSON, originalPostageMode, originalImageProperty, originalVideos, originalParameters string
	var originalPostage int64
	var originalVideoEnabled int
	if err = tx.QueryRowContext(r.Context(), `SELECT title,description,images_json,category_json,skus_json,postage_mode,postage_cent,image_property_name,video_enabled,videos_json,publish_parameters_json FROM product_materials WHERE id=? AND user_id=? AND deleted_at IS NULL`, id, uid).Scan(&originalTitle, &originalDescription, &originalImages, &originalCategory, &sourceSKUsJSON, &originalPostageMode, &originalPostage, &originalImageProperty, &originalVideoEnabled, &originalVideos, &originalParameters); err != nil {
		writeErr(w, 404, "素材不存在")
		return
	}
	material, err := scanMaterial(tx.QueryRowContext(r.Context(), `SELECT id,user_id,source_type,source_id,title,description,images_json,category_json,skus_json,postage_mode,postage_cent,status,created_at,updated_at,image_property_name,video_enabled,videos_json,parent_material_id,split_batch_id,split_group_name,is_split_source,publish_parameters_json,revision FROM product_materials WHERE id=? AND user_id=? AND deleted_at IS NULL`, id, uid))
	if err != nil {
		writeErr(w, 404, "素材不存在")
		return
	}
	if input.ExpectedVersion != "" && input.ExpectedVersion != materialText(material["split_version"]) {
		writeErr(w, 409, "素材内容已变化，请重新生成拆分预览")
		return
	}
	if jsonInt64(material["parent_material_id"]) > 0 {
		writeErr(w, 409, "拆分子素材不能再次拆分；请回到原始拆分源调整分组")
		return
	}
	var sourceSKUs []materialSKU
	if json.Unmarshal([]byte(sourceSKUsJSON), &sourceSKUs) != nil {
		writeErr(w, 500, "素材 SKU 数据损坏")
		return
	}
	byID := make(map[string]materialSKU, len(sourceSKUs))
	for _, sku := range sourceSKUs {
		stableID := strings.TrimSpace(sku.MaterialSKUID)
		if stableID == "" {
			writeErr(w, 422, "素材存在未生成稳定 ID 的 SKU，请先保存素材后再拆分")
			return
		}
		byID[stableID] = sku
	}
	selected := map[string]bool{}
	groupNames := map[string]bool{}
	groupSKUs := make([][]materialSKU, len(input.Groups))
	for index := range input.Groups {
		name := strings.TrimSpace(input.Groups[index].Name)
		if name == "" || len([]rune(name)) > 40 || groupNames[name] || len(input.Groups[index].MaterialSKUIDs) == 0 || len(input.Groups[index].MaterialSKUIDs) > materialSKUGroupLimit {
			writeErr(w, 422, "分组名称必须唯一且不超过 40 字，每组必须包含 1 到 252 个 SKU")
			return
		}
		groupNames[name] = true
		for _, rawID := range input.Groups[index].MaterialSKUIDs {
			stableID := strings.TrimSpace(rawID)
			sku, exists := byID[stableID]
			if !exists {
				writeErr(w, 422, "分组包含不属于当前素材的 SKU: "+stableID)
				return
			}
			if selected[stableID] {
				writeErr(w, 422, "同一 SKU 不能同时分配到多个子素材: "+stableID)
				return
			}
			selected[stableID] = true
			groupSKUs[index] = append(groupSKUs[index], sku)
		}
	}

	rootID := jsonInt64(material["parent_material_id"])
	if rootID == 0 {
		rootID = id
	}
	// An active sibling owns its selected stable IDs until that child is deleted.
	rows, queryErr := tx.QueryContext(r.Context(), `SELECT skus_json FROM product_materials WHERE user_id=? AND parent_material_id=? AND deleted_at IS NULL`, uid, rootID)
	if queryErr != nil {
		writeErr(w, 500, "检查已有拆分素材失败")
		return
	}
	for rows.Next() {
		var encoded string
		_ = rows.Scan(&encoded)
		var siblingSKUs []materialSKU
		_ = json.Unmarshal([]byte(encoded), &siblingSKUs)
		for _, sku := range siblingSKUs {
			if selected[sku.MaterialSKUID] {
				_ = rows.Close()
				writeErr(w, 409, "SKU 已存在于未删除的拆分子素材中: "+sku.MaterialSKUID)
				return
			}
		}
	}
	_ = rows.Close()

	children := make([]map[string]any, 0, len(input.Groups))
	for index, group := range input.Groups {
		encodedSKUs, _ := json.Marshal(groupSKUs[index])
		title := strings.TrimSpace(group.Title)
		if title == "" {
			title = fmt.Sprintf("%s - %s", materialText(material["title"]), strings.TrimSpace(group.Name))
		}
		childID, insertErr := materialInsertReturningID(r.Context(), tx, s.Store.Dialect, `INSERT INTO product_materials(user_id,source_type,source_id,title,description,images_json,category_json,skus_json,postage_mode,postage_cent,status,created_at,updated_at,image_property_name,video_enabled,videos_json,parent_material_id,split_batch_id,split_group_name,is_split_source,publish_parameters_json,revision) VALUES(?,?,?,?,?,?,?,?,?,?,'draft',?,?,?,?,?,?,?,?,?,?,1)`, uid, materialText(material["source_type"]), materialText(material["source_id"]), title, materialText(material["description"]), mustJSON(material["images"]), mustJSON(material["category"]), string(encodedSKUs), materialText(material["postage_mode"]), jsonInt64(material["postage_cent"]), now, now, materialText(material["image_property_name"]), materialBoolInt(material["video_enabled"] == true), mustJSON(material["videos"]), rootID, batchID, strings.TrimSpace(group.Name), 0, originalParameters)
		if insertErr != nil {
			writeErr(w, 500, "创建拆分子素材失败")
			return
		}
		for _, sku := range groupSKUs[index] {
			if _, assignErr := tx.ExecContext(r.Context(), `INSERT INTO material_split_assignments(user_id,root_material_id,child_material_id,material_sku_id,split_batch_id,created_at) VALUES(?,?,?,?,?,?)`, uid, rootID, childID, sku.MaterialSKUID, batchID, now); assignErr != nil {
				writeErr(w, 409, "SKU 已被其他拆分任务占用: "+sku.MaterialSKUID)
				return
			}
		}
		children = append(children, map[string]any{"id": childID, "name": strings.TrimSpace(group.Name), "title": title, "sku_count": len(groupSKUs[index])})
	}
	result, err := tx.ExecContext(r.Context(), `UPDATE product_materials SET is_split_source=1,revision=revision+1,updated_at=? WHERE id=? AND user_id=? AND deleted_at IS NULL AND title=? AND description=? AND images_json=? AND category_json=? AND skus_json=? AND postage_mode=? AND postage_cent=? AND image_property_name=? AND video_enabled=? AND videos_json=? AND publish_parameters_json=?`, now, id, uid, originalTitle, originalDescription, originalImages, originalCategory, sourceSKUsJSON, originalPostageMode, originalPostage, originalImageProperty, originalVideoEnabled, originalVideos, originalParameters)
	if err != nil {
		writeErr(w, 500, "标记拆分源失败")
		return
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		writeErr(w, 409, "素材内容已变化，请重新生成拆分预览")
		return
	}
	response := map[string]any{"success": true, "batch_id": batchID, "children": children, "selected_count": len(selected), "remaining_count": len(sourceSKUs) - len(selected)}
	responseRaw, _ := json.Marshal(response)
	if _, err = tx.ExecContext(r.Context(), `UPDATE material_split_batches SET response_json=? WHERE id=?`, string(responseRaw), batchID); err != nil {
		writeErr(w, 500, "保存拆分结果失败")
		return
	}
	if err = tx.Commit(); err != nil {
		writeErr(w, 500, "提交拆分事务失败")
		return
	}
	writeJSON(w, 201, response)
}

func replayMaterialSplit(w http.ResponseWriter, r *http.Request, s *Server, uid, materialID int64, key string) bool {
	var response string
	err := s.Store.DB.QueryRowContext(r.Context(), `SELECT response_json FROM material_split_batches WHERE user_id=? AND source_material_id=? AND idempotency_key=?`, uid, materialID, key).Scan(&response)
	if err != nil || strings.TrimSpace(response) == "" {
		return false
	}
	var payload map[string]any
	if json.Unmarshal([]byte(response), &payload) != nil {
		return false
	}
	payload["replayed"] = true
	writeJSON(w, 200, payload)
	return true
}

func mustJSON(value any) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}

func materialBoolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func (s *Server) listMaterialPublishRecords(w http.ResponseWriter, r *http.Request) {
	uid := auth.SessionFromContext(r.Context()).UserID
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	pendingRows, pendingErr := s.Store.DB.QueryContext(r.Context(), `SELECT id,cookie_id,published_item_id FROM material_publish_records WHERE material_id=? AND user_id=? AND status='success' AND published_item_id<>'' AND EXISTS(SELECT 1 FROM material_publish_sku_mappings WHERE publish_record_id=material_publish_records.id AND mapping_status='pending')`, id, uid)
	if pendingErr == nil {
		type pendingRecord struct {
			id               int64
			cookieID, itemID string
		}
		pending := []pendingRecord{}
		for pendingRows.Next() {
			var record pendingRecord
			if pendingRows.Scan(&record.id, &record.cookieID, &record.itemID) == nil {
				pending = append(pending, record)
			}
		}
		_ = pendingRows.Close()
		for _, record := range pending {
			s.backfillPublishedSKUMappings(r, record.id, record.cookieID, record.itemID)
		}
	}
	rows, err := s.Store.DB.QueryContext(r.Context(), `SELECT id,request_id,cookie_id,published_item_id,status,error_code,error_message,created_at,finished_at FROM material_publish_records WHERE material_id=? AND user_id=? ORDER BY created_at DESC`, id, uid)
	if err != nil {
		writeErr(w, 500, "查询发布记录失败")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var recordID, created, finished int64
		var requestID, cookieID, itemID, status, code, message string
		if rows.Scan(&recordID, &requestID, &cookieID, &itemID, &status, &code, &message, &created, &finished) == nil {
			mappingCounts := map[string]int64{"pending": 0, "mapped": 0, "unmapped": 0, "ambiguous": 0}
			mappingRows, mappingErr := s.Store.DB.QueryContext(r.Context(), `SELECT mapping_status,COUNT(*) FROM material_publish_sku_mappings WHERE publish_record_id=? GROUP BY mapping_status`, recordID)
			if mappingErr == nil {
				for mappingRows.Next() {
					var mappingStatus string
					var count int64
					if mappingRows.Scan(&mappingStatus, &count) == nil {
						mappingCounts[mappingStatus] = count
					}
				}
				_ = mappingRows.Close()
			}
			out = append(out, map[string]any{"id": recordID, "request_id": requestID, "cookie_id": cookieID, "published_item_id": itemID, "status": status, "error_code": code, "error_message": message, "created_at": created, "finished_at": finished, "mapping_counts": mappingCounts})
		}
	}
	writeJSON(w, 200, out)
}

func (s *Server) materialSourceDiff(w http.ResponseWriter, r *http.Request) {
	uid := auth.SessionFromContext(r.Context()).UserID
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	material, err := scanMaterial(s.Store.DB.QueryRowContext(r.Context(), `SELECT id,user_id,source_type,source_id,title,description,images_json,category_json,skus_json,postage_mode,postage_cent,status,created_at,updated_at,image_property_name,video_enabled,videos_json,parent_material_id,split_batch_id,split_group_name,is_split_source,publish_parameters_json,revision FROM product_materials WHERE id=? AND user_id=? AND deleted_at IS NULL`, id, uid))
	if err != nil || len(materialSourceGoodsIDs(material)) == 0 {
		writeErr(w, 404, "拼多多来源素材不存在")
		return
	}
	s.backfillMaterialSourcePrices(r.Context(), material)
	current := map[string]map[string]any{}
	for _, raw := range material["skus"].([]any) {
		row, _ := raw.(map[string]any)
		goodsID := materialText(row["source_goods_id"])
		if goodsID == "" {
			goodsID = materialText(material["source_id"])
		}
		current[materialSourceKey(goodsID, materialText(row["source_sku_id"]))] = row
	}
	added, changed, seen := []any{}, []any{}, map[string]bool{}
	for _, goodsID := range materialSourceGoodsIDs(material) {
		rows, queryErr := s.Store.DB.QueryContext(r.Context(), `SELECT sku_id,specs_json,thumb_url,prices_json,price_cent,stock,is_onsale,last_collected_at FROM pdd_skus WHERE goods_id=? ORDER BY id`, goodsID)
		if queryErr != nil {
			writeErr(w, 500, "读取来源 SKU 失败")
			return
		}
		for rows.Next() {
			var skuID, specs, image, pricesRaw string
			var price, stock, collectedAt int64
			var onSale int
			_ = rows.Scan(&skuID, &specs, &image, &pricesRaw, &price, &stock, &onSale, &collectedAt)
			key := materialSourceKey(goodsID, skuID)
			seen[key] = true
			old := current[key]
			entry := map[string]any{"source_goods_id": goodsID, "source_sku_id": skuID, "source_properties": jsonValue(specs, []any{}), "source_image_url": image, "source_price_cent": price, "source_normal_price_cent": pddNormalPriceCent(pricesRaw), "source_price_updated_at": collectedAt, "quantity": stock, "enabled": onSale != 0}
			if old == nil {
				added = append(added, entry)
			} else if jsonInt64(old["source_price_cent"]) != price || jsonInt64(old["source_normal_price_cent"]) != pddNormalPriceCent(pricesRaw) || jsonInt64(old["quantity"]) != stock || fmt.Sprint(old["source_image_url"]) != image {
				changed = append(changed, entry)
			}
		}
		_ = rows.Close()
	}
	removed := []string{}
	for sourceKey, row := range current {
		if materialText(row["source_sku_id"]) != "" && !seen[sourceKey] {
			removed = append(removed, sourceKey)
		}
	}
	writeJSON(w, 200, map[string]any{"added": added, "changed": changed, "removed": removed})
}

type syncMaterialSourceInput struct {
	Prices         bool `json:"prices"`
	Stock          bool `json:"stock"`
	Images         bool `json:"images"`
	AddNew         bool `json:"add_new"`
	DisableRemoved bool `json:"disable_removed"`
}

func (s *Server) syncMaterialSource(w http.ResponseWriter, r *http.Request) {
	uid := auth.SessionFromContext(r.Context()).UserID
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var input syncMaterialSourceInput
	if decodeJSON(r, &input) != nil {
		writeErr(w, 400, "同步选项无效")
		return
	}
	material, err := scanMaterial(s.Store.DB.QueryRowContext(r.Context(), `SELECT id,user_id,source_type,source_id,title,description,images_json,category_json,skus_json,postage_mode,postage_cent,status,created_at,updated_at,image_property_name,video_enabled,videos_json,parent_material_id,split_batch_id,split_group_name,is_split_source,publish_parameters_json,revision FROM product_materials WHERE id=? AND user_id=? AND deleted_at IS NULL`, id, uid))
	if err != nil || len(materialSourceGoodsIDs(material)) == 0 {
		writeErr(w, 404, "拼多多来源素材不存在")
		return
	}
	s.backfillMaterialSourcePrices(r.Context(), material)
	var skus []materialSKU
	raw, _ := json.Marshal(material["skus"])
	_ = json.Unmarshal(raw, &skus)
	bySource := map[string]*materialSKU{}
	for i := range skus {
		if skus[i].SourceGoodsID == "" && skus[i].SourceSKUID != "" {
			skus[i].SourceGoodsID = materialText(material["source_id"])
		}
		bySource[materialSourceKey(skus[i].SourceGoodsID, skus[i].SourceSKUID)] = &skus[i]
	}
	seen := map[string]bool{}
	for _, goodsID := range materialSourceGoodsIDs(material) {
		rows, queryErr := s.Store.DB.QueryContext(r.Context(), `SELECT sku_id,specs_json,thumb_url,prices_json,price_cent,stock,is_onsale,last_collected_at FROM pdd_skus WHERE goods_id=? ORDER BY id`, goodsID)
		if queryErr != nil {
			writeErr(w, 500, "读取来源 SKU 失败")
			return
		}
		for rows.Next() {
			var skuID, specsRaw, image, pricesRaw string
			var price, stock, collectedAt int64
			var onSale int
			_ = rows.Scan(&skuID, &specsRaw, &image, &pricesRaw, &price, &stock, &onSale, &collectedAt)
			key := materialSourceKey(goodsID, skuID)
			seen[key] = true
			var specs []pddSpecInput
			_ = json.Unmarshal([]byte(specsRaw), &specs)
			props := []materialProperty{}
			for _, p := range specs {
				props = append(props, materialProperty{Name: p.SpecKey, Value: p.RawValue})
			}
			sku := bySource[key]
			if sku == nil && input.AddNew {
				skus = append(skus, materialSKU{MaterialSKUID: uuid.NewString(), SKUType: materialSKUTypeSource, SourceGoodsID: goodsID, SourceSKUID: skuID, SourceProperties: props, SourceImageURL: image, SourcePriceCents: price, SourceNormalPriceCents: pddNormalPriceCent(pricesRaw), SourcePriceUpdatedAt: collectedAt, SourcePriceOrigin: "synced", Properties: append([]materialProperty(nil), props...), PriceCents: price, Quantity: stock, Enabled: onSale != 0})
				continue
			}
			if sku == nil {
				continue
			}
			sku.SourceProperties = props
			sku.SourceImageURL = image
			if input.Prices {
				sku.SourcePriceCents = price
				sku.SourceNormalPriceCents = pddNormalPriceCent(pricesRaw)
				sku.SourcePriceUpdatedAt = collectedAt
				sku.SourcePriceOrigin = "synced"
			}
			if input.Stock {
				sku.Quantity = stock
			}
			if input.Images {
				sku.ImageURL = image
			}
		}
		_ = rows.Close()
	}
	if input.DisableRemoved {
		for i := range skus {
			if skus[i].SourceSKUID != "" && !seen[materialSourceKey(skus[i].SourceGoodsID, skus[i].SourceSKUID)] {
				skus[i].Enabled = false
			}
		}
	}
	encoded, _ := json.Marshal(skus)
	_, err = s.Store.DB.ExecContext(r.Context(), `UPDATE product_materials SET skus_json=?,updated_at=? WHERE id=? AND user_id=?`, string(encoded), time.Now().Unix(), id, uid)
	if err != nil {
		writeErr(w, 500, "同步素材失败")
		return
	}
	writeJSON(w, 200, map[string]any{"success": true})
}
