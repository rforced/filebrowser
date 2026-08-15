<template>
  <Card class="flex flex-col gap-4 p-6">
    <h3 class="text-lg font-medium text-gray-900 dark:text-gray-100">
      {{ t("prompts.fileInfo") }}
    </h3>

    <div v-if="!item" class="text-sm text-gray-600 dark:text-gray-300">
      {{
        selectedCount > 1
          ? t("prompts.filesSelected", selectedCount)
          : t("files.selectForDetails")
      }}
    </div>

    <div v-else class="flex flex-col gap-3">
      <div>
        <div class="text-sm text-gray-600 dark:text-gray-300">
          {{ t("prompts.displayName") }}
        </div>
        <div class="font-medium text-sm break-all">{{ item.name }}</div>
      </div>

      <div v-if="!item.isDir">
        <div class="text-sm text-gray-600 dark:text-gray-300">
          {{ t("prompts.size") }}
        </div>
        <div class="font-medium text-sm">{{ humanSize }}</div>
      </div>

      <div>
        <div class="text-sm text-gray-600 dark:text-gray-300">
          {{ t("prompts.lastModified") }}
        </div>
        <div class="font-medium text-sm">{{ humanTime }}</div>
      </div>

      <div>
        <div class="text-sm text-gray-600 dark:text-gray-300">
          {{ t("prompts.path") }}
        </div>
        <!-- Port of Horizon's components/prompt.blade.php -->
        <div
          class="bg-gray-50 dark:bg-gray-900 rounded-md flex gap-2 items-center justify-between p-2 mt-1"
        >
          <div class="font-mono text-xs grow break-all">{{ item.path }}</div>
          <button
            v-tooltip="t('buttons.copy')"
            type="button"
            class="text-gray-600 dark:text-gray-400 hover:text-gray-800 dark:hover:text-gray-200 shrink-0"
            :aria-label="t('buttons.copy')"
            @click="copyPath"
          >
            <i class="fa-solid" :class="copied ? 'fa-check' : 'fa-copy'"></i>
          </button>
        </div>
      </div>
    </div>
  </Card>
</template>

<script setup lang="ts">
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import dayjs from "dayjs";

import { useFileActions } from "@/composables/useFileActions";
import { useAuthStore } from "@/stores/auth";
import { filesize } from "@/utils";
import Card from "@/components/ui/Card.vue";

const { t } = useI18n();
const authStore = useAuthStore();
const { selectedItem: item, selectedCount } = useFileActions();

const copied = ref(false);

const humanSize = computed(() => (item.value ? filesize(item.value.size) : ""));

const humanTime = computed(() => {
  if (!item.value) return "";
  const time = dayjs(item.value.modified);
  return authStore.user?.dateFormat ? time.format("L LT") : time.fromNow();
});

const copyPath = async () => {
  if (!item.value) return;

  try {
    await navigator.clipboard.writeText(item.value.path);
    copied.value = true;
    window.setTimeout(() => (copied.value = false), 1500);
  } catch {}
};
</script>
