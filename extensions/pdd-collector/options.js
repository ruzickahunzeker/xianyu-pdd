const defaults = {
  serverURL: "http://127.0.0.1:59188",
  deviceToken: ""
};

const config = { ...defaults, ...(await chrome.storage.local.get(defaults)) };
document.querySelector("#server-url").value = config.serverURL;
if (config.serverURL === "http://127.0.0.1:8080") {
  document.querySelector("#saved").textContent = "旧默认端口 8080，请改为 59188 后保存";
}
document.querySelector("#device-token").value = config.deviceToken;

document.querySelector("#settings-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  await chrome.storage.local.set({
    serverURL: document.querySelector("#server-url").value.trim().replace(/\/+$/, ""),
    deviceToken: document.querySelector("#device-token").value.trim()
  });
  const saved = document.querySelector("#saved");
  saved.textContent = "已保存";
  setTimeout(() => { saved.textContent = ""; }, 1500);
});
