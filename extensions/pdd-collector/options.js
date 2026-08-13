const defaults = {
  serverURL: "http://127.0.0.1:8080",
  deviceToken: "",
  taskID: "",
  leaseToken: ""
};

const config = { ...defaults, ...(await chrome.storage.local.get(defaults)) };
document.querySelector("#server-url").value = config.serverURL;
document.querySelector("#device-token").value = config.deviceToken;
document.querySelector("#task-id").value = config.taskID;
document.querySelector("#lease-token").value = config.leaseToken;

document.querySelector("#settings-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  await chrome.storage.local.set({
    serverURL: document.querySelector("#server-url").value.trim().replace(/\/+$/, ""),
    deviceToken: document.querySelector("#device-token").value.trim(),
    taskID: document.querySelector("#task-id").value.trim(),
    leaseToken: document.querySelector("#lease-token").value.trim()
  });
  const saved = document.querySelector("#saved");
  saved.textContent = "已保存";
  setTimeout(() => { saved.textContent = ""; }, 1500);
});
