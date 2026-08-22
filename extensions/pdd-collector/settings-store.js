export const DEFAULT_SERVER_URL = "http://127.0.0.1:59188";

function cleanServerURL(value) {
  return String(value || "").trim().replace(/\/+$/, "");
}

function cleanProfile(profile, fallbackID) {
  return {
    id: String(profile?.id || fallbackID || crypto.randomUUID()),
    name: String(profile?.name || "本机").trim() || "未命名环境",
    serverURL: cleanServerURL(profile?.serverURL || DEFAULT_SERVER_URL),
    deviceToken: String(profile?.deviceToken || "").trim()
  };
}

export async function loadCollectorSettings(storage = chrome.storage.local) {
  const raw = await storage.get({
    collectorProfiles: [],
    activeCollectorProfileId: "",
    serverURL: DEFAULT_SERVER_URL,
    deviceToken: "",
    preferredSite: "pinduoduo"
  });
  let profiles = Array.isArray(raw.collectorProfiles)
    ? raw.collectorProfiles.map((profile, index) => cleanProfile(profile, `profile-${index + 1}`))
    : [];
  if (profiles.length === 0) {
    profiles = [cleanProfile({
      id: "default",
      name: raw.serverURL === DEFAULT_SERVER_URL ? "本机" : "原有环境",
      serverURL: raw.serverURL,
      deviceToken: raw.deviceToken
    }, "default")];
  }
  const activeProfile = profiles.find(profile => profile.id === raw.activeCollectorProfileId) || profiles[0];
  return {
    profiles,
    activeProfileId: activeProfile.id,
    activeProfile,
    preferredSite: raw.preferredSite === "yangkeduo" ? "yangkeduo" : "pinduoduo"
  };
}

export async function persistCollectorSettings(state, storage = chrome.storage.local) {
  const profiles = state.profiles.map((profile, index) => cleanProfile(profile, `profile-${index + 1}`));
  if (profiles.length === 0) throw new Error("至少需要保留一个服务环境");
  const activeProfile = profiles.find(profile => profile.id === state.activeProfileId) || profiles[0];
  await storage.set({
    collectorProfiles: profiles,
    activeCollectorProfileId: activeProfile.id,
    preferredSite: state.preferredSite === "yangkeduo" ? "yangkeduo" : "pinduoduo",
    serverURL: activeProfile.serverURL,
    deviceToken: activeProfile.deviceToken
  });
  return { profiles, activeProfileId: activeProfile.id, activeProfile };
}

export async function getActiveCollectorSettings(storage = chrome.storage.local) {
  const state = await loadCollectorSettings(storage);
  return {
    serverURL: state.activeProfile.serverURL,
    deviceToken: state.activeProfile.deviceToken,
    profileName: state.activeProfile.name,
    preferredSite: state.preferredSite
  };
}
