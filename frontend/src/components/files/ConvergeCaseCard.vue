<template>
  <Card v-if="summary?.isCase" class="p-4 flex flex-col gap-3">
    <div class="flex flex-wrap gap-x-4 gap-y-2 items-center">
      <span
        class="inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full text-xs font-medium"
        :class="statusClass"
      >
        <i class="fa-solid text-[0.65rem]" :class="statusIcon"></i>
        {{ t(`converge.status.${summary.status}`) }}
      </span>

      <span class="font-medium text-gray-900 dark:text-gray-100">
        {{ t("converge.caseTitle") }}
      </span>

      <span
        v-if="jobLine"
        class="text-sm text-gray-600 dark:text-gray-300 truncate"
      >
        {{ jobLine }}
      </span>

      <span
        v-if="summary.lastActivity && summary.status !== 'idle'"
        class="text-xs text-gray-500 dark:text-gray-400"
      >
        {{
          t("converge.lastActivity", { time: fromNow(summary.lastActivity) })
        }}
      </span>

      <span class="flex-1"></span>

      <button
        v-if="summary.logPath"
        type="button"
        class="btn btn-sm btn-white btn-soft"
        @click="viewLog"
      >
        <i class="fa-solid fa-file-lines mr-1"></i>
        {{ t("converge.viewLog") }}
      </button>

      <button
        v-if="authStore.user?.perm.delete && summary.count > 0"
        type="button"
        class="btn btn-sm btn-white btn-soft"
        @click="layoutStore.showHover('converge-clean')"
      >
        <i class="fa-solid fa-broom mr-1"></i>
        {{ t("converge.cleanOutput") }}
      </button>
    </div>

    <div v-if="progressBounds" class="flex flex-col gap-1">
      <div
        class="flex justify-between text-xs text-gray-600 dark:text-gray-300 tabular-nums"
      >
        <span>{{ t("converge.simTime") }}</span>
        <span>
          {{ formatOutValue(progressBounds.current) }} /
          {{ formatOutValue(progressBounds.end) }}
          {{ summary.progress?.unit }}
        </span>
      </div>
      <div
        class="h-1.5 rounded-full bg-gray-200 dark:bg-gray-700 overflow-hidden"
      >
        <div
          class="h-full rounded-full transition-all"
          :class="
            summary.status === 'running'
              ? 'bg-blue-500'
              : 'bg-gray-400 dark:bg-gray-500'
          "
          :style="{ width: `${progressBounds.percent}%` }"
        ></div>
      </div>
    </div>

    <div
      v-if="summary.groups.length > 0"
      class="flex flex-wrap gap-x-4 gap-y-1 text-xs text-gray-600 dark:text-gray-300"
    >
      <span class="font-medium">
        {{ t("converge.outputFootprint") }}: {{ filesize(summary.size) }}
      </span>
      <span
        v-for="group in summary.groups"
        :key="group.kind"
        class="tabular-nums"
      >
        {{ t(`prompts.convergeKinds.${group.kind}`) }} · {{ group.count }}
        <span class="text-gray-400 dark:text-gray-500">
          ({{ filesize(group.size) }})
        </span>
      </span>
      <span v-if="newestRestart" class="tabular-nums">
        {{ t("converge.newestRestart") }} · {{ newestRestart.name }} ·
        {{ fromNow(newestRestart.modified) }}
      </span>
    </div>
  </Card>
</template>

<script setup lang="ts">
import dayjs from "dayjs";
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";

import type { ConvergeSummary } from "@/api/files";
import Card from "@/components/ui/Card.vue";
import { useAuthStore } from "@/stores/auth";
import { useFileStore } from "@/stores/file";
import { useLayoutStore } from "@/stores/layout";
import { filesize } from "@/utils";
import { formatOutValue } from "@/utils/convergeOut";
import {
  cachedConvergeSummary,
  invalidateConvergeSummary,
} from "@/utils/convergeSummaryCache";

const authStore = useAuthStore();
const fileStore = useFileStore();
const layoutStore = useLayoutStore();
const route = useRoute();
const router = useRouter();
const { t } = useI18n();

const summary = ref<ConvergeSummary | null>(null);

let controller = new AbortController();

const load = async () => {
  summary.value = null;

  controller.abort();
  controller = new AbortController();

  try {
    summary.value = await cachedConvergeSummary(route.path, controller.signal);
  } catch {}
};

onMounted(load);
watch(() => route.path, load);
watch(
  () => fileStore.reload,
  (reloading) => {
    if (reloading) {
      invalidateConvergeSummary(route.path);
      load();
    }
  }
);

const statusClass = computed(() => {
  switch (summary.value?.status) {
    case "running":
      return "bg-blue-100 text-blue-800 dark:bg-blue-900/60 dark:text-blue-200";
    case "completed":
      return "bg-green-100 text-green-800 dark:bg-green-900/60 dark:text-green-200";
    case "interrupted":
      return "bg-amber-100 text-amber-800 dark:bg-amber-900/60 dark:text-amber-200";
    default:
      return "bg-gray-100 text-gray-700 dark:bg-gray-700 dark:text-gray-200";
  }
});

const statusIcon = computed(() => {
  switch (summary.value?.status) {
    case "running":
      return "fa-circle-notch fa-spin";
    case "completed":
      return "fa-circle-check";
    case "interrupted":
      return "fa-triangle-exclamation";
    default:
      return "fa-circle";
  }
});

const jobLine = computed(() => {
  const job = summary.value?.job;
  if (!job) return "";

  const parts: string[] = [];
  if (job.name) parts.push(`${t("converge.job")} “${job.name}”`);
  if (job.appKey || job.appVersion) {
    parts.push([job.appKey, job.appVersion].filter(Boolean).join(" "));
  }
  if (job.nodesCount && job.coresPerNode) {
    parts.push(
      t("converge.nodes", { nodes: job.nodesCount, cores: job.coresPerNode })
    );
  }
  return parts.join(" · ");
});

const progressBounds = computed(() => {
  const progress = summary.value?.progress;
  if (
    !progress ||
    progress.start === undefined ||
    progress.end === undefined ||
    progress.end <= progress.start
  ) {
    return null;
  }

  const percent = Math.min(
    100,
    Math.max(
      0,
      ((progress.current - progress.start) / (progress.end - progress.start)) *
        100
    )
  );
  return { current: progress.current, end: progress.end, percent };
});

const newestRestart = computed(() => summary.value?.restarts[0] ?? null);

const fromNow = (iso: string) => dayjs(iso).fromNow();

const viewLog = () => {
  if (summary.value?.logPath) {
    router.push({ path: `/files${summary.value.logPath}` });
  }
};
</script>
