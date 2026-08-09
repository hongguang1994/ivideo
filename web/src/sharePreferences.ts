export type SharePreferences = {
  defaultProvider: string;
  defaultCategory: string;
  targetFolder: string;
  confirmDelete: boolean;
};

const STORAGE_KEY = "ivideo.share.preferences";

export const defaultSharePreferences: SharePreferences = {
  defaultProvider: "aliyun",
  defaultCategory: "",
  targetFolder: "ivideo",
  confirmDelete: true,
};

export function getSharePreferences(): SharePreferences {
  try {
    const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || "{}");
    return { ...defaultSharePreferences, ...stored };
  } catch {
    return { ...defaultSharePreferences };
  }
}

export function saveSharePreferences(preferences: SharePreferences) {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(preferences));
}
