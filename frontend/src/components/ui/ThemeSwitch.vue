<template>
  <div
    class="flex items-center gap-0.5 rounded-lg bg-gray-200 dark:bg-gray-700 p-1"
    role="radiogroup"
    :aria-label="t('settings.themes.title')"
  >
    <button
      v-for="option in OPTIONS"
      :key="option.value"
      v-tooltip="t(option.label)"
      type="button"
      role="radio"
      :aria-checked="preference === option.value"
      :aria-label="t(option.label)"
      class="rounded-md p-1.5 transition"
      :class="
        preference === option.value
          ? 'bg-white dark:bg-gray-500 shadow-xs text-gray-900 dark:text-white'
          : 'text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200'
      "
      @click="select(option.value)"
    >
      <svg class="w-4 h-4" fill="currentColor" :viewBox="option.viewBox">
        <path :d="option.path" fill-rule="evenodd" clip-rule="evenodd"></path>
      </svg>
    </button>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import {
  getThemePreference,
  setThemePreference,
  watchSystemTheme,
} from "@/utils/theme";

const { t } = useI18n();

const preference = ref<ThemePreference>(getThemePreference());

const OPTIONS: {
  value: ThemePreference;
  label: string;
  viewBox: string;
  path: string;
}[] = [
  {
    value: "light",
    label: "settings.themes.light",
    viewBox: "0 0 20 20",
    path: "M10 2a1 1 0 011 1v1a1 1 0 11-2 0V3a1 1 0 011-1zm4 8a4 4 0 11-8 0 4 4 0 018 0zm-.464 4.95l.707.707a1 1 0 001.414-1.414l-.707-.707a1 1 0 00-1.414 1.414zm2.12-10.607a1 1 0 010 1.414l-.706.707a1 1 0 11-1.414-1.414l.707-.707a1 1 0 011.414 0zM17 11a1 1 0 100-2h-1a1 1 0 100 2h1zm-7 4a1 1 0 011 1v1a1 1 0 11-2 0v-1a1 1 0 011-1zM5.05 6.464A1 1 0 106.465 5.05l-.708-.707a1 1 0 00-1.414 1.414l.707.707zm1.414 8.486l-.707.707a1 1 0 01-1.414-1.414l.707-.707a1 1 0 011.414 1.414zM4 11a1 1 0 100-2H3a1 1 0 000 2h1z",
  },
  {
    value: "system",
    label: "settings.themes.default",
    viewBox: "0 0 24 24",
    path: "M4 6a2 2 0 012-2h12a2 2 0 012 2v7a2 2 0 01-2 2H6a2 2 0 01-2-2V6zm2 9h12l1 3H5l1-3z",
  },
  {
    value: "dark",
    label: "settings.themes.dark",
    viewBox: "0 0 20 20",
    path: "M17.293 13.293A8 8 0 016.707 2.707a8.001 8.001 0 1010.586 10.586z",
  },
];

const select = (value: ThemePreference) => {
  preference.value = value;
  setThemePreference(value);
};

let unwatch: (() => void) | undefined;

onMounted(() => {
  unwatch = watchSystemTheme();
});

onBeforeUnmount(() => unwatch?.());
</script>
