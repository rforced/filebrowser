<template>
  <footer
    data-component="footer"
    class="flex gap-4 md:gap-8 flex-wrap sm:flex-nowrap items-center justify-center bg-gray-100 dark:bg-gray-900 border-t dark:border-gray-700 px-3 md:px-6 py-3 z-30"
  >
    <div
      class="grow flex flex-wrap gap-x-2 gap-y-1 items-center text-gray-700 dark:text-gray-300 text-sm"
    >
      <span class="whitespace-nowrap"
        >&copy; {{ year }} Convergent Science Inc</span
      >

      <template v-if="version">
        <span aria-hidden="true">|</span>
        <span class="whitespace-nowrap text-gray-500 dark:text-gray-400">{{
          version
        }}</span>
      </template>

      <span aria-hidden="true">|</span>
      <button
        type="button"
        class="text-blue-600 dark:text-gray-100 hover:underline whitespace-nowrap"
        @click="layoutStore.showHover('help')"
      >
        {{ t("sidebar.help") }}
      </button>
    </div>

    <theme-switch />
  </footer>
</template>

<script setup lang="ts">
import { useI18n } from "vue-i18n";
import { useLayoutStore } from "@/stores/layout";
import { version } from "@/utils/constants";
import ThemeSwitch from "@/components/ui/ThemeSwitch.vue";

const { t } = useI18n();
const layoutStore = useLayoutStore();
const year = new Date().getFullYear();
</script>
