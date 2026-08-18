<template>
  <div class="flex flex-col gap-1">
    <p v-if="label" class="form-label">{{ label }}</p>
    <ul class="flex flex-col gap-1 text-sm">
      <li
        v-for="item in shown"
        :key="item.name"
        class="flex gap-2 items-center min-w-0"
      >
        <i
          class="fa-solid shrink-0 text-gray-400 dark:text-gray-500"
          :class="item.isDir ? 'fa-folder' : 'fa-file'"
        ></i>
        <span class="truncate font-medium text-gray-800 dark:text-gray-100">
          {{ item.name }}
        </span>
      </li>
      <li v-if="more > 0" class="text-xs text-gray-500 dark:text-gray-400">
        {{ t("prompts.andMoreItems", { count: more }) }}
      </li>
    </ul>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";

const MAX_SHOWN = 6;

const props = defineProps<{
  items: { name: string; isDir: boolean }[];
  label?: string;
}>();
const { t } = useI18n();

const shown = computed(() => props.items.slice(0, MAX_SHOWN));
const more = computed(() => Math.max(0, props.items.length - MAX_SHOWN));
</script>
