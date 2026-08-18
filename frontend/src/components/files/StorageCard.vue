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
      says nothing about what is filling it. Styled to match Create Job in the
      sidebar below, since both are the same kind of thing — a full-width way
      out of this card into somewhere that answers a question.
    -->
    <button
      type="button"
      class="btn btn-menu btn-white btn-soft w-full min-w-0"
      @click="openUsage"
    >
      <i class="fa-solid fa-chart-pie fa-fw"></i>
      <span class="truncate">{{ t("files.usageViewAll") }}</span>
    </button>
  </Card>
</template>

<script setup lang="ts">
import { onUnmounted, reactive, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";

import { files as api } from "@/api";
import { filesize } from "@/utils";
import Card from "@/components/ui/Card.vue";
import Gauge from "@/components/ui/Gauge.vue";

const { t } = useI18n();
const route = useRoute();
const router = useRouter();

const DEFAULT = {
  used: "0 B",
  available: "0 B",
  usedPercentage: 0,
};

const usage = reactive({ ...DEFAULT });

let controller = new AbortController();

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
