<template>
  <select name="selectLanguage" v-on:change="change" :value="selected">
    <option v-for="(language, value) in locales" :key="value" :value="value">
      {{ language }}
    </option>
  </select>
</template>

<script>
import { markRaw } from "vue";

export default {
  name: "languages",
  props: ["locale"],
  data() {
    const dataObj = {};
    const locales = {
      de: "Deutsch",
      en: "English",
      hi: "हिन्दी",
      it: "Italiano",
      ja: "日本語",
      "zh-cn": "中文 (简体)",
    };

    // Vue3 reactivity breaks with this configuration
    // so we need to use markRaw as a workaround
    // https://github.com/vuejs/core/issues/3024
    Object.defineProperty(dataObj, "locales", {
      value: markRaw(locales),
      configurable: false,
      writable: false,
    });

    return dataObj;
  },
  computed: {
    // Accounts left on a locale we no longer ship fall back to English, which
    // is what vue-i18n renders for them anyway. Without this the select has no
    // matching option and renders blank.
    selected() {
      return this.locales[this.locale] ? this.locale : "en";
    },
  },
  methods: {
    change(event) {
      this.$emit("update:locale", event.target.value);
    },
  },
};
</script>
