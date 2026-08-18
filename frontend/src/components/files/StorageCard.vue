<template>
  <Card class="flex flex-col gap-4 p-6">
    <h3 class="text-lg font-medium text-gray-900 dark:text-gray-100">
      {{ t("files.storage") }}
    </h3>

    <div class="flex gap-5 items-center">
      <Gauge :percentage="usage.usedPercentage" class="shrink-0" />

      <div class="flex flex-col gap-1 min-w-0">
        <div
          class="text-2xl font-bold text-gray-900 dark:text-gray-100 break-all"
        >
          {{ usage.used }}
        </div>
        <div class="text-sm text-gray-600 dark:text-gray-300">
          {{ t("files.storageAvailable", { available: usage.available }) }}
        </div>
      </div>
    </div>

    <!--
      The gauge on its own is a dead end: it says the disk is filling up and
      nothing about what is filling it. These rows are the first step of the
      answer.

      They appear only once this directory has actually been scanned, because a
      scan is a recursive stat of the whole tree — far too expensive to fire off
      on every navigation just to populate a sidebar. The button below is the
      thing that starts one.
    -->
    <template v-if="top.length">
      <div class="flex flex-col gap-2">
        <h4
          class="text-xs font-medium uppercase tracking-wider text-gray-600 dark:text-gray-300"
        >
          {{ t("files.usageTopConsumers") }}
        </h4>

        <ul class="flex flex-col gap-1.5">
          <li
            v-for="entry in top"
            :key="entry.name"
            class="flex items-baseline justify-between gap-2 text-sm"
          >
            <span class="truncate text-gray-700 dark:text-gray-200">
              {{ entry.name }}
            </span>
            <span
              class="tabular-nums shrink-0 text-gray-600 dark:text-gray-300"
            >
              {{ filesize(entry.size) }}
            </span>
          </li>
        </ul>
      </div>
    </template>

    <button
      type="button"
      class="btn btn-blue btn-soft text-sm self-start"
      @click="openUsage"
    >
      {{ t("files.usageViewAll") }}
    </button>
  </Card>
</template>

<script setup lang="ts">
import { computed, onUnmounted, reactive, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";

import { files as api } from "@/api";
import { useFileStore } from "@/stores/file";
import { useUsageStore } from "@/stores/usage";
import { filesize } from "@/utils";
import Card from "@/components/ui/Card.vue";
import Gauge from "@/components/ui/Gauge.vue";

const { t } = useI18n();
const route = useRoute();
const router = useRouter();

const fileStore = useFileStore();
const usageStore = useUsageStore();

const DEFAULT = {
  used: "0 B",
  available: "0 B",
  usedPercentage: 0,
};

const usage = reactive({ ...DEFAULT });

let controller = new AbortController();

// Enough to point at the offender without turning the sidebar into a list.
const TOP_N = 4;

const top = computed(() => {
  const path = fileStore.req?.path;
  if (!path || !fileStore.req?.isDir) return [];

  const cached = usageStore.cachedBreakdown(path);
  if (!cached) return [];

  return cached.children.filter((c) => c.size > 0).slice(0, TOP_N);
});

const fetchUsage = async () => {
  const path = route.path.endsWith("/") ? route.path : route.path + "/";

  try {
    controller.abort();
    controller = new AbortController();

    const result = await api.usage(path, controller.signal);

    Object.assign(usage, {
      used: filesize(result.used),
      available: filesize(Math.max(0, result.total - result.used)),
      usedPercentage: result.total
        ? Math.round((result.used / result.total) * 100)
        : 0,
    });
  } catch {
    Object.assign(usage, DEFAULT);
  }
};

const openUsage = () => {
  router.push({ path: route.path, query: { ...route.query, view: "usage" } });
};

watch(
  () => route.path,
  () => fetchUsage(),
  { immediate: true }
);

onUnmounted(() => controller.abort());
</script>
