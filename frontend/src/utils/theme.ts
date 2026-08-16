import { theme as brandingTheme } from "./constants";

export const PREFERENCE_KEY = "color-theme-preference";

const RESOLVED_KEY = "color-theme";

const mediaQuery = () => window.matchMedia("(prefers-color-scheme: dark)");

export const getThemePreference = (): ThemePreference => {
  const stored = localStorage.getItem(PREFERENCE_KEY);
  if (stored === "light" || stored === "dark" || stored === "system") {
    return stored;
  }
  if (brandingTheme === "light" || brandingTheme === "dark") {
    return brandingTheme;
  }
  return "system";
};

export const resolveTheme = (preference: ThemePreference): ResolvedTheme => {
  if (preference === "system") {
    return mediaQuery().matches ? "dark" : "light";
  }
  return preference;
};

export const getTheme = (): ResolvedTheme =>
  document.documentElement.classList.contains("dark") ? "dark" : "light";

export const applyThemePreference = (preference: ThemePreference): void => {
  const resolved = resolveTheme(preference);
  document.documentElement.classList.toggle("dark", resolved === "dark");
  localStorage.setItem(RESOLVED_KEY, resolved);
  window.dispatchEvent(new CustomEvent("theme-changed"));
};

export const setThemePreference = (preference: ThemePreference): void => {
  localStorage.setItem(PREFERENCE_KEY, preference);
  applyThemePreference(preference);
};

export const watchSystemTheme = (): (() => void) => {
  const mq = mediaQuery();
  const onChange = () => {
    if (getThemePreference() === "system") {
      applyThemePreference("system");
    }
  };
  mq.addEventListener("change", onChange);
  return () => mq.removeEventListener("change", onChange);
};

export const getEditorTheme = () =>
  getTheme() === "dark" ? "ace/theme/twilight" : "ace/theme/chrome";
