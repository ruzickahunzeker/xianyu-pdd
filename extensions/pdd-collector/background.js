import { collectDefaultAddressID, collectPDDProduct, collectPDDReviewMedia, isPDDAddressPage } from "./collector.js";

const PDD_SITES = {
  "mobile.pinduoduo.com": { site: "pinduoduo", cookieDomain: ".pinduoduo.com" },
  "mobile.yangkeduo.com": { site: "yangkeduo", cookieDomain: ".yangkeduo.com" }
};

function siteFromURL(rawURL) {
  const parsed = new URL(rawURL);
  const site = PDD_SITES[parsed.hostname];
  if (!site || parsed.protocol !== "https:") throw new Error("当前页面不是支持的拼多多移动站点");
  return { ...site, host: parsed.hostname, baseURL: `https://${parsed.hostname}` };
}

const DEFAULT_SETTINGS = {
  serverURL: "http://127.0.0.1:59188",
  deviceToken: "",
 };

async function settings() {
  return { ...DEFAULT_SETTINGS, ...(await chrome.storage.local.get(DEFAULT_SETTINGS)) };
}

async function activeTab() {
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  if (!tab?.id) throw new Error("没有可采集的当前标签页");
  if (!/^https?:\/\//.test(tab.url ?? "")) throw new Error("当前页面不是 HTTP/HTTPS 页面");
  return tab;
}

async function capture() {
  const tab = await activeTab();
  const [{ result }] = await chrome.scripting.executeScript({
    target: { tabId: tab.id },
    world: "MAIN",
    func: () => JSON.stringify(globalThis.rawData)
  });
  const currentSite = siteFromURL(tab.url);
  const normalized = { ...collectPDDProduct(JSON.parse(result), tab.url), site: currentSite.site, collection_id: crypto.randomUUID() };
  await chrome.storage.local.set({ lastCollection: normalized });
  return normalized;
}

function endpoint(baseURL) {
  const base = baseURL.replace(/\/+$/, "");
  return `${base}/api/pdd-collector/products`;
}

function reviewMediaEndpoint(baseURL) {
  return `${baseURL.replace(/\/+$/, "")}/api/pdd-collector/review-media`;
}

async function upload(payload) {
  const config = await settings();
  if (!config.serverURL) throw new Error("请先配置服务端地址");
  if (!config.deviceToken) throw new Error("请先配置设备 Token");

  const response = await fetch(endpoint(config.serverURL), {
    method: "POST",
    headers: {
      "Authorization": `Bearer ${config.deviceToken}`,
      "Content-Type": "application/json",
      "X-Collector-Version": chrome.runtime.getManifest().version
    },
    body: JSON.stringify(payload)
  });
  const text = await response.text();
  let body;
  try { body = text ? JSON.parse(text) : {}; } catch { body = { message: text }; }
  if (!response.ok) throw new Error(body?.detail || body?.message || `上传失败：HTTP ${response.status}`);
  return body;
}

async function captureReviewMedia() {
  const tab = await activeTab();
  siteFromURL(tab.url);
  const [{ result }] = await chrome.scripting.executeScript({ target: { tabId: tab.id }, world: "MAIN", func: () => JSON.stringify(globalThis.rawData || null) });
  const data = collectPDDReviewMedia(JSON.parse(result), tab.url);
  await chrome.storage.local.set({ lastReviewMedia: data });
  return data;
}

async function uploadReviewMedia(payload) {
  const config = await settings();
  if (!config.serverURL) throw new Error("请先配置服务端地址");
  if (!config.deviceToken) throw new Error("请先配置设备 Token");
  const target = reviewMediaEndpoint(config.serverURL);
  const response = await fetch(target, { method: "POST", headers: { "Authorization": `Bearer ${config.deviceToken}`, "Content-Type": "application/json", "X-Collector-Version": chrome.runtime.getManifest().version }, body: JSON.stringify(payload) });
  const text = await response.text();
  let body;
  try { body = text ? JSON.parse(text) : {}; } catch { body = { message: text }; }
  if (!response.ok) throw new Error(body?.detail || body?.message || `上传失败：HTTP ${response.status}（${target}）`);
  return body;
}

async function captureAccount() {
  const tab = await activeTab();
  const currentSite = siteFromURL(tab.url);
  if (!isPDDAddressPage(tab.url)) {
    const error = new Error("请先打开拼多多收货地址页");
    error.code = "ADDRESS_PAGE_REQUIRED";
    throw error;
  }
	// Never merge credentials from the two domains. A captured account belongs
	// to exactly one site and the server enforces the same boundary.
  const cookies = await chrome.cookies.getAll({ domain: currentSite.cookieDomain });
  const unique = new Map(cookies.map(item => [`${item.name};${item.domain};${item.path}`, item]));
  const cookie = [...unique.values()].map(item => `${item.name}=${item.value}`).join("; ");
  const pddUID = [...unique.values()].find(item => item.name === "pdd_user_id")?.value || "";
  if (!pddUID) throw new Error("未读取到 pdd_user_id，请确认拼多多已登录");
  const [{ result }] = await chrome.scripting.executeScript({
    target: { tabId: tab.id },
    world: "MAIN",
    func: async () => {
      const deadline = Date.now() + 8000;
      while (Date.now() < deadline) {
        if (globalThis.rawData && typeof globalThis.rawData === "object") break;
        await new Promise(resolve => setTimeout(resolve, 250));
      }
      return { rawData: globalThis.rawData || null, userAgent: navigator.userAgent };
    }
  });
  const data = { site: currentSite.site, base_url: currentSite.baseURL, cookie_domain: currentSite.cookieDomain, cookie, pdd_uid: pddUID, default_address_id: collectDefaultAddressID(result?.rawData), user_agent: result?.userAgent || "", captured_at: new Date().toISOString() };
  await chrome.storage.local.set({ lastPDDAccount: data });
  return { ...data, cookie_masked: `${cookie.slice(0, 18)}…（${unique.size} 项）` };
}

async function openAddressPage() {
  const tab = await activeTab();
  let currentSite;
  try { currentSite = siteFromURL(tab.url); } catch { currentSite = { baseURL: "https://mobile.pinduoduo.com" }; }
  const addressURL = `${currentSite.baseURL}/addresses.html`;
  await chrome.tabs.update(tab.id, { url: addressURL });
  return { url: addressURL };
}

chrome.runtime.onMessage.addListener((message, _sender, sendResponse) => {
  const action = message?.type === "CAPTURE" ? capture()
      : message?.type === "UPLOAD" ? upload(message.payload)
      : message?.type === "CAPTURE_ACCOUNT" ? captureAccount()
        : message?.type === "OPEN_ADDRESS_PAGE" ? openAddressPage()
          : message?.type === "CAPTURE_REVIEW_MEDIA" ? captureReviewMedia()
            : message?.type === "UPLOAD_REVIEW_MEDIA" ? uploadReviewMedia(message.payload)
      : Promise.reject(new Error("未知操作"));
  action.then((data) => sendResponse({ ok: true, data }))
    .catch((error) => sendResponse({ ok: false, error: error?.message || String(error), code: error?.code || "" }));
  return true;
});
