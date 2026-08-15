<template>
  <div class="flex flex-col">
    <div class="px-6 py-4 flex flex-col gap-3">
      <p>{{ t("prompts.deleteUser") }}</p>
    </div>

    <div
      class="flex flex-wrap justify-end items-center gap-2 px-6 py-4 bg-gray-50 dark:bg-gray-900 rounded-b-lg"
    >
      <button
        id="focus-prompt"
        class="btn btn-white btn-soft"
        @click="layoutStore.closeHovers"
        :aria-label="t('buttons.cancel')"
        :title="t('buttons.cancel')"
        tabindex="1"
      >
        {{ t("buttons.cancel") }}
      </button>
      <button
        class="btn btn-blue btn-soft"
        @click="layoutStore.currentPrompt?.confirm"
        tabindex="2"
      >
        {{ t("buttons.delete") }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useLayoutStore } from "@/stores/layout";
import { useI18n } from "vue-i18n";

const layoutStore = useLayoutStore();

const { t } = useI18n();

// const emit = defineEmits<{
//   (e: "confirm"): void;
// }>();
</script>
