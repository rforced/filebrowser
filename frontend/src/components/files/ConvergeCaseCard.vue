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
          :class="progressClass"
          :style="{ width: `${progressBounds.percent}%` }"
        ></div>
      </div>
    </div>

    <div v-if="chain.length > 0" class="flex flex-col gap-1.5">
      <span class="text-xs font-medium text-gray-600 dark:text-gray-300">
        {{ t("converge.runChain", { count: chain.length }) }}
      </span>
      <div class="flex flex-wrap gap-1.5 items-center">
        <template v-for="(leg, index) in chain" :key="leg.run.path">
          <i
            v-if="index > 0"
            class="fa-solid fa-angle-right text-[0.6rem] text-gray-400 dark:text-gray-500"
          ></i>
          <button
            type="button"
            class="inline-flex items-center gap-1.5 px-2 py-1 rounded-md text-xs border border-gray-200 dark:border-gray-700 enabled:hover:bg-gray-100 dark:enabled:hover:bg-gray-700 transition disabled:cursor-default"
            :disabled="!leg.run.logPath"
            :title="leg.run.name"
            @click="openLog(leg.run.logPath)"
          >
            <i
              class="fa-solid text-[0.6rem]"
              :class="[iconFor(leg.run.status), inkFor(leg.run.status)]"
            ></i>
            <span class="font-medium text-gray-900 dark:text-gray-100">
              {{ runLabel(leg.run) }}
            </span>
            <span
              v-if="legRange(leg)"
              class="text-gray-500 dark:text-gray-400 tabular-nums"
            >
              {{ legRange(leg) }}
            </span>
            <span class="text-gray-400 dark:text-gray-500 tabular-nums">
              {{ filesize(leg.run.size) }}
            </span>
          </button>
        </template>
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

    <OutputSizeStrip
      v-if="postFiles.length > 1"
      :items="postFiles"
      :start="summary.progress?.start"
      :end="summary.progress?.end"
      :unit="timeUnit"
      @open="openOutput"
    />

    <RestartChooser
      v-if="summary.restarts.length > 0"
      :restarts="summary.restarts"
      :unit="timeUnit"
    />
  </Card>
</template>

<script setup lang="ts">
import dayjs from "dayjs";
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";

import * as api from "@/api";
import type { ConvergeRun, ConvergeStatus, ConvergeSummary } from "@/api/files";
import Card from "@/components/ui/Card.vue";
import OutputSizeStrip from "@/components/files/OutputSizeStrip.vue";
import RestartChooser from "@/components/files/RestartChooser.vue";
import { useFileStore } from "@/stores/file";
import { filesize } from "@/utils";
import { formatOutValue } from "@/utils/convergeOut";
import {
  cachedConvergeSummary,
  invalidateConvergeSummary,
} from "@/utils/convergeSummaryCache";

const fileStore = useFileStore();
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
    loadOutputSizes();
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

const statusClasses: Record<ConvergeStatus, string> = {
  running: "bg-blue-100 text-blue-800 dark:bg-blue-900/60 dark:text-blue-200",
  completed:
    "bg-green-100 text-green-800 dark:bg-green-900/60 dark:text-green-200",
  needsRestart:
    "bg-indigo-100 text-indigo-800 dark:bg-indigo-900/60 dark:text-indigo-200",
  interrupted:
    "bg-amber-100 text-amber-800 dark:bg-amber-900/60 dark:text-amber-200",
  idle: "bg-gray-100 text-gray-700 dark:bg-gray-700 dark:text-gray-200",
};

const statusIcons: Record<ConvergeStatus, string> = {
  running: "fa-circle-notch fa-spin",
  completed: "fa-circle-check",
  needsRestart: "fa-rotate-right",
  interrupted: "fa-triangle-exclamation",
  idle: "fa-circle",
};

const statusInk: Record<ConvergeStatus, string> = {
  running: "text-blue-600 dark:text-blue-400",
  completed: "text-green-600 dark:text-green-400",
  needsRestart: "text-indigo-600 dark:text-indigo-400",
  interrupted: "text-amber-600 dark:text-amber-400",
  idle: "text-gray-400 dark:text-gray-500",
};

const classFor = (status?: ConvergeStatus) =>
  statusClasses[status ?? "idle"] ?? statusClasses.idle;
const iconFor = (status?: ConvergeStatus) =>
  statusIcons[status ?? "idle"] ?? statusIcons.idle;
const inkFor = (status?: ConvergeStatus) =>
  statusInk[status ?? "idle"] ?? statusInk.idle;

const statusClass = computed(() => classFor(summary.value?.status));
const statusIcon = computed(() => iconFor(summary.value?.status));

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

const progressClass = computed(() => {
  switch (summary.value?.status) {
    case "running":
      return "bg-blue-500";
    case "needsRestart":
      return "bg-indigo-500";
    default:
      return "bg-gray-400 dark:bg-gray-500";
  }
});

const newestRestart = computed(() => summary.value?.restarts[0] ?? null);

const timeUnit = computed(() =>
  summary.value?.progress?.unit === "deg" ? "deg" : "s"
);

// The newest leg is the one still being written, so its output directory is
// the profile worth showing.
const newestRun = computed(() => summary.value?.runs[0] ?? null);

const postFiles = ref<{ name: string; size: number }[]>([]);
const outputDir = ref("");

// Sizes come from a plain directory listing — post file size tracks cell count
// closely, so the profile costs one stat per file and opens nothing.
const loadOutputSizes = async () => {
  postFiles.value = [];
  outputDir.value = "";

  const run = newestRun.value;
  const hasPost = summary.value?.groups.some(
    (g) => g.kind === "post" && g.count > 1
  );
  if (!run || !hasPost) return;

  const dir = `${run.path}/output`;
  try {
    const res = await api.files.fetch(dir);
    if (!res.isDir) return;
    postFiles.value = (res.items ?? []).map((item) => ({
      name: item.name,
      size: item.size,
    }));
    outputDir.value = dir;
  } catch {
    // No output subdirectory (older flat layouts) simply means no profile.
  }
};

const openOutput = (name: string) => {
  if (outputDir.value) {
    router.push({ path: `/files${outputDir.value}/${name}` });
  }
};

// The chain reads oldest to newest, the direction the solve ran, while the API
// reports runs newest-first. Each leg only records where it stopped, so the one
// before it supplies where it started; the deck's start_time opens the chain.
// A leg whose log could not be read leaves the next one's start unknown rather
// than inventing a join.
const chain = computed(() => {
  const runs = summary.value?.runs ?? [];
  if (runs.length < 2) return [];

  let start = summary.value?.progress?.start;

  return runs
    .slice()
    .reverse()
    .map((run) => {
      const leg = { run, start, end: run.end };
      start = run.end;
      return leg;
    });
});

type ChainLeg = (typeof chain.value)[number];

const runLabel = (run: ConvergeRun) =>
  run.name.replace(/^outputs_/i, "") || run.name;

const legRange = (leg: ChainLeg) => {
  if (leg.end === undefined) return "";

  const unit = summary.value?.progress?.unit ?? "";
  const end = `${formatOutValue(leg.end)}${unit && ` ${unit}`}`;
  return leg.start === undefined ? end : `${formatOutValue(leg.start)}–${end}`;
};

const fromNow = (iso: string) => dayjs(iso).fromNow();

const openLog = (logPath?: string) => {
  if (logPath) {
    router.push({ path: `/files${logPath}` });
  }
};

const viewLog = () => openLog(summary.value?.logPath);
</script>
