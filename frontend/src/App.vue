<template>
  <div>
    <h1 class="sr-only">{{ name }}</h1>
    <router-view></router-view>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, watch } from "vue";
import { useI18n } from "vue-i18n";
import { setHtmlLocale } from "./i18n";
import {
  applyThemePreference,
  getThemePreference,
  watchSystemTheme,
} from "./utils/theme";
import { name } from "./utils/constants";

const { locale } = useI18n();

let unwatchTheme: (() => void) | undefined;

onMounted(() => {
  applyThemePreference(getThemePreference());
  unwatchTheme = watchSystemTheme();

  setHtmlLocale(locale.value);

  const loading = document.getElementById("loading");
  loading?.classList.add("done");

  setTimeout(function () {
    loading?.parentNode?.removeChild(loading);
  }, 200);
});

watch(locale, (newValue) => {
  newValue && setHtmlLocale(newValue);
});

onBeforeUnmount(() => unwatchTheme?.());
</script>
