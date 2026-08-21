const defaults = {
  serverURL: "http://127.0.0.1:59188",
  deviceToken: "",
  preferredSite: "pinduoduo"
};

const config = { ...defaults, ...(await chrome.storage.local.get(defaults)) };
document.querySelector("#server-url").value = config.serverURL;
if (config.serverURL === "http://127.0.0.1:8080") {
  document.querySelector("#saved").textContent = "旧默认端口 8080，请改为 59188 后保存";
}
document.querySelector("#device-token").value = config.deviceToken;
document.querySelector("#preferred-site").value = config.preferredSite;

function formConfig() {
  return {
    serverURL: document.querySelector("#server-url").value.trim().replace(/\/+$/, ""),
    deviceToken: document.querySelector("#device-token").value.trim(),
    preferredSite: document.querySelector("#preferred-site").value
  };
}

function showResult(text, kind = "") {
  const saved = document.querySelector("#saved");
  saved.textContent = text;
  saved.dataset.kind = kind;
}

document.querySelector("#settings-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  await chrome.storage.local.set(formConfig());
  showResult("已保存", "success");
  setTimeout(() => { showResult(""); }, 1500);
});

document.querySelector("#test-connection").addEventListener("click", async () => {
  const button = document.querySelector("#test-connection");
  const { serverURL, deviceToken } = formConfig();
  if (!serverURL || !deviceToken) {
    showResult("请先填写服务地址和设备 Token", "error");
    return;
  }
  button.disabled = true;
  showResult("正在测试…");
  try {
    const response = await fetch(`${serverURL}/api/pdd-collector/device`, {
      headers: { "Authorization": `Bearer ${deviceToken}` }
    });
    const text = await response.text();
    let body = {};
    try { body = text ? JSON.parse(text) : {}; } catch { body = { message: text }; }
    if (!response.ok) throw new Error(body?.detail || body?.message || `HTTP ${response.status}`);
    await chrome.storage.local.set(formConfig());
    showResult(`连接成功：${body.device_name || body.device_id}`, "success");
  } catch (error) {
    const reason = error instanceof TypeError
      ? "无法连接服务。请检查 IP、端口及 Docker 的 XIANYU_BIND_ADDRESS"
      : error.message;
    showResult(`连接失败：${reason}`, "error");
  } finally {
    button.disabled = false;
  }
});
