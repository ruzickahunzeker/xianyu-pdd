package pddaddress

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type UpdateRequest struct {
	Name   string
	Mobile string
	Match  Match
}

type UpdateResult struct {
	Status       string
	HTTPStatus   int
	ResponseBody string
}

type Updater interface {
	Update(context.Context, UpdateRequest) (UpdateResult, error)
}

type Config struct{ BaseURL, PDDUID, AddressID, Cookie, UserAgent string }

func ConfigFromEnv() (Config, error) {
	c := Config{
		BaseURL:   strings.TrimSpace(os.Getenv("PDD_ADDRESS_BASE_URL")),
		AddressID: strings.TrimSpace(os.Getenv("PDD_ADDRESS_ID")), Cookie: strings.TrimSpace(os.Getenv("PDD_ADDRESS_COOKIE")),
		UserAgent: strings.TrimSpace(os.Getenv("PDD_ADDRESS_USER_AGENT")),
	}
	if c.BaseURL == "" {
		c.BaseURL = "https://mobile.pinduoduo.com"
	}
	if c.UserAgent == "" {
		c.UserAgent = "Mozilla/5.0"
	}
	c.PDDUID = PDDUIDFromCookie(c.Cookie)
	if c.PDDUID == "" || c.AddressID == "" || c.Cookie == "" {
		return Config{}, errors.New("拼多多地址配置不完整")
	}
	if _, err := url.ParseRequestURI(c.BaseURL); err != nil {
		return Config{}, errors.New("拼多多地址服务地址无效")
	}
	return c, nil
}

type Client struct {
	Config     Config
	HTTPClient *http.Client
}

func NewClient(config Config) *Client {
	return &Client{Config: config, HTTPClient: &http.Client{Timeout: 20 * time.Second}}
}

func (c *Client) Update(ctx context.Context, in UpdateRequest) (UpdateResult, error) {
	endpoint := strings.TrimRight(c.Config.BaseURL, "/") + "/proxy/api/api/origenes/address_info/" + url.PathEscape(c.Config.AddressID)
	u, err := url.Parse(endpoint)
	if err != nil {
		return UpdateResult{}, errors.New("拼多多地址服务地址无效")
	}
	q := u.Query()
	q.Set("pdduid", c.Config.PDDUID)
	u.RawQuery = q.Encode()
	payload := map[string]any{
		"name": in.Name, "mobile": in.Mobile, "province_id": in.Match.ProvinceID, "city_id": in.Match.CityID,
		"district_id": in.Match.DistrictID, "address": in.Match.Address, "community_address_request": map[string]any{},
		"is_default": "0", "check_region": true, "address_id": c.Config.AddressID,
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return UpdateResult{}, errors.New("创建拼多多地址请求失败")
	}
	req.Header.Set("Content-Type", "application/json;charset=UTF-8")
	req.Header.Set("User-Agent", c.Config.UserAgent)
	req.Header.Set("Origin", "https://mobile.pinduoduo.com")
	req.Header.Set("Referer", "https://mobile.pinduoduo.com/")
	req.Header.Set("Cookie", c.Config.Cookie)
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return UpdateResult{}, errors.New("调用拼多多地址接口失败")
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
	if readErr != nil {
		return UpdateResult{}, errors.New("读取拼多多地址响应失败")
	}
	result := UpdateResult{HTTPStatus: resp.StatusCode, ResponseBody: string(raw), Status: classifyResponse(resp.StatusCode, raw)}
	if result.Status == "failed" {
		return result, fmt.Errorf("拼多多地址修改失败（HTTP %d）", resp.StatusCode)
	}
	return result, nil
}

func classifyResponse(status int, raw []byte) string {
	if status < 200 || status >= 300 {
		return "failed"
	}
	var value map[string]any
	if json.Unmarshal(raw, &value) != nil {
		return "result_unknown"
	}
	if success, ok := value["success"].(bool); ok {
		if success {
			return "applied"
		}
		return "failed"
	}
	if code, ok := numberValue(value["error_code"]); ok {
		if code == 0 {
			return "applied"
		}
		return "failed"
	}
	if code, ok := numberValue(value["code"]); ok && code == 0 {
		return "applied"
	}
	if result, ok := value["result"].(bool); ok && result {
		return "applied"
	}
	return "result_unknown"
}

func numberValue(v any) (float64, bool) { n, ok := v.(float64); return n, ok }

func PDDUIDFromCookie(cookie string) string {
	for _, part := range strings.Split(cookie, ";") {
		name, value, found := strings.Cut(strings.TrimSpace(part), "=")
		if found && strings.EqualFold(strings.TrimSpace(name), "pdd_user_id") {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
