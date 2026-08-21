package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mxschmitt/playwright-go"

	"xianyu-go/internal/db"
	"xianyu-go/internal/pddcheckout"
	"xianyu-go/internal/pddproduct"
	"xianyu-go/internal/pddshipping"
	"xianyu-go/internal/pddsite"
)

type task struct {
	ID, OrderID, LeaseToken, PDDAccountID, GoodsID, SKUID string
	ReceiverName, Province, City, District, DetailAddress string
	Quantity                                              int
	XianyuAmountCent                                      int64
	StartedAt                                             int64
	BeforeOrderSNs                                        []string
	RecoveryOnly                                          bool
}

type messageTask struct {
	ID, LeaseToken, PDDAccountID, GoodsID, SKUID, MallSN, CapturedChatURL string
	TaskType, Message, Action, PDDOrderID                                 string
}

type pddChatCapture struct {
	Endpoint string          `json:"endpoint"`
	Response json.RawMessage `json:"response"`
	MallID   string          `json:"mall_id"`
	At       int64           `json:"at"`
}

type pddChatCaptureState struct {
	Captures     []pddChatCapture `json:"captures"`
	TitanWakeups int64            `json:"titan_wakeups"`
}

type worker struct {
	baseURL, apiKey, screenshotDir string
	submit, once, logisticsOnly    bool
	client                         *http.Client
	database                       *sql.DB
	store                          *db.Store
	pw                             *playwright.Playwright
	browser                        playwright.Browser
	lastLogisticsSync              time.Time
	lastStateReport                time.Time
	lastReportedState              string
	lastReportedError              string
}

func main() {
	w := &worker{
		baseURL:       strings.TrimRight(env("FULFILLMENT_API_URL", "http://127.0.0.1:59188"), "/"),
		apiKey:        strings.TrimSpace(os.Getenv("FULFILLMENT_API_KEY")),
		screenshotDir: env("PDD_SCREENSHOT_DIR", "/data/screenshots"), submit: strings.EqualFold(os.Getenv("PDD_PURCHASE_SUBMIT"), "true"),
		once: strings.EqualFold(os.Getenv("PDD_WORKER_ONCE"), "true"), client: &http.Client{Timeout: 30 * time.Second},
		logisticsOnly: strings.EqualFold(os.Getenv("PDD_WORKER_LOGISTICS_ONLY"), "true"),
	}
	if w.apiKey == "" {
		log.Fatal("FULFILLMENT_API_KEY 必须配置")
	}
	if strings.TrimSpace(os.Getenv("XIANYU_DATA_KEY")) == "" {
		log.Fatal("XIANYU_DATA_KEY 必须配置，worker 才能解密设置中保存的拼多多 Cookie")
	}
	if err := w.openStore(); err != nil {
		log.Fatal(err)
	}
	readyCtx, readyCancel := context.WithTimeout(context.Background(), 90*time.Second)
	if err := w.waitForAPI(readyCtx); err != nil {
		readyCancel()
		log.Fatal(err)
	}
	readyCancel()
	if err := w.startBrowser(); err != nil {
		log.Fatal(err)
	}
	w.reportState("idle", "")
	defer w.close()
	defer w.reportState("stopped", "")
	if w.logisticsOnly {
		if err := w.syncLogisticsForce(); err != nil {
			log.Fatal(err)
		}
		return
	}
	for {
		purchaseErr := w.runOne()
		if purchaseErr != nil && !errors.Is(purchaseErr, errNoTask) {
			log.Printf("采购任务失败: %v", purchaseErr)
		}

		// Purchase and merchant-message queues are independent. Always give the
		// message queue a turn so a failed or long-lived purchase task cannot
		// leave confirmed messages waiting forever.
		messageErr := w.runMessageOne()
		if messageErr != nil && !errors.Is(messageErr, errNoTask) {
			log.Printf("商家消息任务失败: %v", messageErr)
		}

		var logisticsErr error
		if errors.Is(purchaseErr, errNoTask) && errors.Is(messageErr, errNoTask) {
			w.reportState("syncing_logistics", "")
			if logisticsErr = w.syncLogistics(); logisticsErr != nil {
				log.Printf("物流同步失败: %v", logisticsErr)
			}
		}
		lastError := ""
		for _, runErr := range []error{purchaseErr, messageErr, logisticsErr} {
			if runErr != nil && !errors.Is(runErr, errNoTask) {
				lastError = runErr.Error()
				break
			}
		}
		if lastError != "" {
			w.reportState("degraded", lastError)
		} else {
			w.reportState("idle", "")
		}
		if w.once {
			return
		}
		time.Sleep(5 * time.Second)
	}
}

func (w *worker) reportState(state, lastError string) {
	if state == w.lastReportedState && lastError == w.lastReportedError && time.Since(w.lastStateReport) < 15*time.Second {
		return
	}
	_ = w.store.Settings.SetMany(context.Background(), map[string]string{"pdd_worker_last_seen_at": strconv.FormatInt(time.Now().Unix(), 10), "pdd_worker_state": state, "pdd_worker_last_error": lastError})
	w.lastReportedState, w.lastReportedError, w.lastStateReport = state, lastError, time.Now()
}

