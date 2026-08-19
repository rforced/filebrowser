import { cspNonce, recaptcha, recaptchaKey } from "@/utils/constants";

export const recaptchaEnabled = Boolean(recaptcha && recaptchaKey);

const scriptSrc = `https://www.google.com/recaptcha/enterprise.js?render=${recaptchaKey}`;

let script: HTMLScriptElement | null = null;
let mounts = 0;

export function mountRecaptcha() {
  if (!recaptchaEnabled) return;

  mounts++;
  if (script !== null) return;

  script = document.createElement("script");
  if (cspNonce) {
    script.nonce = cspNonce;
    script.setAttribute("nonce", cspNonce);
  }
  script.src = scriptSrc;
  document.head.appendChild(script);
}

export function unmountRecaptcha() {
  if (mounts === 0) return;

  mounts--;
  if (mounts > 0) return;

  script?.remove();
  script = null;
  document.querySelector(".grecaptcha-badge")?.remove();
  window.grecaptcha = undefined;
}

const loaded = () =>
  typeof window.grecaptcha !== "undefined" &&
  typeof window.grecaptcha.enterprise !== "undefined";

function whenLoaded(timeout: number): Promise<void> {
  return new Promise((resolve, reject) => {
    let settled = false;

    const timer = setTimeout(() => {
      settled = true;
      reject(new Error("reCAPTCHA script load timeout"));
    }, timeout);

    const check = () => {
      if (settled) return;

      if (loaded()) {
        settled = true;
        clearTimeout(timer);
        resolve();
        return;
      }

      setTimeout(check, 100);
    };

    check();
  });
}

export async function executeRecaptcha(
  action: string,
  timeout = 10000
): Promise<string> {
  if (!recaptchaEnabled) return "";

  await whenLoaded(timeout);
  return window.grecaptcha.enterprise.execute(recaptchaKey, { action });
}
