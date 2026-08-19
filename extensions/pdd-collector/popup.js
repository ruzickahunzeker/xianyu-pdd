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
let accountPayload = null;
let reviewMediaPayload = null;

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
  if (!response?.ok) {
    const error = new Error(response?.error || "扩展后台无响应");
    error.code = response?.code || "";
    throw error;
  }
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
    const result = await call({ type: "UPLOAD", payload });
    const stock = Number(result?.material_stock_updates || 0);
    status(`上传/更新成功：${Number(result?.sku_count || payload.skus.length)} 个 SKU${stock ? `，同步 ${stock} 个素材库存` : ""}`, "success");
  } catch (error) {
    status(error.message, "error");
  } finally {
    elements.upload.disabled = false;
  }
});

document.querySelector("#capture-review-media").addEventListener("click", async () => {
  const button = document.querySelector("#capture-review-media"); button.disabled = true; status("正在读取评论媒体…");
  try {
    const data = await call({ type: "CAPTURE_REVIEW_MEDIA" }); reviewMediaPayload = data;
    document.querySelector("#review-goods-id").textContent = data.goods_id;
    document.querySelector("#review-image-count").textContent = String(data.media.filter(item => item.media_type === "image").length);
    document.querySelector("#review-video-count").textContent = String(data.media.filter(item => item.media_type === "video").length);
    document.querySelector("#review-media-summary").classList.remove("hidden");
    document.querySelector("#upload-review-media").disabled = false;
    status(`采集成功：${data.media.length} 个评论媒体`, "success");
  } catch (error) { status(error.message.replace(/^\w+:\s*/, ""), "error"); } finally { button.disabled = false; }
});
document.querySelector("#upload-review-media").addEventListener("click", async () => {
  if (!reviewMediaPayload) return;
  const button = document.querySelector("#upload-review-media"); button.disabled = true; status("正在上传评论媒体…");
  try { const result = await call({ type: "UPLOAD_REVIEW_MEDIA", payload: reviewMediaPayload }); status(`上传成功：保存 ${result.saved_count || 0} 个媒体`, "success"); }
  catch (error) { status(error.message, "error"); } finally { button.disabled = false; }
});

document.querySelector("#open-options").addEventListener("click", () => chrome.runtime.openOptionsPage());

document.querySelector("#capture-account").addEventListener("click", async () => {
  const button = document.querySelector("#capture-account");
  const addressButton = document.querySelector("#open-address-page");
  button.disabled = true; addressButton.classList.add("hidden"); status("正在读取拼多多账号配置…");
  try {
    const data = await call({ type: "CAPTURE_ACCOUNT" }); accountPayload = data;
    document.querySelector("#pdd-uid").textContent = data.pdd_uid;
    document.querySelector("#pdd-address-id").textContent = data.default_address_id || "当前页面未找到";
    document.querySelector("#pdd-cookie").textContent = data.cookie_masked;
    document.querySelector("#account-summary").classList.remove("hidden");
    document.querySelector("#copy-account").disabled = false;
    status("账号配置读取成功", "success");
  } catch (error) {
    if (error.code === "ADDRESS_PAGE_REQUIRED") addressButton.classList.remove("hidden");
    status(error.message, "error");
  } finally { button.disabled = false; }
});
document.querySelector("#open-address-page").addEventListener("click", async () => {
  try {
    await call({ type: "OPEN_ADDRESS_PAGE" });
    window.close();
  } catch (error) { status(error.message, "error"); }
});
document.querySelector("#copy-account").addEventListener("click", async () => {
  if (!accountPayload) return;
  const { cookie_masked: _, ...data } = accountPayload;
  await navigator.clipboard.writeText(JSON.stringify(data, null, 2)); status("账号配置已复制，请妥善保管 Cookie", "success");
});

const { lastCollection } = await chrome.storage.local.get("lastCollection");
if (lastCollection) showPayload(lastCollection);