func (w *worker) runMessageOne() error {
	t, err := w.claimMessage()
	if err != nil {
		return err
	}
	ctx, site, err := w.taskContext(task{PDDAccountID: t.PDDAccountID})
	if err != nil {
		return w.messageResult(t, "failed", err.Error(), nil)
	}
	defer ctx.Close()
	page, err := ctx.NewPage()
	if err != nil {
		return w.messageResult(t, "failed", err.Error(), nil)
	}
	defer page.Close()
	if err = w.openMerchantChat(page, site, t); err != nil {
		return w.messageResult(t, "failed", err.Error(), nil)
	}
	_ = w.messageHeartbeat(t)
	current, _ := url.Parse(page.URL())
	if current != nil && current.Query().Get("mall_sn") != "" && current.Query().Get("mall_sn") != t.MallSN {
		return w.messageResult(t, "failed", "merchant_mismatch", nil)
	}
	input := page.Locator("textarea#input-content, textarea.input-content").First()
	inputDeadline := time.Now().Add(20 * time.Second)
	networkRetries := 0
	for time.Now().Before(inputDeadline) {
		if count, _ := input.Count(); count == 1 {
			break
		}
		body, _ := page.Locator("body").InnerText()
		normalized := strings.ToLower(body)
		if strings.Contains(body, "网络异常") || strings.Contains(body, "请稍后重试") {
			if networkRetries < 2 {
				networkRetries++
				refresh := page.GetByText("刷新", playwright.PageGetByTextOptions{Exact: playwright.Bool(true)}).First()
				if count, _ := refresh.Count(); count == 1 {
					_ = refresh.Click()
				} else {
					_, _ = page.Reload(playwright.PageReloadOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded})
				}
				time.Sleep(2 * time.Second)
				continue
			}
			_ = w.screenshot(page, "message-"+t.ID+"-network-error.png")
			return w.messageResult(t, "failed", "network_error", map[string]any{"url": page.URL(), "retries": networkRetries})
		}
		if strings.Contains(normalized, "登录") && !strings.Contains(normalized, "发送") {
			_ = w.screenshot(page, "message-"+t.ID+"-login-required.png")
			return w.messageResult(t, "failed", "login_required", map[string]any{"url": page.URL()})
		}
		if strings.Contains(normalized, "验证码") || strings.Contains(normalized, "安全验证") {
			_ = w.screenshot(page, "message-"+t.ID+"-captcha-required.png")
			return w.messageResult(t, "failed", "captcha_required", map[string]any{"url": page.URL()})
		}
		time.Sleep(500 * time.Millisecond)
	}
	if count, _ := input.Count(); count != 1 {
		title, _ := page.Title()
		_ = w.screenshot(page, "message-"+t.ID+"-input-not-found.png")
		return w.messageResult(t, "failed", "input_not_found", map[string]any{"url": page.URL(), "title": title})
	}
	button := page.Locator("div.send-button").Filter(playwright.LocatorFilterOptions{HasText: "发送"}).First()
	if t.Action == "preflight" {
		// The send button is rendered only after text exists. A preflight must not
		// mutate the input, so input visibility is the safe readiness boundary.
		_ = w.screenshot(page, "message-"+t.ID+"-preflight.png")
		_, err = w.jsonRequest(http.MethodPost, "/api/pdd/messages/"+t.ID+"/preflight", map[string]any{"lease_token": t.LeaseToken}, nil)
		return err
	}
	if err = input.Fill(t.Message); err != nil {
		return w.messageResult(t, "failed", "填写消息失败: "+err.Error(), nil)
	}
	if actual, readErr := input.InputValue(); readErr != nil || actual != t.Message {
		return w.messageResult(t, "failed", "消息正文回读不一致", nil)
	}
	buttonDeadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(buttonDeadline) {
		if count, _ := button.Count(); count == 1 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if count, _ := button.Count(); count != 1 {
		_ = w.screenshot(page, "message-"+t.ID+"-send-button-not-found.png")
		return w.messageResult(t, "failed", "send_button_not_found", nil)
	}
	_ = w.screenshot(page, "message-"+t.ID+"-before-send.png")
	clicked := false
	if err = button.Click(); err != nil {
		return w.messageResult(t, "failed", "点击发送失败: "+err.Error(), nil)
	}
	clicked = true
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		value, _ := input.InputValue()
		pageBody, _ := page.Locator("body").InnerText()
		if value == "" && strings.Contains(pageBody, t.Message) {
			time.Sleep(500 * time.Millisecond)
			_ = w.flushPDDChatCapture(page, t.PDDAccountID, t.MallSN, t.GoodsID, t.PDDOrderID)
			_ = w.screenshot(page, "message-"+t.ID+"-verified.png")
			return w.messageResult(t, "verified", "", map[string]any{"url": page.URL(), "input_cleared": true, "message_visible": true})
		}
		time.Sleep(300 * time.Millisecond)
	}
	if clicked {
		_ = w.flushPDDChatCapture(page, t.PDDAccountID, t.MallSN, t.GoodsID, t.PDDOrderID)
		return w.messageResult(t, "result_unknown", "点击发送后无法确认消息是否出现", map[string]any{"url": page.URL(), "clicked": true})
	}
	return w.messageResult(t, "failed", "发送未执行", nil)
}

func (w *worker) openMerchantChat(page playwright.Page, site pddsite.Site, t messageTask) error {
	productURL := ""
	if strings.TrimSpace(t.PDDOrderID) == "" {
		if strings.TrimSpace(t.GoodsID) == "" {
			return errors.New("消息任务缺少拼多多订单号和 goods_id，无法进入商家聊天")
		}
		if err := w.database.QueryRow(`SELECT final_url FROM pdd_products WHERE goods_id=?`, t.GoodsID).Scan(&productURL); err != nil {
			return errors.New("数据库中没有该商品链接，无法进入商家聊天")
		}
	}
	entryURL, buttonText, pageName, err := merchantEntryForTask(site, t, productURL)
	if err != nil {
		return err
	}
	if _, err := page.Goto(entryURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(30000)}); err != nil {
		return fmt.Errorf("打开拼多多%s页面失败: %w", pageName, err)
	}
	if strings.Contains(page.URL(), "/login.html") {
		return errors.New("拼多多页面跳转登录，请更新对应站点 Cookie")
	}
	button := page.GetByText(buttonText, playwright.PageGetByTextOptions{Exact: playwright.Bool(true)}).First()
	if err := button.WaitFor(playwright.LocatorWaitForOptions{State: playwright.WaitForSelectorStateVisible, Timeout: playwright.Float(15000)}); err != nil {
		return fmt.Errorf("拼多多%s页面未找到“%s”按钮", pageName, buttonText)
	}
	if err := button.Click(); err != nil {
		return fmt.Errorf("点击“%s”失败: %w", buttonText, err)
	}
	page.WaitForTimeout(1500)
	return nil
}

func merchantEntryForTask(site pddsite.Site, t messageTask, productURL string) (entryURL, buttonText, pageName string, err error) {
	if strings.TrimSpace(t.PDDOrderID) != "" {
		return site.URL("/order.html", url.Values{"order_sn": {t.PDDOrderID}}), "联系商家", "订单", nil
	}
	if strings.TrimSpace(t.GoodsID) == "" {
		return "", "", "", errors.New("消息任务缺少拼多多订单号和 goods_id，无法进入商家聊天")
	}
	return pddproduct.ProductURLForSite(productURL, t.GoodsID, site), "客服", "商品", nil
}

