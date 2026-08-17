<template>
  <Card
    v-for="job in extractStore.jobs"
    :key="job.id"
    class="flex flex-col overflow-hidden w-full pointer-events-auto"
  >
    <div class="flex items-start gap-3 px-4 py-3">
      <i
        class="fa-solid fa-box-open mt-0.5 shrink-0 text-gray-500 dark:text-gray-400"
      ></i>

      <div class="flex-1 min-w-0 flex flex-col gap-1">
        <h2
          class="text-sm font-medium text-gray-900 dark:text-gray-100 truncate"
        >
          {{ t("prompts.extractingName", { name: job.name }) }}
        </h2>

        <div
          class="text-xs text-gray-600 dark:text-gray-300 flex flex-wrap gap-x-3 gap-y-0.5"
        >
          <span v-if="job.current > 0" class="tabular-nums">
            {{ job.current }} {{ t("prompts.extractFiles") }}
          </span>
          <span class="truncate min-w-0">{{ job.currentFile }}</span>
        </div>
      </div>

      <button
        v-tooltip="t('upload.abortUpload')"
        type="button"
        class="w-7 h-7 shrink-0 flex items-center justify-center rounded-md text-gray-500 dark:text-gray-400 hover:text-red-600 dark:hover:text-red-300 hover:bg-gray-200 dark:hover:bg-gray-700 transition"
        :aria-label="t('buttons.cancel')"
        @click="cancel(job.id)"
      >
        <i class="fa-solid fa-circle-xmark"></i>
      </button>
    </div>

    <div class="h-1 bg-gray-200 dark:bg-gray-700 overflow-hidden">
      <div
        v-if="job.total > 0"
        class="h-full bg-blue-500 dark:bg-teal-600 transition-[width] duration-200"
        :style="{ width: (job.current / job.total) * 100 + '%' }"
      ></div>
      <div
        v-else
        class="h-full w-1/3 bg-blue-500 dark:bg-teal-600 animate-indeterminate"
      ></div>
    </div>
  </Card>
</template>

<script setup lang="ts">
import { useI18n } from "vue-i18n";

import { useExtractStore } from "@/stores/extract";
import Card from "@/components/ui/Card.vue";

const { t } = useI18n();
const extractStore = useExtractStore();

const cancel = (id: number) => {
  if (confirm(t("upload.abortUpload"))) {
    extractStore.cancel(id);
  }
};
</script>

<style scoped>
@keyframes indeterminate {
  0% {
    transform: translateX(-100%);
  }
  100% {
    transform: translateX(300%);
  }
}

.animate-indeterminate {
  animation: indeterminate 1.4s ease-in-out infinite;
}
</style>
