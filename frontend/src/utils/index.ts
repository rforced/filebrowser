import { partial } from "filesize";

/**
 * Formats filesize as KiB/MiB/...
 */
export const filesize = partial({ base: 2 });

/**
 * URL-safe base64 of a string's UTF-8 bytes, without padding.
 *
 * Used to derive stable list keys from filenames, which may contain characters
 * outside Latin-1 that plain `btoa` would reject.
 */
export const base64url = (value: string): string => {
  const bytes = new TextEncoder().encode(value);
  let binary = "";
  for (const byte of bytes) {
    binary += String.fromCharCode(byte);
  }
  return btoa(binary)
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "");
};

/**
 * Structural clone of JSON-shaped data.
 *
 * `structuredClone` is not usable here: it throws DataCloneError on Vue's
 * reactive proxies, and callers routinely pass objects spread out of a store.
 * Reading properties normally goes through the proxy instead. Unlike a JSON
 * round-trip this also keeps explicit `undefined` values, so they still
 * overwrite the previous value when the result is merged.
 */
export const deepClone = <T>(value: T): T => {
  if (Array.isArray(value)) {
    return value.map(deepClone) as T;
  }

  if (value !== null && typeof value === "object") {
    return Object.fromEntries(
      Object.entries(value).map(([key, val]) => [key, deepClone(val)])
    ) as T;
  }

  return value;
};

export const vClickOutside = {
  created(el: HTMLElement, binding: any) {
    el.clickOutsideEvent = (event: Event) => {
      const target = event.target;

      if (target instanceof Node) {
        if (!el.contains(target)) {
          binding.value(event);
        }
      }
    };

    document.addEventListener("click", el.clickOutsideEvent);
  },

  unmounted(el: HTMLElement) {
    if (el.clickOutsideEvent) {
      document.removeEventListener("click", el.clickOutsideEvent);
    }
  },
};