const pddChatObserverScript = `(() => {
  if (window.__xianyuPDDChat) return;
  const state = {http: [], titanWakeups: 0, titanLastAt: 0};
  Object.defineProperty(window, '__xianyuPDDChat', {value: state, configurable: false});
  const endpoint = value => String(value || '').split('?')[0];
  const relevant = value => /\/rainbow\/(chat|conv)\//.test(value);
  const mallFromBody = body => {
    try {
      const value = typeof body === 'string' ? JSON.parse(body) : body;
      return String(value?.list?.with?.id || value?.message?.to?.uid || value?.mall_id || '');
    } catch (_) { return ''; }
  };
  const nativeFetch = window.fetch;
  window.fetch = async function(input, init) {
    const requestURL = typeof input === 'string' ? input : input?.url;
    const response = await nativeFetch.apply(this, arguments);
    if (relevant(String(requestURL || ''))) {
      try {
        const text = await response.clone().text();
        let payload = {};
        try { payload = JSON.parse(text); } catch (_) { payload = {raw_text: text.slice(0, 200000)}; }
        state.http.push({endpoint: endpoint(requestURL), response: payload, mall_id: mallFromBody(init?.body), at: Date.now()});
        if (state.http.length > 50) state.http.splice(0, state.http.length - 50);
      } catch (_) {}
    }
    return response;
  };
  const NativeWebSocket = window.WebSocket;
  function ObservedWebSocket(url, protocols) {
    const socket = protocols === undefined ? new NativeWebSocket(url) : new NativeWebSocket(url, protocols);
    if (String(url || '').includes('/proxy/ws/titan')) {
      socket.addEventListener('message', () => { state.titanWakeups++; state.titanLastAt = Date.now(); });
    }
    return socket;
  }
  ObservedWebSocket.prototype = NativeWebSocket.prototype;
  Object.setPrototypeOf(ObservedWebSocket, NativeWebSocket);
  for (const key of ['CONNECTING','OPEN','CLOSING','CLOSED']) ObservedWebSocket[key] = NativeWebSocket[key];
  window.WebSocket = ObservedWebSocket;
})();`

func installPDDChatObserver(ctx playwright.BrowserContext) error {
	return ctx.AddInitScript(playwright.Script{Content: playwright.String(pddChatObserverScript)})
}

func (w *worker) drainPDDChatCapture(page playwright.Page) (pddChatCaptureState, error) {
	value, err := page.Evaluate(`() => {
      const state = window.__xianyuPDDChat;
      if (!state) return {captures: [], titan_wakeups: 0};
      const captures = state.http.splice(0, state.http.length);
      const titanWakeups = state.titanWakeups;
      state.titanWakeups = 0;
      return {captures, titan_wakeups: titanWakeups};
    }`)
	if err != nil {
		return pddChatCaptureState{}, err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return pddChatCaptureState{}, err
	}
	var state pddChatCaptureState
	err = json.Unmarshal(raw, &state)
	return state, err
}

func (w *worker) flushPDDChatCapture(page playwright.Page, accountID, mallSN, goodsID, orderID string) error {
	state, err := w.drainPDDChatCapture(page)
	if err != nil || (len(state.Captures) == 0 && state.TitanWakeups == 0) {
		return err
	}
	mallID := ""
	for _, capture := range state.Captures {
		if capture.MallID != "" {
			mallID = capture.MallID
			break
		}
	}
	_, err = w.jsonRequest(http.MethodPost, "/api/pdd/chat/capture", map[string]any{"pdd_account_id": accountID, "mall_sn": mallSN, "mall_id": mallID, "goods_id": goodsID, "pdd_order_id": orderID, "page_url": page.URL(), "titan_wakeups": state.TitanWakeups, "captures": state.Captures}, nil)
	return err
}

func (w *worker) syncPDDChatInbox() error {
	var accountID, mallSN, goodsID, orderID, pageURL string
	err := w.database.QueryRow(`SELECT pdd_account_id,mall_sn,goods_id,pdd_order_id,page_url FROM pdd_chat_conversations WHERE status='active' ORDER BY last_sync_at ASC LIMIT 1`).Scan(&accountID, &mallSN, &goodsID, &orderID, &pageURL)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	ctx, site, err := w.taskContext(task{PDDAccountID: accountID})
	if err != nil {
		return err
	}
	defer ctx.Close()
	if err = installPDDChatObserver(ctx); err != nil {
		return err
	}
	page, err := ctx.NewPage()
	if err != nil {
		return err
	}
	defer page.Close()
	if strings.TrimSpace(pageURL) == "" || pddsite.Detect(pageURL) != site {
		pageURL = merchantChatURL(site, goodsID, mallSN)
	}
	if _, err = page.Goto(pageURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded}); err != nil {
		return err
	}
	time.Sleep(2500 * time.Millisecond)
	state, err := w.drainPDDChatCapture(page)
	if err != nil {
		return err
	}
	if state.TitanWakeups > 0 {
		if reload, reloadErr := page.Reload(playwright.PageReloadOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded}); reloadErr == nil && reload != nil {
			time.Sleep(1200 * time.Millisecond)
			more, _ := w.drainPDDChatCapture(page)
			state.Captures = append(state.Captures, more.Captures...)
			state.TitanWakeups += more.TitanWakeups
		}
	}
	mallID := ""
	for _, capture := range state.Captures {
		if capture.MallID != "" {
			mallID = capture.MallID
			break
		}
	}
	_, err = w.jsonRequest(http.MethodPost, "/api/pdd/chat/capture", map[string]any{"pdd_account_id": accountID, "mall_sn": mallSN, "mall_id": mallID, "goods_id": goodsID, "pdd_order_id": orderID, "page_url": page.URL(), "titan_wakeups": state.TitanWakeups, "captures": state.Captures}, nil)
	return err
}

func (w *worker) claimMessage() (messageTask, error) {
	var raw map[string]any
	status, err := w.jsonRequest(http.MethodPost, "/api/pdd/messages/claim", map[string]any{"worker_id": "pdd-worker", "lease_seconds": 180}, &raw)
	if status == http.StatusNotFound {
		return messageTask{}, errNoTask
	}
	if err != nil {
		return messageTask{}, err
	}
	text := func(k string) string { v, _ := raw[k].(string); return v }
	return messageTask{ID: text("id"), LeaseToken: text("lease_token"), PDDAccountID: text("pdd_account_id"), GoodsID: text("goods_id"), SKUID: text("sku_id"), MallSN: text("mall_sn"), CapturedChatURL: text("captured_chat_url"), TaskType: text("task_type"), Message: text("message"), Action: text("action"), PDDOrderID: text("pdd_order_id")}, nil
}

