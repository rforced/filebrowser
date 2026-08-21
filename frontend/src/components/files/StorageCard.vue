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
import { useUsageStore } from "@/stores/usage";
import { filesize } from "@/utils";
import Card from "@/components/ui/Card.vue";
import Gauge from "@/components/ui/Gauge.vue";

const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const usageStore = useUsageStore();

const DEFAULT = {
  used: "0 B",
  available: "0 B",
  usedPercentage: 0,
};

const usage = reactive({ ...DEFAULT });

let controller = new AbortController();

/*
 * ZFS applies frees at transaction-group sync, so the statfs behind /api/usage
 * still reports the old number for a moment after a delete. Measured on a
 * Horizon FileSystem (zfs_txg_timeout=5): the space reappears in a single step
 * 3.3-4.5s after the unlink, identically for one 5 GiB file and for 2000 small
 * ones. It is a txg boundary rather than a drain, so one late read catches all
 * of it — and watching for the number to move would not work anyway, since a
 * solver writing output moves it every txg regardless. Without this a 40 GB
 * clean looks like it did nothing.
 */
const SETTLE_DELAY = 8000;

let settleTimer: ReturnType<typeof setTimeout> | undefined;

const fetchUsage = async () => {
  const path = route.path.endsWith("/") ? route.path : route.path + "/";

  controller.abort();
  controller = new AbortController();
  const mine = controller;

  try {
    const result = await api.usage(path, mine.signal);

    Object.assign(usage, {
      used: filesize(result.used),
      available: filesize(Math.max(0, result.total - result.used)),
      usedPercentage: result.total
        ? Math.round((result.used / result.total) * 100)
        : 0,
    });
  } catch {
    // Our own abort means a newer read is already on its way. Falling back to
    // DEFAULT here would flash 0 B under the gauge before it lands.
    if (!mine.signal.aborted) Object.assign(usage, DEFAULT);
  } finally {
    if (!mine.signal.aborted) scheduleSettle();
  }
};

/*
 * Re-read once the filesystem has had time to catch up, whether this card was
 * mounted when the change happened or navigated to afterwards — deleting a file
 * lands you in its parent directory, which mounts this card fresh. Self-
 * terminating: the follow-up read finds the window elapsed and arms nothing.
 */
const scheduleSettle = () => {
  clearTimeout(settleTimer);

  const since = Date.now() - usageStore.changedAt;
  if (since >= SETTLE_DELAY) return;

  settleTimer = setTimeout(fetchUsage, SETTLE_DELAY - since);
};

const openUsage = () => {
  router.push({ path: route.path, query: { ...route.query, view: "usage" } });
};

watch(
  () => route.path,
  () => fetchUsage(),
  { immediate: true }
);

// A delete in the directory already on screen moves nothing else this card
// watches, so the usage store's "bytes changed" signal is the only cue it gets.
watch(
  () => usageStore.revision,
  () => fetchUsage()
);

onUnmounted(() => {
  controller.abort();
  clearTimeout(settleTimer);
});
</script>
