<template>
  <div
    data-component="help-box"
    class="flex gap-3 bg-blue-50 dark:bg-gray-800 rounded-lg p-4"
  >
    <i class="fa-solid fa-info-circle text-blue-400 dark:text-teal mt-0.5"></i>

    <div class="flex flex-col gap-2 text-sm min-w-0">
      <h3 class="font-medium text-blue-800 dark:text-gray-100">
        {{ t("help.needHelp") }}
      </h3>

      <div class="text-blue-700 dark:text-gray-300">
        {{ t("help.documentationBody") }}
      </div>

      <a
        :href="href"
        target="_blank"
        rel="noopener noreferrer"
        class="inline-block font-medium text-blue-700 dark:text-teal hover:underline"
      >
        <span>{{ t("help.viewDocumentation") }}</span>
        <i class="fa-solid fa-arrow-right ml-1"></i>
      </a>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { domain } from "@/utils/constants";

const props = withDefaults(defineProps<{ path?: string }>(), {
  path: "admin_guides/file_system_administration.html",
});
const { t } = useI18n();
const href = computed(() => `${domain}/docs/${props.path}`);
</script>