func (w *worker) messageHeartbeat(t messageTask) error {
	_, err := w.jsonRequest(http.MethodPost, "/api/pdd/messages/"+t.ID+"/heartbeat", map[string]any{"lease_token": t.LeaseToken}, nil)
	return err
}
func (w *worker) messageResult(t messageTask, status, reason string, result map[string]any) error {
	_, err := w.jsonRequest(http.MethodPost, "/api/pdd/messages/"+t.ID+"/result", map[string]any{"lease_token": t.LeaseToken, "status": status, "error": reason, "result": result}, nil)
	if err != nil {
		return fmt.Errorf("%s（回传失败: %v）", reason, err)
	}
	if status != "verified" {
		return errors.New(reason)
	}
	return nil
}

var errNoTask = errors.New("没有可领取任务")

func (w *worker) syncLogistics() error { return w.syncLogisticsWithForce(false) }
func (w *worker) syncLogisticsWithForce(force bool) error {
	enabledRaw, _ := w.store.Settings.Get(context.Background(), "pdd_logistics_sync_enabled")
	if !force && !settingBool(enabledRaw) {
		return nil
	}
	if !force && !w.hasPendingLogisticsDemand() {
		return nil
	}
	minutes := w.logisticsIntervalAt(time.Now())
	if !force && minutes == 0 {
		return nil
	}
	if !w.lastLogisticsSync.IsZero() && time.Since(w.lastLogisticsSync) < time.Duration(minutes)*time.Minute {
		return nil
	}
	w.lastLogisticsSync = time.Now()
	var accountID string
	if err := w.database.QueryRow(`SELECT id FROM pdd_accounts WHERE enabled=1 AND is_default=1 ORDER BY updated_at DESC LIMIT 1`).Scan(&accountID); err != nil {
		return nil
	}
	ctx, site, err := w.taskContext(task{PDDAccountID: accountID})
	if err != nil {
		return err
	}
	defer ctx.Close()
	page, err := ctx.NewPage()
	if err != nil {
		return err
	}
	if _, err = page.Goto(site.URL("/orders.html", url.Values{"type": {"3"}, "comment_tab": {"1"}, "combine_orders": {"1"}, "main_orders": {"1"}, "order_index": {"0"}}), playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded}); err != nil {
		return err
	}
	html, err := page.Content()
	if err != nil {
		return err
	}
	seen, shipments := map[string]bool{}, []pddshipping.Shipment{}
	for _, orderID := range pddshipping.ParseOrderIDs([]byte(html)) {
		if seen[orderID] {
			continue
		}
		seen[orderID] = true
		if _, err = page.Goto(site.URL("/order.html", url.Values{"order_sn": {orderID}}), playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded}); err != nil {
			continue
		}
		detail, _ := page.Content()
		shipments = append(shipments, pddshipping.ParseHTML([]byte(detail))...)
	}
	if len(shipments) == 0 {
		return nil
	}
	log.Printf("待收货物流解析完成: 订单 %d 条，物流 %d 条", len(seen), len(shipments))
	for _, shipment := range shipments {
		log.Printf("待收货物流: order=%s goods=%s sku=%s company=%s tracking=%s", shipment.OrderID, shipment.GoodsID, shipment.SKUID, shipment.Company, shipment.TrackingNumber)
	}
	var result struct {
		ReadyOrderIDs []string `json:"ready_order_ids"`
	}
	if _, err = w.jsonRequest(http.MethodPost, "/api/fulfillment/logistics/snapshot", map[string]any{"shipments": shipments}, &result); err != nil {
		return err
	}
	autoRaw, _ := w.store.Settings.Get(context.Background(), "shipping_auto_enabled")
	if !settingBool(autoRaw) {
		return nil
	}
	for _, orderID := range result.ReadyOrderIDs {
		if err := w.autoShipOrder(orderID); err != nil {
			log.Printf("自动闲鱼发货失败: order=%s err=%v", orderID, err)
		}
	}
	return nil
}

func (w *worker) hasPendingLogisticsDemand() bool {
	var count int
	err := w.database.QueryRow(`SELECT COUNT(*) FROM order_fulfillments WHERE pdd_order_id<>'' AND pdd_shipped=0 AND xianyu_shipped=0 AND fulfillment_exempt=0`).Scan(&count)
	return err == nil && count > 0
}

func (w *worker) autoShipOrder(orderID string) error {
	var precheck struct {
		Ready          bool     `json:"ready"`
		Problems       []string `json:"problems"`
		TrackingNumber string   `json:"tracking_number"`
		AddressID      int64    `json:"address_id"`
	}
	if _, err := w.jsonRequest(http.MethodPost, "/api/fulfillment/orders/"+url.PathEscape(orderID)+"/shipping-precheck", nil, &precheck); err != nil {
		return err
	}
	if !precheck.Ready {
		return fmt.Errorf("发货预检查未通过: %s", strings.Join(precheck.Problems, "；"))
	}
	req, _ := http.NewRequest(http.MethodPost, w.baseURL+"/api/fulfillment/orders/"+url.PathEscape(orderID)+"/ship", nil)
	req.Header.Set("Authorization", "Bearer "+w.apiKey)
	req.Header.Set("Idempotency-Key", fmt.Sprintf("auto-ship-%s-%s-%d", orderID, precheck.TrackingNumber, precheck.AddressID))
	response, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode/100 != 2 {
		raw, _ := io.ReadAll(response.Body)
		return fmt.Errorf("HTTP %d: %s", response.StatusCode, raw)
	}
	return nil
}

func (w *worker) syncLogisticsForce() error {
	w.lastLogisticsSync = time.Time{}
	return w.syncLogisticsWithForce(true)
}

func (w *worker) logisticsIntervalAt(now time.Time) int {
	hour := now.In(time.FixedZone("Asia/Shanghai", 8*3600)).Hour()
	key, def := "pdd_logistics_normal_interval_minutes", 10
	if hour >= 16 && hour < 22 {
		key, def = "pdd_logistics_peak_interval_minutes", 2
	} else if hour < 8 {
		enabled, _ := w.store.Settings.Get(context.Background(), "pdd_logistics_night_enabled")
		if !settingBool(enabled) {
			return 0
		}
		key, def = "pdd_logistics_night_interval_minutes", 30
	}
	raw, _ := w.store.Settings.Get(context.Background(), key)
	if n, e := strconv.Atoi(strings.TrimSpace(raw)); e == nil && n >= 1 && n <= 1440 {
		return n
	}
	return def
}

func gotoUnpaidOrders(page playwright.Page, site pddsite.Site) error {
	if _, err := page.Goto(site.URL("/personal.html", nil), playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded}); err != nil {
		return err
	}
	_, err := page.Goto(ordersURL(site, "1"), playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded})
	return err
}

