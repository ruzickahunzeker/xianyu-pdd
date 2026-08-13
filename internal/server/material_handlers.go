package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"xianyu-go/internal/auth"
)

type materialSKU struct {
	SourceSKUID string             `json:"source_sku_id,omitempty"`
	PriceCents  int64              `json:"price_cent"`
	Quantity    int64              `json:"quantity"`
	Enabled     bool               `json:"enabled"`
	Properties  []materialProperty `json:"properties"`
	ImageURL    string             `json:"image_url,omitempty"`
}
type materialProperty struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	ImageURL string `json:"image_url,omitempty"`
}
type materialInput struct {
	Title        string         `json:"title"`
	Description  string         `json:"description"`
	Images       []string       `json:"images"`
	Category     map[string]any `json:"category"`
	SKUs         []materialSKU  `json:"skus"`
	PostageMode  string         `json:"postage_mode"`
	PostageCents int64          `json:"postage_cent"`
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
}

type publishMaterialInput struct {
	CookieID string `json:"cookie_id"`
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
	material, err := scanMaterial(s.Store.DB.QueryRowContext(r.Context(), `SELECT id,user_id,source_type,source_id,title,description,images_json,category_json,skus_json,postage_mode,postage_cent,status,created_at,updated_at FROM product_materials WHERE id=? AND user_id=? AND deleted_at IS NULL`, id, uid))
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
				source := strings.TrimSpace(fmt.Sprint(property["image_url"]))
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
	s.publishItem(w, proxy)
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

func materialMap(id, userID int64, sourceType, sourceID, title, description, images, category, skus, postageMode, status string, postage, created, updated int64) map[string]any {
	return map[string]any{"id": id, "user_id": userID, "source_type": sourceType, "source_id": sourceID, "title": title, "description": description, "images": jsonValue(images, []string{}), "category": jsonValue(category, map[string]any{}), "skus": jsonValue(skus, []any{}), "postage_mode": postageMode, "postage_cent": postage, "status": status, "created_at": created, "updated_at": updated}
}
func scanMaterial(row interface{ Scan(...any) error }) (map[string]any, error) {
	var id, userID, postage, created, updated int64
	var sourceType, sourceID, title, description, images, category, skus, postageMode, status string
	err := row.Scan(&id, &userID, &sourceType, &sourceID, &title, &description, &images, &category, &skus, &postageMode, &postage, &status, &created, &updated)
	return materialMap(id, userID, sourceType, sourceID, title, description, images, category, skus, postageMode, status, postage, created, updated), err
}

func (s *Server) listMaterials(w http.ResponseWriter, r *http.Request) {
	uid := auth.SessionFromContext(r.Context()).UserID
	rows, err := s.Store.DB.QueryContext(r.Context(), `SELECT id,user_id,source_type,source_id,title,description,images_json,category_json,skus_json,postage_mode,postage_cent,status,created_at,updated_at FROM product_materials WHERE user_id=? AND deleted_at IS NULL ORDER BY updated_at DESC`, uid)
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
	m, err := scanMaterial(s.Store.DB.QueryRowContext(r.Context(), `SELECT id,user_id,source_type,source_id,title,description,images_json,category_json,skus_json,postage_mode,postage_cent,status,created_at,updated_at FROM product_materials WHERE id=? AND user_id=? AND deleted_at IS NULL`, id, uid))
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
	res, err := s.Store.DB.ExecContext(r.Context(), `INSERT INTO product_materials(user_id,source_type,source_id,title,description,images_json,category_json,skus_json,postage_mode,postage_cent,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,'draft',?,?)`, uid, sourceType, sourceID, in.Title, in.Description, string(images), string(cat), string(skus), in.PostageMode, in.PostageCents, now, now)
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
			props = append(props, materialProperty{Name: p.SpecKey, Value: p.RawValue, ImageURL: thumb})
		}
		skus = append(skus, materialSKU{SourceSKUID: skuID, PriceCents: price, Quantity: stock, Enabled: enabled != 0, Properties: props, ImageURL: thumb})
	}
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
	if err := validateMaterial(&in); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	images, _ := json.Marshal(in.Images)
	cat, _ := json.Marshal(in.Category)
	skus, _ := json.Marshal(in.SKUs)
	res, err := s.Store.DB.ExecContext(r.Context(), `UPDATE product_materials SET title=?,description=?,images_json=?,category_json=?,skus_json=?,postage_mode=?,postage_cent=?,updated_at=? WHERE id=? AND user_id=? AND deleted_at IS NULL`, in.Title, in.Description, string(images), string(cat), string(skus), in.PostageMode, in.PostageCents, time.Now().Unix(), id, uid)
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
