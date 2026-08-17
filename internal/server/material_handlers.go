package server

import (
	"bytes"
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
)

type materialSKU struct {
	MaterialSKUID    string             `json:"material_sku_id"`
	SourceGoodsID    string             `json:"source_goods_id,omitempty"`
	SourceSKUID      string             `json:"source_sku_id,omitempty"`
	SourceProperties []materialProperty `json:"source_properties,omitempty"`
	SourceImageURL   string             `json:"source_image_url,omitempty"`
	PriceCents       int64              `json:"price_cent"`
	Quantity         int64              `json:"quantity"`
	Enabled          bool               `json:"enabled"`
	Properties       []materialProperty `json:"properties"`
	ImageURL         string             `json:"image_url,omitempty"`
}
type materialProperty struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	ImageURL string `json:"image_url,omitempty"`
}
type materialInput struct {
	Title             string         `json:"title"`
	Description       string         `json:"description"`
	Images            []string       `json:"images"`
	Category          map[string]any `json:"category"`
	SKUs              []materialSKU  `json:"skus"`
	PostageMode       string         `json:"postage_mode"`
	PostageCents      int64          `json:"postage_cent"`
	ImagePropertyName string         `json:"image_property_name"`
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
}

type publishMaterialInput struct {
	CookieID string `json:"cookie_id"`
}