func unpaidOrdersURL(site pddsite.Site) string {
	return ordersURL(site, "1")
}

func ordersURL(site pddsite.Site, listType string) string {
	return site.URL("/orders.html", url.Values{"type": {listType}, "comment_tab": {"1"}, "combine_orders": {"1"}, "main_orders": {"1"}, "refer_page_name": {"personal"}, "refer_page_sn": {"10001"}, "order_index": {"0"}})
}

func (w *worker) waitForAPI(ctx context.Context) error {
	healthURL := w.baseURL + "/health"
	var lastErr error
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
		if err == nil {
			response, requestErr := w.client.Do(req)
			if requestErr == nil {
				_ = response.Body.Close()
				if response.StatusCode/100 == 2 {
					return nil
				}
				lastErr = fmt.Errorf("健康检查返回 HTTP %d", response.StatusCode)
			} else {
				lastErr = requestErr
			}
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("等待履约 API 就绪超时: %w", lastErr)
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (w *worker) startBrowser() error {
	var err error
	w.pw, err = playwright.Run()
	if err != nil {
		return fmt.Errorf("启动 Playwright: %w", err)
	}
	w.browser, err = w.pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{Headless: playwright.Bool(!strings.EqualFold(os.Getenv("PDD_HEADLESS"), "false"))})
	if err != nil {
		return fmt.Errorf("启动 Chromium: %w", err)
	}
	return nil
}

func (w *worker) openStore() error {
	dbURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if dbURL == "" {
		dbURL = "/app/data/xianyu_data.db"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	database, dialect, err := db.Open(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("打开系统数据库: %w", err)
	}
	w.database = database
	w.store = db.NewStore(database, dialect)
	return nil
}

func (w *worker) taskContext(t task) (playwright.BrowserContext, pddsite.Site, error) {
	if w.store == nil || w.store.PDDAccounts == nil {
		return nil, "", errors.New("拼多多账号存储未初始化")
	}
	account, err := w.store.PDDAccounts.GetByID(context.Background(), t.PDDAccountID)
	if err != nil {
		return nil, "", fmt.Errorf("读取任务绑定的拼多多账号: %w", err)
	}
	site, err := pddsite.Parse(account.Site)
	if err != nil {
		return nil, "", err
	}
	if !account.Enabled {
		return nil, "", errors.New("任务绑定的拼多多账号已禁用")
	}
	if strings.TrimSpace(account.Cookie) == "" {
		return nil, "", errors.New("任务绑定的拼多多账号 Cookie 为空")
	}
	options := playwright.BrowserNewContextOptions{Locale: playwright.String("zh-CN"), TimezoneId: playwright.String("Asia/Shanghai"), Viewport: &playwright.Size{Width: 1280, Height: 900}}
	if userAgent := strings.TrimSpace(account.UserAgent); userAgent != "" {
		options.UserAgent = playwright.String(userAgent)
	}
	browserContext, err := w.browser.NewContext(options)
	if err != nil {
		return nil, "", err
	}
	cookies := []playwright.OptionalCookie{}
	for _, part := range strings.Split(account.Cookie, ";") {
		pair := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(pair) == 2 && pair[0] != "" {
			cookies = append(cookies, playwright.OptionalCookie{Name: pair[0], Value: pair[1], URL: playwright.String(site.BaseURL())})
		}
	}
	if len(cookies) == 0 {
		_ = browserContext.Close()
		return nil, "", errors.New("设置中保存的拼多多 Cookie 格式无效")
	}
	if err := browserContext.AddCookies(cookies); err != nil {
		_ = browserContext.Close()
		return nil, "", err
	}
	// Ensure every account/site pair owns a distinct profile directory. The
	// current context is intentionally ephemeral; this path is the stable home
	// for site-scoped browser state when persistence is enabled.
	_ = os.MkdirAll(site.ProfileDir(env("PDD_BROWSER_DATA_DIR", "/app/browser_data"), account.ID), 0o700)
	return browserContext, site, nil
}

func (w *worker) loadGoodsSnapshot(t task, page playwright.Page, site pddsite.Site) (pddproduct.Snapshot, string, bool, error) {
	hours := 72
	if raw, err := w.store.Settings.Get(context.Background(), "pdd_product_refresh_interval_hours"); err == nil {
		if parsed, parseErr := strconv.Atoi(strings.TrimSpace(raw)); parseErr == nil && parsed >= 1 && parsed <= 720 {
			hours = parsed
		}
	}
	var cached string
	// Only a real curl/browser capture starts a new refresh window. A cache-hit
	// validation is still audited, but must not keep extending the cache forever.
	err := w.database.QueryRow(`SELECT snapshot_json FROM pdd_purchase_goods_snapshots WHERE pdd_account_id=? AND goods_id=? AND cache_hit=0 AND blocking_errors_json='[]' AND captured_at>=? ORDER BY captured_at DESC LIMIT 1`, t.PDDAccountID, t.GoodsID, time.Now().Add(-time.Duration(hours)*time.Hour).Unix()).Scan(&cached)
	if err == nil {
		var snapshot pddproduct.Snapshot
		if json.Unmarshal([]byte(cached), &snapshot) == nil && snapshot.GoodsID == t.GoodsID && (len(snapshot.GroupOrderIDs) == 0 || len(snapshot.GroupOffers) > 0) {
			return snapshot, "cache", true, nil
		}
	}
	account, err := w.store.PDDAccounts.GetByID(context.Background(), t.PDDAccountID)
	if err != nil {
		return pddproduct.Snapshot{}, "", false, err
	}
	var capturedURL string
	if scanErr := w.database.QueryRow(`SELECT final_url FROM pdd_collection_snapshots WHERE goods_id=? ORDER BY received_at DESC,id DESC LIMIT 1`, t.GoodsID).Scan(&capturedURL); scanErr != nil {
		_ = w.database.QueryRow(`SELECT final_url FROM pdd_products WHERE goods_id=?`, t.GoodsID).Scan(&capturedURL)
	}
	goodsURL := purchaseGoodsURL(capturedURL, t.GoodsID, site)
	req, _ := http.NewRequest(http.MethodGet, goodsURL, nil)
	req.Header.Set("Cookie", account.Cookie)
	if account.UserAgent != "" {
		req.Header.Set("User-Agent", account.UserAgent)
	}
	response, curlErr := w.client.Do(req)
	if curlErr == nil {
		defer response.Body.Close()
		if response.StatusCode/100 == 2 {
			raw, readErr := io.ReadAll(io.LimitReader(response.Body, 12<<20))
			if readErr == nil {
				if snapshot, parseErr := pddproduct.ParseHTML(raw); parseErr == nil {
					return snapshot, "curl", false, nil
				}
			}
		}
	}
	if _, err = page.Goto(goodsURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded}); err != nil {
		return pddproduct.Snapshot{}, "", false, fmt.Errorf("curl 商品页失败且浏览器回退失败: %w", err)
	}
	html, err := page.Content()
	if err != nil {
		return pddproduct.Snapshot{}, "", false, err
	}
	snapshot, err := pddproduct.ParseHTML([]byte(html))
	return snapshot, "browser", false, err
}

func purchaseGoodsURL(capturedURL, goodsID string, site pddsite.Site) string {
	return pddproduct.ProductURLForSite(capturedURL, goodsID, site)
}

func merchantChatURL(site pddsite.Site, goodsID, mallSN string) string {
	return site.URL("/chat_detail.html", url.Values{"goods_id": {goodsID}, "mall_sn": {mallSN}, "from": {"goods"}, "page_from": {"101"}})
}

func (w *worker) validateGoods(t task, snapshot pddproduct.Snapshot, source string, cacheHit bool) error {
	_, err := w.jsonRequest(http.MethodPost, "/api/fulfillment/purchase-tasks/"+t.ID+"/goods-validation", map[string]any{"lease_token": t.LeaseToken, "source": source, "cache_hit": cacheHit, "snapshot": snapshot}, nil)
	return err
}

func (w *worker) close() {
	if w.browser != nil {
		_ = w.browser.Close()
	}
	if w.pw != nil {
		_ = w.pw.Stop()
	}
	if w.database != nil {
		_ = w.database.Close()
	}
}

func (w *worker) runOne() error {
	t, err := w.claim()
	if err != nil {
		return err
	}
	browserContext, site, err := w.taskContext(t)
	if err != nil {
		return w.abort(t, err.Error())
	}
	defer browserContext.Close()
	page, err := browserContext.NewPage()
	if err != nil {
		return w.abort(t, err.Error())
	}
	defer page.Close()
	if t.RecoveryOnly {
		order, recoverErr := w.findCreatedOrder(t, page, site, 15*time.Second)
		if recoverErr != nil {
			_ = w.resultUnknown(t, recoverErr.Error())
			return recoverErr
		}
		return w.browserResult(t, order)
	}
	failedAfterSubmit := false
	defer func() {
		if failedAfterSubmit {
			_ = w.resultUnknown(t, "点击立即支付后未能唯一核对拼多多订单")
		}
	}()
	if err = w.heartbeat(t, "loading_goods"); err != nil {
		return w.abort(t, err.Error())
	}
	snapshot, source, cacheHit, err := w.loadGoodsSnapshot(t, page, site)
	if err != nil {
		return w.abort(t, "读取拼多多商品页失败: "+err.Error())
	}
	if err = w.heartbeat(t, "validating_goods"); err != nil {
		return w.abort(t, err.Error())
	}
	if err = w.validateGoods(t, snapshot, source, cacheHit); err != nil {
		return w.abort(t, "商品实时校验失败: "+err.Error())
	}

	if err = w.heartbeat(t, "browser_preparing"); err != nil {
		return w.abort(t, err.Error())
	}
	if err = gotoUnpaidOrders(page, site); err != nil {
		return w.abort(t, "读取待付款基线失败: "+err.Error())
	}
	beforeHTML, err := page.Content()
	if err != nil {
		return w.abort(t, err.Error())
	}
	if err = w.snapshot(t, "before", beforeHTML); err != nil {
		return w.abort(t, err.Error())
	}
	if err = w.applyAddress(t); err != nil {
		return w.abort(t, err.Error())
	}

	checkout, checkoutMode, checkoutErr := purchaseCheckoutURL(site, snapshot, t.GoodsID, t.SKUID, t.Quantity, time.Now())
	if checkoutErr != nil {
		return w.abort(t, "生成拼多多结算链接失败: "+checkoutErr.Error())
	}
	log.Printf("采购结算上下文: task=%s mode=%s detail_id=%s group_id=%s", t.ID, checkoutMode, snapshot.DetailID, snapshot.GroupID)
	if _, err = page.Goto(checkout, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded}); err != nil {
		return w.abort(t, "打开结算页失败: "+err.Error())
	}
	body, err := page.Locator("body").InnerText()
	if err != nil {
		return w.abort(t, "读取结算页失败: "+err.Error())
	}
	for _, expected := range []string{t.ReceiverName, t.Province, t.City, t.District, t.DetailAddress} {
		if expected != "" && !strings.Contains(pddcheckout.NormalizeAddress(body), pddcheckout.NormalizeAddress(expected)) {
			return w.abort(t, "结算页地址核对失败: "+expected)
		}
	}
	if err = setCheckoutQuantity(page, t.Quantity); err != nil {
		return w.abort(t, "设置数量失败: "+err.Error())
	}
	time.Sleep(300 * time.Millisecond)
	body, err = page.Locator("body").InnerText()
	if err != nil {
		return w.abort(t, "设置数量后读取结算页失败: "+err.Error())
	}
	amount, err := checkoutAmountCent(body)
	if err != nil {
		return w.abort(t, err.Error())
	}
	if amount > t.XianyuAmountCent-50 {
		profit := t.XianyuAmountCent - amount
		return w.abort(t, fmt.Sprintf("预计利润低于 0.5 元（闲鱼金额 %.2f 元，拼多多结算金额 %.2f 元，预计利润 %.2f 元）", float64(t.XianyuAmountCent)/100, float64(amount)/100, float64(profit)/100))
	}
	if err = w.heartbeat(t, "submitting_unpaid_order"); err != nil {
		return w.abort(t, err.Error())
	}
	_ = w.screenshot(page, t.ID+"-before-submit.png")
	if !w.submit {
		return w.abort(t, "只读验证完成；设置 PDD_PURCHASE_SUBMIT=true 才会创建待付款订单")
	}
	button := page.Locator(`[role="button"]`).Filter(playwright.LocatorFilterOptions{HasText: "立即支付"}).Last()
	if count, _ := button.Count(); count != 1 {
		return w.abort(t, "无法唯一定位立即支付按钮")
	}
	failedAfterSubmit = true
	if err = button.Click(); err != nil {
		return fmt.Errorf("点击立即支付失败: %w", err)
	}
	before, err := pddcheckout.ParseUnpaidHTML([]byte(beforeHTML))
	if err != nil {
		return err
	}
	for _, order := range before {
		t.BeforeOrderSNs = append(t.BeforeOrderSNs, order.OrderID)
	}
	order, err := w.findCreatedOrder(t, page, site, 45*time.Second)
	if err != nil {
		return err
	}
	failedAfterSubmit = false
	return w.browserResult(t, order)
}

