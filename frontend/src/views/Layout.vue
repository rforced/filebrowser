<template>
  <div class="flex flex-col min-h-screen">
    <div
      v-if="uploadStore.totalBytes"
      class="fixed top-0 left-0 w-full h-[3px] z-[9999]"
    >
      <div
        class="h-full bg-blue-400 dark:bg-teal-500 transition-[width] duration-200"
        :style="{ width: sentPercent + '%' }"
      ></div>
    </div>

    <app-header />

    <div class="grow min-w-0 bg-gray-50 dark:bg-gray-900">
      <router-view />
    </div>

    <app-footer />

    <prompts />
    <upload-files />
    <toasts />
  </div>
</template>

<script setup lang="ts">
import { computed, watch } from "vue";
import { useRoute } from "vue-router";

import { useLayoutStore } from "@/stores/layout";
import { useFileStore } from "@/stores/file";
import { useUploadStore } from "@/stores/upload";

import AppHeader from "@/components/layout/AppHeader.vue";
import AppFooter from "@/components/layout/AppFooter.vue";
import Prompts from "@/components/prompts/Prompts.vue";
import UploadFiles from "@/components/prompts/UploadFiles.vue";
import Toasts from "@/components/ui/Toasts.vue";

const layoutStore = useLayoutStore();
const fileStore = useFileStore();
const uploadStore = useUploadStore();
const route = useRoute();

const sentPercent = computed(() =>
  ((uploadStore.sentBytes / uploadStore.totalBytes) * 100).toFixed(2)
);

watch(route, () => {
  fileStore.selected = [];
  fileStore.multiple = false;
  if (layoutStore.currentPromptName !== "success") {
    layoutStore.closeHovers();
  }
});
</script>
