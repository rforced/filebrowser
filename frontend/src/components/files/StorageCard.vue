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
          {{ t("files.storageOf", { total: usage.total }) }}
        </div>
      </div>
    </div>
  </Card>
</template>

<script setup lang="ts">
import { onUnmounted, reactive, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";

import { files as api } from "@/api";
import { filesize } from "@/utils";
import Card from "@/components/ui/Card.vue";
import Gauge from "@/components/ui/Gauge.vue";

const { t } = useI18n();
const route = useRoute();

const DEFAULT = { used: "0 B", total: "0 B", usedPercentage: 0 };

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
      total: filesize(result.total),
      usedPercentage: Math.round((result.used / result.total) * 100),
    });
  } catch {
    // A failed usage probe is not worth a toast — the card just shows zeroes.
    // The listing itself is what matters and it reports its own errors.
    Object.assign(usage, DEFAULT);
  }
};

watch(
  () => route.path,
  () => fetchUsage(),
  { immediate: true }
);

onUnmounted(() => controller.abort());
</script>