func purchaseCheckoutURL(site pddsite.Site, snapshot pddproduct.Snapshot, goodsID, skuID string, quantity int, now time.Time) (string, string, error) {
	if strings.TrimSpace(goodsID) == "" || strings.TrimSpace(skuID) == "" || quantity < 1 {
		return "", "", errors.New("商品、SKU或数量无效")
	}
	values := url.Values{"goods_id": {goodsID}, "sku_id": {skuID}, "goods_number": {strconv.Itoa(quantity)}}
	if offer, ok, err := pddproduct.SelectActiveGroupOffer(snapshot, now); err != nil {
		return "", "", err
	} else if ok {
		values.Set("group_id", offer.GroupID)
		values.Set("detail_id", offer.DetailID)
		values.Set("group_order_id", offer.GroupOrderID)
		values.Set("is_history_group", "1")
		values.Set("page_from", "0")
		return site.URL("/order_checkout.html", values), "join_group", nil
	}
	if snapshot.GroupID != "" && snapshot.DetailID != "" {
		values.Set("group_id", snapshot.GroupID)
		values.Set("detail_id", snapshot.DetailID)
		values.Set("page_from", "31")
		return site.URL("/order_checkout.html", values), "create_group", nil
	}
	return site.URL("/order_checkout.html", values), "direct", nil
}

