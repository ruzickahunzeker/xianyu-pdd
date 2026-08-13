let payload = null;

const elements = {
  status: document.querySelector("#status"),
  capture: document.querySelector("#capture"),
  upload: document.querySelector("#upload"),
  copy: document.querySelector("#copy"),
  summary: document.querySelector("#summary"),
  previewWrap: document.querySelector("#preview-wrap"),
  preview: document.querySelector("#preview"),
  goodsID: document.querySelector("#goods-id"),
  title: document.querySelector("#title"),
  skuCount: document.querySelector("#sku-count")
};

function status(text, kind = "") {
  elements.status.textContent = text;
  elements.status.dataset.kind = kind;
}

function showPayload(data) {
  payload = data;
  elements.goodsID.textContent = data.goods.goods_id;
  elements.title.textContent = data.goods.title || "（无标题）";
  elements.skuCount.textContent = String(data.skus.length);
  elements.preview.textContent = JSON.stringify(data, null, 2);
  elements.summary.classList.remove("hidden");
  elements.previewWrap.classList.remove("hidden");
  elements.upload.disabled = false;
  elements.copy.disabled = false;
}

async function call(message) {
  const response = await chrome.runtime.sendMessage(message);
  if (!response?.ok) throw new Error(response?.error || "扩展后台无响应");
  return response.data;
}

elements.capture.addEventListener("click", async () => {
  elements.capture.disabled = true;
  status("正在读取 window.rawData.store…");
  try {
    const data = await call({ type: "CAPTURE" });
    showPayload(data);
    status(`采集成功：${data.skus.length} 个 SKU`, "success");
  } catch (error) {
    status(error.message, "error");
  } finally {
    elements.capture.disabled = false;
  }
});

elements.copy.addEventListener("click", async () => {
  if (!payload) return;
  await navigator.clipboard.writeText(JSON.stringify(payload, null, 2));
  status("JSON 已复制", "success");
});

elements.upload.addEventListener("click", async () => {
  if (!payload) return;
  elements.upload.disabled = true;
  status("正在上传服务端…");
  try {
    await call({ type: "UPLOAD", payload });
    status("上传成功", "success");
  } catch (error) {
    status(error.message, "error");
  } finally {
    elements.upload.disabled = false;
  }
});

document.querySelector("#open-options").addEventListener("click", () => chrome.runtime.openOptionsPage());

const { lastCollection } = await chrome.storage.local.get("lastCollection");
if (lastCollection) showPayload(lastCollection);
