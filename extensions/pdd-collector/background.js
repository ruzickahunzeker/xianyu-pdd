import { collectPDDProduct } from "./collector.js";

const DEFAULT_SETTINGS = {
  serverURL: "http://127.0.0.1:8080",
  deviceToken: "",
  taskID: "",
  leaseToken: ""
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
  const normalized = collectPDDProduct(JSON.parse(result));
  await chrome.storage.local.set({ lastCollection: normalized });
  return normalized;
}

function endpoint(baseURL, taskID) {
  const base = baseURL.replace(/\/+$/, "");
  return `${base}/api/pdd-collector/tasks/${encodeURIComponent(taskID)}/complete`;
}

async function upload(payload) {
  const config = await settings();
  if (!config.serverURL) throw new Error("请先配置服务端地址");
  if (!config.deviceToken) throw new Error("请先配置设备 Token");
  if (!config.taskID) throw new Error("请先配置采集任务 ID");
  if (!config.leaseToken) throw new Error("请先配置任务租约 Token");

  const response = await fetch(endpoint(config.serverURL, config.taskID), {
    method: "POST",
    headers: {
      "Authorization": `Bearer ${config.deviceToken}`,
      "Content-Type": "application/json",
      "X-Collector-Version": chrome.runtime.getManifest().version
    },
    body: JSON.stringify({ ...payload, lease_token: config.leaseToken })
  });
  const text = await response.text();
  let body;
  try { body = text ? JSON.parse(text) : {}; } catch { body = { message: text }; }
  if (!response.ok) throw new Error(body?.message || `上传失败：HTTP ${response.status}`);
  return body;
}

chrome.runtime.onMessage.addListener((message, _sender, sendResponse) => {
  const action = message?.type === "CAPTURE" ? capture()
    : message?.type === "UPLOAD" ? upload(message.payload)
      : Promise.reject(new Error("未知操作"));
  action.then((data) => sendResponse({ ok: true, data }))
    .catch((error) => sendResponse({ ok: false, error: error?.message || String(error) }));
  return true;
});