func (w *worker) findCreatedOrder(t task, page playwright.Page, site pddsite.Site, timeout time.Duration) (pddcheckout.Order, error) {
	seen := map[string]bool{}
	for _, id := range t.BeforeOrderSNs {
		seen[id] = true
	}
	deadline := time.Now().Add(timeout)
	lastCount := 0
	lastObserved := []string{}
	var lastHTML string
	for {
		listTypes := []string{"1"}
		if t.RecoveryOnly {
			listTypes = append(listTypes, "0")
		}
		candidateByID := map[string]pddcheckout.Order{}
		lastObserved = lastObserved[:0]
		for _, listType := range listTypes {
			if _, navErr := page.Goto(ordersURL(site, listType), playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded}); navErr != nil {
				continue
			}
			time.Sleep(800 * time.Millisecond)
			if html, contentErr := page.Content(); contentErr == nil {
				lastHTML = html
				if orders, parseErr := pddcheckout.ParseOrdersHTML([]byte(html), listType); parseErr == nil {
					for _, order := range orders {
						lastObserved = append(lastObserved, fmt.Sprintf("%s(goods=%s sku=%s qty=%d time=%d)", order.OrderID, order.GoodsID, order.SKUID, order.Quantity, order.OrderTime))
						if !seen[order.OrderID] && order.GoodsID == t.GoodsID && order.SKUID == t.SKUID && int(order.Quantity) == t.Quantity && (t.StartedAt == 0 || order.OrderTime >= t.StartedAt-5) {
							candidateByID[order.OrderID] = order
						}
					}
				}
			}
		}
		lastCount = len(candidateByID)
		if len(candidateByID) == 1 {
			for _, candidate := range candidateByID {
				return w.verifyCreatedOrder(t, page, site, candidate)
			}
		}
		if time.Now().After(deadline) {
			if lastHTML != "" {
				_ = os.MkdirAll(w.screenshotDir, 0o700)
				_ = os.WriteFile(filepath.Join(w.screenshotDir, t.ID+"-unpaid-recovery.html"), []byte(lastHTML), 0o600)
				_ = w.screenshot(page, t.ID+"-unpaid-recovery.png")
			}
			return pddcheckout.Order{}, fmt.Errorf("等待待付款订单超时（候选=%d，已解析=%s）", lastCount, strings.Join(lastObserved, ","))
		}
		time.Sleep(2 * time.Second)
	}
}

func (w *worker) verifyCreatedOrder(t task, page playwright.Page, site pddsite.Site, order pddcheckout.Order) (pddcheckout.Order, error) {
	if _, err := page.Goto(site.URL("/order.html", url.Values{"order_sn": {order.OrderID}}), playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded}); err != nil {
		return pddcheckout.Order{}, err
	}
	detailBody, err := page.Locator("body").InnerText()
	if err != nil {
		return pddcheckout.Order{}, err
	}
	for _, expected := range []string{t.ReceiverName, t.DetailAddress} {
		if expected != "" && !strings.Contains(pddcheckout.NormalizeAddress(detailBody), pddcheckout.NormalizeAddress(expected)) {
			return pddcheckout.Order{}, fmt.Errorf("订单详情地址核对失败: %s", expected)
		}
	}
	_ = w.screenshot(page, t.ID+"-order-detail.png")
	return order, nil
}

func quantityButtonPlan(current, desired int) (string, int, error) {
	if current < 1 || desired < 1 {
		return "", 0, errors.New("商品数量必须大于 0")
	}
	if current == desired {
		return "", 0, nil
	}
	if desired > current {
		return "增加数量", desired - current, nil
	}
	return "减少数量", current - desired, nil
}