func normalizePublishedMaterialSKU(sourceType string, row map[string]any) {
	if sourceType == "pdd" && strings.TrimSpace(fmt.Sprint(row["source_sku_id"])) == "" {
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
	material, err := scanMaterial(s.Store.DB.QueryRowContext(r.Context(), `SELECT id,user_id,source_type,source_id,title,description,images_json,category_json,skus_json,postage_mode,postage_cent,status,created_at,updated_at,image_property_name FROM product_materials WHERE id=? AND user_id=? AND deleted_at IS NULL`, id, uid))
	if err != nil {
		writeErr(w, http.StatusNotFound, "素材不存在")
		return
	}
	images := stringSlice(material["images"])
	if len(images) == 0 || len(images) > 9 {
		writeErr(w, http.StatusBadRequest, "素材图片必须为 1 到 9 张")
		return
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fields := map[string]string{
		"cookie_id": input.CookieID, "title": fmt.Sprint(material["title"]),
		"description": fmt.Sprint(material["description"]), "postage_mode": fmt.Sprint(material["postage_mode"]),
		"postage": fmt.Sprintf("%.2f", float64(material["postage_cent"].(int64))/100),
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
	fields["original_price"] = fields["price"]
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
	if len(in.SKUs) == 0 || len(in.SKUs) > 200 {
		return errors.New("素材必须包含 1 到 200 个 SKU")
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

func materialMap(id, userID int64, sourceType, sourceID, title, description, images, category, skus, postageMode, status, imagePropertyName string, postage, created, updated int64) map[string]any {
	material := map[string]any{"id": id, "user_id": userID, "source_type": sourceType, "source_id": sourceID, "title": title, "description": description, "images": jsonValue(images, []string{}), "category": jsonValue(category, map[string]any{}), "skus": jsonValue(skus, []any{}), "postage_mode": postageMode, "postage_cent": postage, "status": status, "image_property_name": imagePropertyName, "created_at": created, "updated_at": updated}
	material["source_ids"] = materialSourceGoodsIDs(material)
	return material
}
func scanMaterial(row interface{ Scan(...any) error }) (map[string]any, error) {
	var id, userID, postage, created, updated int64
	var sourceType, sourceID, title, description, images, category, skus, postageMode, status, imagePropertyName string
	err := row.Scan(&id, &userID, &sourceType, &sourceID, &title, &description, &images, &category, &skus, &postageMode, &postage, &status, &created, &updated, &imagePropertyName)
	return materialMap(id, userID, sourceType, sourceID, title, description, images, category, skus, postageMode, status, imagePropertyName, postage, created, updated), err
}

func (s *Server) listMaterials(w http.ResponseWriter, r *http.Request) {
	uid := auth.SessionFromContext(r.Context()).UserID
	query := `SELECT id,user_id,source_type,source_id,title,description,images_json,category_json,skus_json,postage_mode,postage_cent,status,created_at,updated_at,image_property_name FROM product_materials WHERE user_id=? AND deleted_at IS NULL`
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
	writeJSON(w, 200, out)
}
func (s *Server) getMaterial(w http.ResponseWriter, r *http.Request) {
	uid := auth.SessionFromContext(r.Context()).UserID
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	m, err := scanMaterial(s.Store.DB.QueryRowContext(r.Context(), `SELECT id,user_id,source_type,source_id,title,description,images_json,category_json,skus_json,postage_mode,postage_cent,status,created_at,updated_at,image_property_name FROM product_materials WHERE id=? AND user_id=? AND deleted_at IS NULL`, id, uid))
	if err != nil {
		writeErr(w, 404, "素材不存在")
		return
	}
	writeJSON(w, 200, m)
}
func (s *Server) createMaterial(w http.ResponseWriter, r *http.Request) {
	var in materialInput
	if decodeJSON(r, &in) != nil || validateMaterial(&in) != nil {
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
	now := time.Now().Unix()
	res, err := s.Store.DB.ExecContext(r.Context(), `INSERT INTO product_materials(user_id,source_type,source_id,title,description,images_json,category_json,skus_json,postage_mode,postage_cent,status,created_at,updated_at,image_property_name) VALUES(?,?,?,?,?,?,?,?,?,?,'draft',?,?,?)`, uid, sourceType, sourceID, in.Title, in.Description, string(images), string(cat), string(skus), in.PostageMode, in.PostageCents, now, now, in.ImagePropertyName)
	if err != nil {
		writeErr(w, 500, "创建素材失败")
		return
	}
	id, _ := res.LastInsertId()
	writeJSON(w, 201, map[string]any{"success": true, "id": id})
}
func (s *Server) createMaterialFromPDD(w http.ResponseWriter, r *http.Request) {
	goodsID := chi.URLParam(r, "goodsID")
	var title, images string
	var productID int64
	if err := s.Store.DB.QueryRowContext(r.Context(), `SELECT id,title,images_json FROM pdd_products WHERE goods_id=?`, goodsID).Scan(&productID, &title, &images); err != nil {
		writeErr(w, 404, "采集商品不存在")
		return
	}
	rows, err := s.Store.DB.QueryContext(r.Context(), `SELECT sku_id,specs_json,thumb_url,price_cent,stock,is_onsale FROM pdd_skus WHERE product_id=? ORDER BY id`, productID)
	if err != nil {
		writeErr(w, 500, "读取采集 SKU 失败")
		return
	}
	defer rows.Close()
	skus := []materialSKU{}
	for rows.Next() {
		var skuID, specRaw, thumb string
		var price, stock int64
		var enabled int
		_ = rows.Scan(&skuID, &specRaw, &thumb, &price, &stock, &enabled)
		var specs []pddSpecInput
		_ = json.Unmarshal([]byte(specRaw), &specs)
		props := []materialProperty{}
		for _, p := range specs {
			props = append(props, materialProperty{Name: p.SpecKey, Value: p.RawValue})
		}
		skus = append(skus, materialSKU{MaterialSKUID: uuid.NewString(), SourceGoodsID: goodsID, SourceSKUID: skuID, SourceProperties: append([]materialProperty(nil), props...), SourceImageURL: thumb, PriceCents: price, Quantity: stock, Enabled: enabled != 0, Properties: props, ImageURL: thumb})
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
	in := materialInput{Title: title, Description: title, Images: cleanImages, Category: map[string]any{}, SKUs: skus, PostageMode: "free"}
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
	var primarySourceID string
	_ = s.Store.DB.QueryRowContext(r.Context(), `SELECT source_id FROM product_materials WHERE id=? AND user_id=? AND deleted_at IS NULL`, id, uid).Scan(&primarySourceID)
	for index := range in.SKUs {
		if in.SKUs[index].SourceGoodsID == "" && in.SKUs[index].SourceSKUID != "" {
			in.SKUs[index].SourceGoodsID = primarySourceID
		}
	}
	if err := validateMaterial(&in); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	images, _ := json.Marshal(in.Images)
	cat, _ := json.Marshal(in.Category)
	skus, _ := json.Marshal(in.SKUs)
	res, err := s.Store.DB.ExecContext(r.Context(), `UPDATE product_materials SET title=?,description=?,images_json=?,category_json=?,skus_json=?,postage_mode=?,postage_cent=?,image_property_name=?,updated_at=? WHERE id=? AND user_id=? AND deleted_at IS NULL`, in.Title, in.Description, string(images), string(cat), string(skus), in.PostageMode, in.PostageCents, in.ImagePropertyName, time.Now().Unix(), id, uid)
	if err != nil {
		writeErr(w, 500, "更新素材失败")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeErr(w, 404, "素材不存在")
		return
	}
	writeJSON(w, 200, map[string]any{"success": true})
}
func (s *Server) deleteMaterial(w http.ResponseWriter, r *http.Request) {
	uid := auth.SessionFromContext(r.Context()).UserID
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	res, err := s.Store.DB.ExecContext(r.Context(), `UPDATE product_materials SET deleted_at=?,updated_at=? WHERE id=? AND user_id=? AND deleted_at IS NULL`, time.Now().Unix(), time.Now().Unix(), id, uid)
	if err != nil {
		writeErr(w, 500, "删除素材失败")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeErr(w, 404, "素材不存在")
		return
	}
	writeJSON(w, 200, map[string]any{"success": true})
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
	material, err := scanMaterial(s.Store.DB.QueryRowContext(r.Context(), `SELECT id,user_id,source_type,source_id,title,description,images_json,category_json,skus_json,postage_mode,postage_cent,status,created_at,updated_at,image_property_name FROM product_materials WHERE id=? AND user_id=? AND deleted_at IS NULL`, id, uid))
	if err != nil || len(materialSourceGoodsIDs(material)) == 0 {
		writeErr(w, 404, "拼多多来源素材不存在")
		return
	}
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
		rows, queryErr := s.Store.DB.QueryContext(r.Context(), `SELECT sku_id,specs_json,thumb_url,price_cent,stock,is_onsale FROM pdd_skus WHERE goods_id=? ORDER BY id`, goodsID)
		if queryErr != nil {
			writeErr(w, 500, "读取来源 SKU 失败")
			return
		}
		for rows.Next() {
			var skuID, specs, image string
			var price, stock int64
			var onSale int
			_ = rows.Scan(&skuID, &specs, &image, &price, &stock, &onSale)
			key := materialSourceKey(goodsID, skuID)
			seen[key] = true
			old := current[key]
			entry := map[string]any{"source_goods_id": goodsID, "source_sku_id": skuID, "source_properties": jsonValue(specs, []any{}), "source_image_url": image, "price_cent": price, "quantity": stock, "enabled": onSale != 0}
			if old == nil {
				added = append(added, entry)
			} else if jsonInt64(old["price_cent"]) != price || jsonInt64(old["quantity"]) != stock || fmt.Sprint(old["source_image_url"]) != image {
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
	material, err := scanMaterial(s.Store.DB.QueryRowContext(r.Context(), `SELECT id,user_id,source_type,source_id,title,description,images_json,category_json,skus_json,postage_mode,postage_cent,status,created_at,updated_at,image_property_name FROM product_materials WHERE id=? AND user_id=? AND deleted_at IS NULL`, id, uid))
	if err != nil || len(materialSourceGoodsIDs(material)) == 0 {
		writeErr(w, 404, "拼多多来源素材不存在")
		return
	}
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
		rows, queryErr := s.Store.DB.QueryContext(r.Context(), `SELECT sku_id,specs_json,thumb_url,price_cent,stock,is_onsale FROM pdd_skus WHERE goods_id=? ORDER BY id`, goodsID)
		if queryErr != nil {
			writeErr(w, 500, "读取来源 SKU 失败")
			return
		}
		for rows.Next() {
			var skuID, specsRaw, image string
			var price, stock int64
			var onSale int
			_ = rows.Scan(&skuID, &specsRaw, &image, &price, &stock, &onSale)
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
				skus = append(skus, materialSKU{MaterialSKUID: uuid.NewString(), SourceGoodsID: goodsID, SourceSKUID: skuID, SourceProperties: props, SourceImageURL: image, Properties: append([]materialProperty(nil), props...), PriceCents: price, Quantity: stock, Enabled: onSale != 0})
				continue
			}
			if sku == nil {
				continue
			}
			sku.SourceProperties = props
			sku.SourceImageURL = image
			if input.Prices {
				sku.PriceCents = price
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
