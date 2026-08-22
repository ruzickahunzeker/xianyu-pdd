import { DEFAULT_SERVER_URL, loadCollectorSettings, persistCollectorSettings } from "./settings-store.js";

let state = await loadCollectorSettings();

const profileSelect = document.querySelector("#profile-select");
const profileName = document.querySelector("#profile-name");
const serverURL = document.querySelector("#server-url");
const deviceToken = document.querySelector("#device-token");
const preferredSite = document.querySelector("#preferred-site");

function activeProfile() {
  return state.profiles.find(profile => profile.id === state.activeProfileId) || state.profiles[0];
}

function renderProfileOptions() {
  profileSelect.replaceChildren(...state.profiles.map(profile => {
    const option = document.createElement("option");
    option.value = profile.id;
    option.textContent = `${profile.name} · ${profile.serverURL}`;
    return option;
  }));
  profileSelect.value = state.activeProfileId;
}

function renderForm() {
  const profile = activeProfile();
  state.activeProfileId = profile.id;
  renderProfileOptions();
  profileName.value = profile.name;
  serverURL.value = profile.serverURL;
  deviceToken.value = profile.deviceToken;
  preferredSite.value = state.preferredSite;
  document.querySelector("#delete-profile").disabled = state.profiles.length <= 1;
  if (profile.serverURL === "http://127.0.0.1:8080") {
    showResult("旧默认端口 8080，请改为 59188 后保存", "error");
  }
}

function formProfile() {
  return {
    id: state.activeProfileId,
    name: profileName.value.trim(),
    serverURL: serverURL.value.trim().replace(/\/+$/, ""),
    deviceToken: deviceToken.value.trim()
  };
}

function applyFormToState() {
  const profile = formProfile();
  const index = state.profiles.findIndex(item => item.id === profile.id);
  if (index >= 0) state.profiles[index] = profile;
  state.preferredSite = preferredSite.value;
  return profile;
}

function showResult(text, kind = "") {
  const saved = document.querySelector("#saved");
  saved.textContent = text;
  saved.dataset.kind = kind;
}

profileSelect.addEventListener("change", () => {
  state.activeProfileId = profileSelect.value;
  renderForm();
  showResult("已切换环境；上传将使用该环境保存的 Token");
});

document.querySelector("#new-profile").addEventListener("click", () => {
  const id = crypto.randomUUID();
  state.profiles.push({ id, name: `新环境 ${state.profiles.length + 1}`, serverURL: DEFAULT_SERVER_URL, deviceToken: "" });
  state.activeProfileId = id;
  renderForm();
  profileName.focus();
  profileName.select();
  showResult("请填写新环境地址及在该服务器创建的 Token");
});

document.querySelector("#delete-profile").addEventListener("click", async () => {
  if (state.profiles.length <= 1) return;
  const profile = activeProfile();
  if (!confirm(`确认删除环境“${profile.name}”？`)) return;
  state.profiles = state.profiles.filter(item => item.id !== profile.id);
  state.activeProfileId = state.profiles[0].id;
  await persistCollectorSettings(state);
  renderForm();
  showResult("环境已删除", "success");
});

document.querySelector("#settings-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const profile = applyFormToState();
  if (!profile.name || !profile.serverURL) {
    showResult("环境名称和服务地址不能为空", "error");
    return;
  }
  state = { ...state, ...(await persistCollectorSettings(state)) };
  renderForm();
  showResult("当前环境已保存并启用", "success");
});

document.querySelector("#test-connection").addEventListener("click", async () => {
  const button = document.querySelector("#test-connection");
  const profile = applyFormToState();
  if (!profile.serverURL || !profile.deviceToken) {
    showResult("请先填写服务地址和该服务器创建的设备 Token", "error");
    return;
  }
  button.disabled = true;
  showResult(`正在测试“${profile.name}”…`);
  try {
    const response = await fetch(`${profile.serverURL}/api/pdd-collector/device`, {
      headers: { "Authorization": `Bearer ${profile.deviceToken}` }
    });
    const text = await response.text();
    let body = {};
    try { body = text ? JSON.parse(text) : {}; } catch { body = { message: text }; }
    if (!response.ok) throw new Error(body?.detail || body?.message || `HTTP ${response.status}`);
    state = { ...state, ...(await persistCollectorSettings(state)) };
    renderForm();
    showResult(`连接成功：${body.device_name || body.device_id}`, "success");
  } catch (error) {
    const reason = error instanceof TypeError
      ? "无法连接服务。请检查 IP、端口、防火墙及 Docker 监听地址"
      : `${error.message}；请确认 Token 是在当前所选服务器创建的`;
    showResult(`连接失败：${reason}`, "error");
  } finally {
    button.disabled = false;
  }
});

renderForm();