func setCheckoutQuantity(page playwright.Page, desired int) error {
	input := page.Locator(`input[type="number"]`).First()
	raw, err := input.InputValue()
	if err != nil {
		return fmt.Errorf("读取当前数量: %w", err)
	}
	current, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("当前数量无效: %q", raw)
	}
	buttonLabel, clicks, err := quantityButtonPlan(current, desired)
	if err != nil || clicks == 0 {
		return err
	}
	button := page.Locator(`[role="button"][aria-label="` + buttonLabel + `"]`).First()
	if count, countErr := button.Count(); countErr != nil || count != 1 {
		return fmt.Errorf("无法唯一定位%s按钮", buttonLabel)
	}
	for step := 0; step < clicks; step++ {
		if err := button.Click(); err != nil {
			return fmt.Errorf("点击%s: %w", buttonLabel, err)
		}
		expected := current
		if buttonLabel == "增加数量" {
			expected++
		} else {
			expected--
		}
		deadline := time.Now().Add(2 * time.Second)
		for {
			value, readErr := input.InputValue()
			actual, parseErr := strconv.Atoi(strings.TrimSpace(value))
			if readErr == nil && parseErr == nil && actual == expected {
				current = actual
				break
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("点击%s后数量未变为 %d", buttonLabel, expected)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	return nil
}

var amountPattern = regexp.MustCompile(`(?:实付|合计|应付)[^0-9￥¥]{0,20}[￥¥]?\s*([0-9]+(?:\.[0-9]{1,2})?)`)

func checkoutAmountCent(body string) (int64, error) {
	matches := amountPattern.FindAllStringSubmatch(body, -1)
	values := map[int64]bool{}
	for _, match := range matches {
		value, _ := strconv.ParseFloat(match[1], 64)
		if value > 0 {
			values[int64(value*100+0.5)] = true
		}
	}
	if len(values) != 1 {
		return 0, fmt.Errorf("无法唯一读取结算金额（候选=%d）", len(values))
	}
	for value := range values {
		return value, nil
	}
	return 0, errors.New("结算金额无效")
}

func (w *worker) claim() (task, error) {
	var raw map[string]any
	status, err := w.jsonRequest(http.MethodPost, "/api/fulfillment/purchase-tasks/claim", map[string]any{"worker_id": "pdd-worker", "lease_seconds": 180}, &raw)
	if status == http.StatusNotFound {
		return task{}, errNoTask
	}
	if err != nil {
		return task{}, err
	}
	text := func(key string) string { value, _ := raw[key].(string); return value }
	number := func(key string) int64 { value, _ := raw[key].(float64); return int64(value) }
	before := []string{}
	if values, ok := raw["before_order_sns"].([]any); ok {
		for _, value := range values {
			if id, ok := value.(string); ok {
				before = append(before, id)
			}
		}
	}
	recovery, _ := raw["recovery_only"].(bool)
	return task{ID: text("id"), OrderID: text("order_id"), LeaseToken: text("lease_token"), PDDAccountID: text("pdd_account_id"), GoodsID: text("source_goods_id"), SKUID: text("source_sku_id"), ReceiverName: text("receiver_name"), Province: text("province"), City: text("city"), District: text("district"), DetailAddress: text("detail_address"), Quantity: int(number("quantity")), XianyuAmountCent: number("xianyu_amount_cent"), StartedAt: number("started_at"), BeforeOrderSNs: before, RecoveryOnly: recovery}, nil
}

func (w *worker) heartbeat(t task, status string) error {
	w.reportState(status, "")
	_, err := w.jsonRequest(http.MethodPost, "/api/fulfillment/purchase-tasks/"+t.ID+"/heartbeat", map[string]any{"lease_token": t.LeaseToken, "status": status, "lease_seconds": 180}, nil)
	return err
}
func (w *worker) abort(t task, reason string) error {
	_, err := w.jsonRequest(http.MethodPost, "/api/fulfillment/purchase-tasks/"+t.ID+"/abort", map[string]any{"lease_token": t.LeaseToken, "reason": reason}, nil)
	if err != nil {
		return fmt.Errorf("%s（中止任务失败: %v）", reason, err)
	}
	return errors.New(reason)
}
func (w *worker) resultUnknown(t task, reason string) error {
	_, err := w.jsonRequest(http.MethodPost, "/api/fulfillment/purchase-tasks/"+t.ID+"/browser-result", map[string]any{"lease_token": t.LeaseToken, "status": "result_unknown", "error": reason}, nil)
	return err
}
func (w *worker) applyAddress(t task) error {
	req, _ := http.NewRequest(http.MethodPost, w.baseURL+"/api/fulfillment/orders/"+url.PathEscape(t.OrderID)+"/pdd-address/apply", nil)
	req.Header.Set("Authorization", "Bearer "+w.apiKey)
	req.Header.Set("Idempotency-Key", t.OrderID+"-purchase-"+t.ID)
	response, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode/100 != 2 {
		raw, _ := io.ReadAll(response.Body)
		return fmt.Errorf("修改地址失败 HTTP %d: %s", response.StatusCode, raw)
	}
	return nil
}
func (w *worker) snapshot(t task, phase, html string) error {
	endpoint := "/api/fulfillment/purchase-tasks/" + t.ID + "/unpaid-snapshot?phase=" + phase + "&lease_token=" + url.QueryEscape(t.LeaseToken)
	req, _ := http.NewRequest(http.MethodPost, w.baseURL+endpoint, strings.NewReader(html))
	req.Header.Set("Authorization", "Bearer "+w.apiKey)
	req.Header.Set("Content-Type", "text/html")
	response, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode/100 != 2 {
		raw, _ := io.ReadAll(response.Body)
		return fmt.Errorf("保存待付款基线失败 HTTP %d: %s", response.StatusCode, raw)
	}
	return nil
}
func (w *worker) browserResult(t task, order pddcheckout.Order) error {
	payload := map[string]any{"lease_token": t.LeaseToken, "status": "unpaid_order_created", "pdd_order": map[string]any{"order_id": order.OrderID, "group_order_id": order.GroupOrderID, "goods_id": order.GoodsID, "sku_id": order.SKUID, "quantity": order.Quantity, "address_id": order.AddressID, "amount_cent": order.AmountCent, "order_time": order.OrderTime, "payment_deadline": order.PaymentDeadline, "receiver_name": t.ReceiverName, "province": t.Province, "city": t.City, "district": t.District, "detail_address": t.DetailAddress}}
	_, err := w.jsonRequest(http.MethodPost, "/api/fulfillment/purchase-tasks/"+t.ID+"/browser-result", payload, nil)
	return err
}
func (w *worker) jsonRequest(method, path string, input, output any) (int, error) {
	var body io.Reader
	if input != nil {
		raw, _ := json.Marshal(input)
		body = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, w.baseURL+path, body)
	req.Header.Set("Authorization", "Bearer "+w.apiKey)
	req.Header.Set("Content-Type", "application/json")
	response, err := w.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)
	if response.StatusCode/100 != 2 {
		return response.StatusCode, fmt.Errorf("HTTP %d: %s", response.StatusCode, raw)
	}
	if output != nil && len(raw) > 0 {
		if err = json.Unmarshal(raw, output); err != nil {
			return response.StatusCode, err
		}
	}
	return response.StatusCode, nil
}
func (w *worker) screenshot(page playwright.Page, name string) error {
	if err := os.MkdirAll(w.screenshotDir, 0700); err != nil {
		return err
	}
	_, err := page.Screenshot(playwright.PageScreenshotOptions{Path: playwright.String(filepath.Join(w.screenshotDir, name)), FullPage: playwright.Bool(true)})
	return err
}
func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
func settingBool(raw string) bool {
	raw = strings.ToLower(strings.TrimSpace(raw))
	return raw == "true" || raw == "1" || raw == "yes" || raw == "on"
}
