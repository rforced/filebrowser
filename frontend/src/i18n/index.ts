import dayjs from "dayjs";
import { createI18n } from "vue-i18n";

import("dayjs/locale/de");
import("dayjs/locale/en");
import("dayjs/locale/hi");
import("dayjs/locale/it");
import("dayjs/locale/ja");
import("dayjs/locale/zh-cn");

// All i18n resources specified in the plugin `include` option can be loaded
// at once using the import syntax
import messages from "@intlify/unplugin-vue-i18n/messages";

export function detectLocale() {
  // locale is an RFC 5646 language tag
  // https://developer.mozilla.org/en-US/docs/Web/API/Navigator/language
  let locale = navigator.language.toLowerCase();
  switch (true) {
    case /^de\b/.test(locale):
      locale = "de";
      break;
    case /^en\b/.test(locale):
      locale = "en";
      break;
    case /^hi\b/.test(locale):
      locale = "hi";
      break;
    case /^it\b/.test(locale):
      locale = "it";
      break;
    case /^ja\b/.test(locale):
      locale = "ja";
      break;
    // Simplified Chinese is the only Chinese locale we ship, so every zh-*
    // tag (Traditional included) resolves to it rather than falling back to
    // English.
    case /^zh\b/.test(locale):
      locale = "zh-cn";
      break;

    default:
      locale = "en";
  }

  return locale;
}

// TODO: was this really necessary?
// function removeEmpty(obj: Record<string, any>): void {
//   Object.keys(obj)
//     .filter((k) => obj[k] !== null && obj[k] !== undefined && obj[k] !== "") // Remove undef. and null and empty.string.
//     .reduce(
//       (newObj, k) =>
//         typeof obj[k] === "object"
//           ? Object.assign(newObj, { [k]: removeEmpty(obj[k]) }) // Recurse.
//           : Object.assign(newObj, { [k]: obj[k] }), // Copy value.
//       {}
//     );
// }

// None of the locales we ship are right-to-left. The plumbing stays so that
// adding one back (Arabic, Hebrew, ...) only needs an entry here.
export const rtlLanguages: string[] = [];

export const i18n = createI18n({
  locale: detectLocale(),
  fallbackLocale: "en",
  messages,
  legacy: false,
});

export const isRtl = (locale?: string) => {
  return rtlLanguages.includes(locale || i18n.global.locale.value);
};

export function setLocale(locale: string) {
  dayjs.locale(locale);
  i18n.global.locale.value = locale;
}

export function setHtmlLocale(locale: string) {
  const html = document.documentElement;
  html.lang = locale;
  if (isRtl(locale)) html.dir = "rtl";
  else html.dir = "ltr";
}

export default i18n;
