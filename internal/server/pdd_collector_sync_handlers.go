package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const maxPDDRemoteSyncResponse = 1 << 20

type pddRemoteSyncInput struct {
	TargetURL   string `json:"target_url"`
	DeviceToken string `json:"device_token"`
}

func normalizePDDRemoteTargetPath(raw, path string) (string, error) {
	target, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || target.Hostname() == "" || (target.Scheme != "http" && target.Scheme != "https") {
		return "", errors.New("目标服务器地址必须是有效的 HTTP 或 HTTPS 地址")
	}
	if target.User != nil || target.RawQuery != "" || target.Fragment != "" {
		return "", errors.New("目标服务器地址不能包含账号、查询参数或片段")
	}
	if target.Path != "" && target.Path != "/" {
		return "", errors.New("目标服务器地址只填写站点根地址，例如 http://10.10.1.10:59188")
	}
	target.Path = path
	return target.String(), nil
}

func normalizePDDRemoteTarget(raw string) (string, error) {
	return normalizePDDRemoteTargetPath(raw, "/api/pdd-collector/products")
}

func pddRemoteClient() *http.Client {
	return &http.Client{Timeout: 45 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
}

func (s *Server) pddTestRemoteCollector(w http.ResponseWriter, r *http.Request) {
	var input pddRemoteSyncInput
	if decodeJSON(r, &input) != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	target, err := normalizePDDRemoteTargetPath(input.TargetURL, "/api/pdd-collector/device")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(input.DeviceToken) == "" {
		writeErr(w, http.StatusBadRequest, "请输入目标服务器的采集设备 Token")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(input.DeviceToken))
	response, err := pddRemoteClient().Do(req)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "连接目标服务器失败: "+err.Error())
		return
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		writeErr(w, http.StatusBadGateway, fmt.Sprintf("目标服务器验证失败（HTTP %d），请检查地址和 Token", response.StatusCode))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "target_url": input.TargetURL})
}

func (s *Server) collectedProductPayload(ctx context.Context, goodsID string) (pddCollectionInput, error) {
	var in pddCollectionInput
	var imagesJSON, videosJSON, propertiesJSON string
	var collectedAt int64
	if err := s.Store.DB.QueryRowContext(ctx, `SELECT goods_id,mall_sn,final_url,title,images_json,videos_json,properties_json,last_collected_at FROM pdd_products WHERE goods_id=?`, goodsID).Scan(&in.Goods.GoodsID, &in.Goods.MallSN, &in.FinalURL, &in.Goods.Title, &imagesJSON, &videosJSON, &propertiesJSON, &collectedAt); err != nil {
		return in, err
	}
	if err := json.Unmarshal([]byte(imagesJSON), &in.Goods.Images); err != nil {
		return in, fmt.Errorf("解析商品图片失败: %w", err)
	}
	if err := json.Unmarshal([]byte(videosJSON), &in.Goods.Videos); err != nil {
		return in, fmt.Errorf("解析商品视频失败: %w", err)
	}
	if err := json.Unmarshal([]byte(propertiesJSON), &in.Goods.GoodsProperty); err != nil {
		return in, fmt.Errorf("解析商品属性失败: %w", err)
	}
	rows, err := s.Store.DB.QueryContext(ctx, `SELECT sku_id,specs_json,spec_value_ids_json,thumb_url,prices_json,stock,is_onsale,raw_snapshot_json FROM pdd_skus WHERE goods_id=? ORDER BY id`, goodsID)
	if err != nil {
		return in, err
	}
	defer rows.Close()
	for rows.Next() {
		var sku pddSKUInput
		var skuID, thumbURL, specsJSON, idsJSON, pricesJSON, rawJSON string
		var stock int64
		var onSale int
		if err := rows.Scan(&skuID, &specsJSON, &idsJSON, &thumbURL, &pricesJSON, &stock, &onSale, &rawJSON); err != nil {
			return in, err
		}
		_ = json.Unmarshal([]byte(rawJSON), &sku)
		sku.SKUID, sku.GoodsID, sku.ThumbURL, sku.Stock, sku.IsOnsale = strings.TrimSpace(skuID), goodsID, strings.TrimSpace(thumbURL), stock, onSale != 0
		if err := json.Unmarshal([]byte(specsJSON), &sku.Specs); err != nil {
			return in, fmt.Errorf("解析 SKU %s 规格失败: %w", sku.SKUID, err)
		}
		if err := json.Unmarshal([]byte(idsJSON), &sku.SpecValueIDs); err != nil {
			return in, fmt.Errorf("解析 SKU %s 规格 ID 失败: %w", sku.SKUID, err)
		}
		if err := json.Unmarshal([]byte(pricesJSON), &sku.Prices); err != nil {
			return in, fmt.Errorf("解析 SKU %s 价格失败: %w", sku.SKUID, err)
		}
		in.SKUs = append(in.SKUs, sku)
	}
	if err := rows.Err(); err != nil {
		return in, err
	}
	in.SchemaVersion = 1
	in.CollectionID = uuid.NewString()
	in.CollectionMethod = "server_sync"
	in.CollectedAt = time.Unix(collectedAt, 0).UTC().Format(time.RFC3339)
	return in, nil
}

func (s *Server) pddSyncProductToRemote(w http.ResponseWriter, r *http.Request) {
	goodsID := strings.TrimSpace(chi.URLParam(r, "goodsID"))
	if !pddNumericID.MatchString(goodsID) {
		writeErr(w, http.StatusBadRequest, "goods_id 无效")
		return
	}
	var input pddRemoteSyncInput
	if decodeJSON(r, &input) != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	target, err := normalizePDDRemoteTarget(input.TargetURL)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(input.DeviceToken) == "" {
		writeErr(w, http.StatusBadRequest, "请输入目标服务器的采集设备 Token")
		return
	}
	payload, err := s.collectedProductPayload(r.Context(), goodsID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "读取采集商品失败")
		return
	}
	body, _ := json.Marshal(payload)
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, target, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(input.DeviceToken))
	response, err := pddRemoteClient().Do(req)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "连接目标服务器失败: "+err.Error())
		return
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxPDDRemoteSyncResponse+1))
	if err != nil || len(data) > maxPDDRemoteSyncResponse {
		writeErr(w, http.StatusBadGateway, "目标服务器响应无效或过大")
		return
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message := strings.TrimSpace(string(data))
		if len(message) > 500 {
			message = message[:500]
		}
		writeErr(w, http.StatusBadGateway, fmt.Sprintf("目标服务器返回 HTTP %d: %s", response.StatusCode, message))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "goods_id": goodsID, "sku_count": len(payload.SKUs), "target_url": input.TargetURL})
}
