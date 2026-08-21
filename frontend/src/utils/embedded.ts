import { authMethod, baseURL, domain } from "./constants";
import { saveToken } from "./auth";
import { applyEmbeddedTheme } from "./theme";

// Whether Horizon is really running inside a frame (the job platform embeds
// it). Auth handoff and theme sync only make sense here — they talk to the
// embedding page.
export const framed = window.self !== window.top;

// Presentation switch: the slimmed-down chrome. Follows framing, but can be
// forced with ?embed in a plain tab to work on the embedded styling.
export const embedded =
  framed || new URLSearchParams(window.location.search).has("embed");

const HANDOFF_REQUEST = "horizon:fm-handoff-request";
const HANDOFF_CODE = "horizon:fm-handoff-code";
const THEME_REQUEST = "horizon:fm-theme-request";
const THEME = "horizon:fm-theme";

function fromPlatform(event: MessageEvent): boolean {
  return event.origin === domain && event.source === window.parent;
}

/**
 * Ask Horizon for a single-use handoff code and exchange
 * it for a session, so an embedded user never sees a second login form.
 * Resolves false when not framed, when the parent stays silent, or when the
 * exchange is refused — callers fall back to the interactive login.
 */
export async function embeddedHandoff(): Promise<boolean> {
  if (!framed || authMethod !== "hook" || !domain) {
    return false;
  }

  const code = await requestHandoffCode();
  if (code === null) {
    return false;
  }

  try {
    const res = await fetch(`${baseURL}/api/handoff`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ code }),
    });

    if (res.status !== 200) {
      return false;
    }

    await saveToken(await res.text());
    return true;
  } catch {
    return false;
  }
}

/**
 * Follow the embedding platform's light/dark theme: ask once at startup and
 * apply every change it pushes afterwards.
 */
export function syncEmbeddedTheme(): void {
  if (!framed || !domain) {
    return;
  }

  window.addEventListener("message", (event: MessageEvent) => {
    if (!fromPlatform(event) || event.data?.type !== THEME) {
      return;
    }
    if (event.data.theme === "light" || event.data.theme === "dark") {
      applyEmbeddedTheme(event.data.theme);
    }
  });

  window.parent.postMessage({ type: THEME_REQUEST }, domain);
}

function requestHandoffCode(timeoutMs = 5000): Promise<string | null> {
  return new Promise((resolve) => {
    const finish = (code: string | null) => {
      window.clearTimeout(timer);
      window.removeEventListener("message", onMessage);
      resolve(code);
    };

    const timer = window.setTimeout(() => finish(null), timeoutMs);

    const onMessage = (event: MessageEvent) => {
      if (!fromPlatform(event) || event.data?.type !== HANDOFF_CODE) {
        return;
      }
      finish(typeof event.data.code === "string" ? event.data.code : null);
    };

    window.addEventListener("message", onMessage);
    window.parent.postMessage({ type: HANDOFF_REQUEST }, domain);
  });
}
