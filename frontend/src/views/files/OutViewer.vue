<template>
  <div
    id="out-viewer-container"
    class="fixed inset-0 z-9998 flex flex-col bg-gray-50 dark:bg-gray-900"
  >
    <header
      class="flex gap-3 items-center justify-between bg-gray-200 dark:bg-gray-900 border-b border-gray-300 dark:border-gray-700 p-3 md:px-6 shrink-0"
    >
      <div class="flex gap-2 items-center min-w-0">
        <button
          v-tooltip="t('buttons.close')"
          type="button"
          class="action shrink-0"
          :aria-label="t('buttons.close')"
          @click="close()"
        >
          <i class="fa-solid fa-xmark text-lg"></i>
        </button>

        <span class="font-medium text-gray-900 dark:text-gray-100 truncate">
          {{ fileStore.req?.name ?? "" }}
        </span>
      </div>

      <div class="flex gap-2 items-center shrink-0">
        <button
          v-if="canOpenAsText"
          type="button"
          class="btn btn-flex btn-white btn-soft"
          :aria-label="t('buttons.openAsText')"
          @click="openAsText"
        >
          <i class="fa-solid fa-file-lines"></i>
          <span class="hidden md:inline">{{ t("buttons.openAsText") }}</span>
        </button>

        <a
          v-if="authStore.user?.perm.download"
          :href="downloadUrl"
          target="_blank"
          class="btn btn-flex btn-blue btn-soft"
          :aria-label="t('buttons.download')"
        >
          <i class="fa-solid fa-download"></i>
          <span class="hidden md:inline">{{ t("buttons.download") }}</span>
        </a>
      </div>
    </header>

    <div
      v-if="loading"
      class="flex-1 flex items-center justify-center text-gray-500 dark:text-gray-400"
    >
      <i class="fa-solid fa-spinner fa-spin text-3xl"></i>
    </div>

    <div
      v-else-if="failure !== null"
      class="flex-1 flex flex-col items-center justify-center gap-3 p-6 text-center text-gray-600 dark:text-gray-300"
    >
      <i class="fa-solid fa-chart-line text-4xl opacity-60"></i>
      <p class="text-sm font-medium">{{ t(failure) }}</p>
      <a
        v-if="authStore.user?.perm.download"
        :href="downloadUrl"
        target="_blank"
        class="btn btn-flex btn-blue btn-soft"
      >
        <i class="fa-solid fa-download"></i>
        <span>{{ t("buttons.download") }}</span>
      </a>
    </div>

    <template v-else>
      <div
        class="flex flex-wrap gap-x-6 gap-y-2 items-center px-3 md:px-6 py-2 bg-white dark:bg-gray-800 border-b border-gray-200 dark:border-gray-700 shrink-0"
      >
        <label
          class="flex gap-2 items-center text-sm text-gray-700 dark:text-gray-200"
        >
          <span class="font-medium">{{ t("outPlot.xAxis") }}</span>
          <select
            v-model.number="xIndex"
            class="form-control !w-auto !py-1 text-sm"
            :aria-label="t('outPlot.xAxis')"
          >
            <option
              v-for="column in table.columns"
              :key="column.index"
              :value="column.index"
            >
              {{ columnLabel(column) }}
            </option>
          </select>
        </label>

        <label
          class="flex gap-2 items-center text-sm text-gray-700 dark:text-gray-200 cursor-pointer"
        >
          <input
            v-model="logScale"
            type="checkbox"
            class="form-checkbox cursor-pointer"
          />
          <span>{{ t("outPlot.logScale") }}</span>
        </label>

        <div
          v-if="chainCtx"
          role="group"
          class="inline-flex items-center rounded-md border border-gray-300 dark:border-gray-600 overflow-hidden text-sm"
        >
          <button
            type="button"
            class="px-2.5 py-1 transition"
            :class="
              !chainRequested
                ? 'bg-blue-600 text-white'
                : 'text-gray-700 dark:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-700'
            "
            :aria-pressed="!chainRequested"
            @click="setMode(false)"
          >
            {{ t("outPlot.thisRun") }}
          </button>
          <button
            type="button"
            class="px-2.5 py-1 transition border-l border-gray-300 dark:border-gray-600"
            :class="
              chainRequested
                ? 'bg-blue-600 text-white'
                : 'text-gray-700 dark:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-700'
            "
            :aria-pressed="chainRequested"
            @click="setMode(true)"
          >
            <i v-if="chainLoading" class="fa-solid fa-spinner fa-spin mr-1"></i>
            {{ t("outPlot.fullChain") }}
          </button>
        </div>

        <button
          type="button"
          class="btn btn-sm btn-flex btn-white btn-soft disabled:opacity-40"
          :disabled="chainRequested"
          :aria-label="following ? t('buttons.pause') : t('buttons.follow')"
          @click="toggleLive"
        >
          <i class="fa-solid" :class="following ? 'fa-pause' : 'fa-play'"></i>
          <span>{{
            following ? t("buttons.pause") : t("buttons.follow")
          }}</span>
        </button>

        <span
          v-if="following"
          class="inline-flex items-center gap-1.5 text-xs font-medium text-green-700 dark:text-green-400"
        >
          <i class="fa-solid fa-circle text-[0.5rem] animate-pulse"></i>
          {{ t("logView.live") }}
        </span>

        <span class="text-xs text-gray-500 dark:text-gray-400">
          {{ t("outPlot.rows", { count: table.rowCount }) }}
          <template v-if="chainActive">
            ·
            {{
              chainShown < chainTotal
                ? t("outPlot.chainPartial", {
                    shown: chainShown,
                    total: chainTotal,
                  })
                : t("outPlot.chainRuns", { count: chainShown })
            }}
          </template>
          <template v-if="chainActive && chainTrimmed > 0">
            · {{ t("outPlot.chainTrimmed", { count: chainTrimmed }) }}
          </template>
          <template v-if="decimated"> · {{ t("outPlot.decimated") }}</template>
          <template v-if="table.skippedRows > 0">
            · {{ t("outPlot.skippedRows", { count: table.skippedRows }) }}
          </template>
        </span>
      </div>

      <div
        class="flex gap-1.5 items-start px-3 md:px-6 py-2 bg-white dark:bg-gray-800 border-b border-gray-200 dark:border-gray-700 shrink-0"
      >
        <span
          class="text-sm font-medium text-gray-700 dark:text-gray-200 mr-1.5 shrink-0 py-0.5"
        >
          {{ t("outPlot.series") }}
        </span>

        <div
          class="flex flex-wrap gap-1.5 items-center min-w-0 max-h-24 overflow-y-auto"
        >
          <button
            v-for="column in yCandidates"
            :key="column.index"
            type="button"
            class="px-2 py-0.5 rounded-full text-xs font-medium border transition"
            :class="
              selected.includes(column.index)
                ? 'border-transparent text-white'
                : 'border-gray-300 dark:border-gray-600 text-gray-700 dark:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-700 disabled:opacity-40'
            "
            :style="
              selected.includes(column.index)
                ? { backgroundColor: seriesColor(column.index) }
                : {}
            "
            :disabled="
              !selected.includes(column.index) && selected.length >= maxSeries
            "
            :aria-pressed="selected.includes(column.index)"
            @click="toggleSeries(column.index)"
          >
            {{ columnLabel(column) }}
          </button>
        </div>

        <span
          v-if="selected.length >= maxSeries"
          class="text-xs text-gray-500 dark:text-gray-400 shrink-0 py-1"
        >
          {{ t("outPlot.maxSeries", { count: maxSeries }) }}
        </span>
      </div>

      <p
        v-if="logScale && logHidesValues"
        class="px-3 md:px-6 py-1.5 text-xs text-amber-700 dark:text-amber-400 bg-amber-50 dark:bg-amber-950/40 shrink-0"
      >
        {{ t("outPlot.logHidden") }}
      </p>

      <p
        v-if="chainNote"
        class="px-3 md:px-6 py-1.5 text-xs text-amber-700 dark:text-amber-400 bg-amber-50 dark:bg-amber-950/40 shrink-0"
      >
        {{ t(chainNote) }}
      </p>

      <p
        v-if="newerRun && !chainActive"
        class="px-3 md:px-6 py-1.5 text-xs text-blue-700 dark:text-blue-300 bg-blue-50 dark:bg-blue-950/40 shrink-0"
      >
        {{ t("outPlot.newerRun") }}
        <button
          type="button"
          class="underline font-medium ml-1"
          @click="openNewerRun"
        >
          {{ t("outPlot.openNewerRun", { name: runLabel(newerRun.runName) }) }}
        </button>
      </p>

      <div class="relative flex-1 min-h-0 m-3 md:m-4">
        <canvas ref="canvas"></canvas>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import {
  Chart,
  Decimation,
  Legend,
  LinearScale,
  LineController,
  LineElement,
  LogarithmicScale,
  PointElement,
  Tooltip,
  type ChartDataset,
} from "chart.js";
import {
  computed,
  onBeforeUnmount,
  onMounted,
  ref,
  shallowRef,
  watch,
} from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";

import { files as api } from "@/api";
import { useAuthStore } from "@/stores/auth";
import { useFileStore } from "@/stores/file";
import url from "@/utils/url";
import {
  appendOutRows,
  columnLabel,
  formatOutValue,
  isMonotonic,
  isOutFileName,
  parseOutFile,
  type OutTable,
} from "@/utils/convergeOut";
import {
  checkNewestRun,
  discoverChain,
  fetchChain,
  type ChainContext,
  type ChainFetch,
  type NewestRunFile,
} from "@/utils/outChain";
import { parseContentRange, parseUnsatisfiedRange } from "@/utils/logTail";

Chart.register(
  LineController,
  LineElement,
  PointElement,
  LinearScale,
  LogarithmicScale,
  Legend,
  Tooltip,
  Decimation
);

const SERIES_LIGHT = [
  "#2a78d6",
  "#eb6834",
  "#1baf7a",
  "#eda100",
  "#e87ba4",
  "#008300",
  "#4a3aa7",
  "#e34948",
];
const SERIES_DARK = [
  "#3987e5",
  "#d95926",
  "#199e70",
  "#c98500",
  "#d55181",
  "#008300",
  "#9085e9",
  "#e66767",
];

const maxSeries = SERIES_LIGHT.length;

const DECIMATION_THRESHOLD = 4000;

const MAX_PLOT_BYTES = 50 * 1024 * 1024;

const crosshair = {
  id: "convergeCrosshair",
  afterDraw(chart: Chart) {
    const active = chart.tooltip?.getActiveElements();
    if (!active || active.length === 0) return;

    const { top, bottom } = chart.chartArea;
    const ctx = chart.ctx;
    ctx.save();
    ctx.beginPath();
    ctx.lineWidth = 1;
    ctx.strokeStyle = isDark() ? "#4b5563" : "#c3c2b7";
    ctx.moveTo(active[0].element.x, top);
    ctx.lineTo(active[0].element.x, bottom);
    ctx.stroke();
    ctx.restore();
  },
};

const authStore = useAuthStore();
const fileStore = useFileStore();
const route = useRoute();
const router = useRouter();
const { t } = useI18n();

const loading = ref(true);
const failure = ref<string | null>(null);
const baseTable = ref<OutTable>({
  columns: [],
  values: [],
  rowCount: 0,
  skippedRows: 0,
});

const chainCtx = shallowRef<ChainContext | null>(null);
const chainData = shallowRef<ChainFetch | null>(null);
const chainDiscovered = ref(false);
const chainLoading = ref(false);
const chainNote = ref<string | null>(null);
const newerRun = ref<NewestRunFile | null>(null);

const chainRequested = computed(() => route.query.runs === "chain");
const chainActive = computed(
  () => chainRequested.value && chainData.value !== null
);
const table = computed<OutTable>(() =>
  chainRequested.value && chainData.value
    ? chainData.value.stitch.table
    : baseTable.value
);
const xIndex = ref(0);
const selected = ref<number[]>([]);
const logScale = ref(false);

const canvas = ref<HTMLCanvasElement | null>(null);
const chart = shallowRef<Chart | null>(null);

const downloadUrl = computed(() =>
  fileStore.req ? api.getDownloadURL(fileStore.req, false) : ""
);

const canOpenAsText = computed(
  () =>
    fileStore.req?.type === "text" || fileStore.req?.type === "textImmutable"
);

const yCandidates = computed(() =>
  table.value.columns.filter((c) => c.index !== xIndex.value)
);

const xMonotonic = computed(() =>
  isMonotonic(table.value.values[xIndex.value] ?? [])
);

const decimated = computed(
  () => table.value.rowCount > DECIMATION_THRESHOLD && xMonotonic.value
);

const logHidesValues = computed(() =>
  selected.value.some((c) => table.value.values[c]?.some((v) => v <= 0))
);

const isDark = () => document.documentElement.classList.contains("dark");

const seriesColor = (columnIndex: number) => {
  const slot = selected.value.indexOf(columnIndex);
  const palette = isDark() ? SERIES_DARK : SERIES_LIGHT;
  return palette[Math.max(slot, 0) % palette.length];
};

const chainShown = computed(() => chainData.value?.stitch.segments.length ?? 0);
const chainTotal = computed(() => chainData.value?.totalLegs ?? 0);
const chainTrimmed = computed(() => chainData.value?.stitch.trimmedRows ?? 0);

const segments = computed(() =>
  chainActive.value && chainData.value ? chainData.value.stitch.segments : []
);

const segmentBoundaries = computed(() => {
  const segs = segments.value;
  if (segs.length < 2) return [];
  const xs = table.value.values[xIndex.value] ?? [];
  return segs.slice(1).map((seg) => ({ x: xs[seg.startRow], name: seg.name }));
});

const runLabel = (name: string) => name.replace(/^outputs_/i, "") || name;

const segmentAt = (x: number): string | null => {
  const segs = segments.value;
  if (segs.length < 2 || !xMonotonic.value) return null;
  const xs = table.value.values[xIndex.value] ?? [];
  let name = segs[0].name;
  for (let i = 1; i < segs.length; i++) {
    if (x >= xs[segs[i].startRow]) name = segs[i].name;
    else break;
  }
  return name;
};

let seamPointerX: number | null = null;

const drawSeamLabel = (
  ctx: CanvasRenderingContext2D,
  px: number,
  top: number,
  right: number,
  text: string
) => {
  ctx.font = "11px system-ui, sans-serif";
  const width = ctx.measureText(text).width + 10;
  const x = px + 6 + width > right ? px - 6 - width : px + 6;
  ctx.setLineDash([]);
  ctx.fillStyle = isDark() ? "#1f2937" : "#ffffff";
  ctx.beginPath();
  ctx.rect(x, top + 4, width, 18);
  ctx.fill();
  ctx.stroke();
  ctx.fillStyle = isDark() ? "#c3c2b7" : "#52514e";
  ctx.textBaseline = "middle";
  ctx.fillText(text, x + 5, top + 13);
};

const chainSeams = {
  id: "convergeChainSeams",
  afterEvent(
    _chart: Chart,
    args: { event: { x: number | null }; changed?: boolean }
  ) {
    const x = args.event.x ?? null;
    if (x === seamPointerX) return;
    seamPointerX = x;
    if (segmentBoundaries.value.length > 0) args.changed = true;
  },
  afterDraw(chart: Chart) {
    const bounds = segmentBoundaries.value;
    if (bounds.length === 0) return;

    const { top, bottom, left, right } = chart.chartArea;
    const ctx = chart.ctx;
    ctx.save();
    ctx.setLineDash([4, 4]);
    ctx.lineWidth = 1;
    ctx.strokeStyle = isDark() ? "#4b5563" : "#c3c2b7";
    let hovered: { px: number; name: string } | null = null;
    for (const bound of bounds) {
      const px = chart.scales.x.getPixelForValue(bound.x);
      if (!Number.isFinite(px) || px < left || px > right) continue;
      ctx.beginPath();
      ctx.moveTo(px, top);
      ctx.lineTo(px, bottom);
      ctx.stroke();
      if (seamPointerX !== null && Math.abs(seamPointerX - px) < 6) {
        hovered = { px, name: runLabel(bound.name) };
      }
    }
    if (hovered) drawSeamLabel(ctx, hovered.px, top, right, hovered.name);
    ctx.restore();
  },
};

const storageKey = () =>
  `outPlot:${fileStore.req?.name ?? ""}:${table.value.columns
    .map((c) => c.name)
    .join(",")}`;

const restoreSelection = () => {
  try {
    const saved = JSON.parse(sessionStorage.getItem(storageKey()) ?? "");
    if (
      Number.isInteger(saved.x) &&
      saved.x >= 0 &&
      saved.x < table.value.columns.length &&
      Array.isArray(saved.ys)
    ) {
      const ys = saved.ys.filter(
        (y: unknown): y is number =>
          Number.isInteger(y) &&
          (y as number) >= 0 &&
          (y as number) < table.value.columns.length &&
          y !== saved.x
      );
      if (ys.length > 0) {
        xIndex.value = saved.x;
        selected.value = ys.slice(0, maxSeries);
        logScale.value = saved.log === true;
        return;
      }
    }
  } catch {}

  xIndex.value = 0;
  selected.value = table.value.columns.length > 1 ? [1] : [];
};

const persistSelection = () => {
  try {
    sessionStorage.setItem(
      storageKey(),
      JSON.stringify({
        x: xIndex.value,
        ys: selected.value,
        log: logScale.value,
      })
    );
  } catch {}
};

const toggleSeries = (columnIndex: number) => {
  const at = selected.value.indexOf(columnIndex);
  if (at >= 0) {
    selected.value.splice(at, 1);
  } else if (selected.value.length < maxSeries) {
    selected.value.push(columnIndex);
  }
};

let discoverController: AbortController | null = null;
let chainController: AbortController | null = null;

const setMode = (chain: boolean) => {
  router.replace({
    query: { ...route.query, runs: chain ? "chain" : undefined },
  });
};

const discoverForFile = async () => {
  discoverController?.abort();
  const controller = new AbortController();
  discoverController = controller;
  chainDiscovered.value = false;
  chainCtx.value = null;
  newerRun.value = null;

  const req = fileStore.req;
  if (req?.path && !req.isDir) {
    try {
      const ctx = await discoverChain(req.path, req.size, controller.signal);
      if (controller.signal.aborted) return;
      chainCtx.value = ctx;
      if (ctx && ctx.newestHasFile) {
        const currentLeg = ctx.legs.find((leg) => leg.current);
        if (currentLeg && currentLeg.runPath !== ctx.newestRunPath) {
          newerRun.value = {
            runName: ctx.newestRunName,
            runPath: ctx.newestRunPath,
            filePath: ctx.newestRunPath + ctx.remainder,
          };
        }
      }
    } catch {
      if (controller.signal.aborted) return;
    }
  }
  chainDiscovered.value = true;
};

const ensureChain = async () => {
  if (!chainRequested.value) {
    chainController?.abort();
    return;
  }
  if (!chainDiscovered.value) return;
  if (chainCtx.value === null) {
    setMode(false);
    return;
  }
  if (chainData.value !== null || chainLoading.value) return;

  stopLive();
  chainNote.value = null;
  const controller = new AbortController();
  chainController?.abort();
  chainController = controller;
  chainLoading.value = true;
  try {
    const req = fileStore.req;
    const result = await fetchChain(
      chainCtx.value,
      MAX_PLOT_BYTES,
      req?.path ? { path: req.path, table: baseTable.value } : null,
      controller.signal
    );
    if (chainController !== controller || controller.signal.aborted) return;
    if (!chainRequested.value) return;
    if ("error" in result) {
      chainNote.value =
        result.error === "mismatch"
          ? "outPlot.chainMismatch"
          : "outPlot.chainFailed";
      setMode(false);
    } else {
      chainData.value = result;
    }
  } catch {
    if (chainController === controller && !controller.signal.aborted) {
      chainNote.value = "outPlot.chainFailed";
      setMode(false);
    }
  } finally {
    if (chainController === controller) chainLoading.value = false;
  }
};

const openNewerRun = () => {
  if (!newerRun.value) return;
  router.push({
    path: "/files" + newerRun.value.filePath,
    query: route.query,
  });
};

// --- Live refresh --------------------------------------------------------
// The solver appends rows while a run is hot; polling asks the server for
// bytes past what has been parsed (raw.go serves ranges) and feeds only the
// new lines into the table. Byte offsets come from the raw buffers — string
// lengths cannot track them.

const LIVE_POLL_MS = 3000;
const LIVE_FRESH_WINDOW = 10 * 60 * 1000;

const following = ref(false);

let liveOffset = 0;
let livePartial = "";
let liveTimer: number | null = null;
let liveDecoder = new TextDecoder("utf-8");

const rawUrl = computed(() =>
  fileStore.req ? api.getDownloadURL(fileStore.req, true) : ""
);

const isFresh = () => {
  const modified = fileStore.req?.modified;
  return modified
    ? Date.now() - Date.parse(modified) < LIVE_FRESH_WINDOW
    : false;
};

// fetchFullForLive reads the whole file once, establishing the offset the
// polls continue from, and replaces the table.
const fetchFullForLive = async (): Promise<boolean> => {
  try {
    const res = await fetch(rawUrl.value, { cache: "no-store" });
    if (!res.ok) return false;

    const buf = await res.arrayBuffer();
    liveDecoder = new TextDecoder("utf-8");
    const parsed = parseOutFile(liveDecoder.decode(buf, { stream: true }));
    if (parsed.rowCount === 0 || parsed.columns.length < 2) return false;

    liveOffset = buf.byteLength;
    livePartial = "";

    const sameShape = parsed.columns.length === baseTable.value.columns.length;
    baseTable.value = parsed;
    if (!sameShape) {
      restoreSelection();
    } else {
      selected.value = selected.value.filter((c) => c < parsed.columns.length);
    }
    return true;
  } catch {
    return false;
  }
};

const refreshChartData = () => {
  if (!chart.value) {
    buildChart();
    return;
  }
  chart.value.data.datasets = buildDatasets();
  const dec = chart.value.options.plugins?.decimation;
  if (dec) dec.enabled = decimated.value;
  chart.value.update("none");
};

const livePoll = async () => {
  try {
    const res = await fetch(rawUrl.value, {
      headers: { Range: `bytes=${liveOffset}-` },
      cache: "no-store",
    });

    if (res.status === 404) {
      stopLive();
      return;
    }

    if (res.status === 416) {
      const size = parseUnsatisfiedRange(res.headers.get("Content-Range"));
      // Smaller than what was parsed: the file was rewritten by a new run.
      if (size !== null && size < liveOffset) {
        if (await fetchFullForLive()) refreshChartData();
      }
      return;
    }

    if (res.status === 206) {
      const range = parseContentRange(res.headers.get("Content-Range"));
      if (range && range.start !== liveOffset) {
        if (await fetchFullForLive()) refreshChartData();
        return;
      }

      const buf = await res.arrayBuffer();
      if (buf.byteLength === 0) return;

      const combined = livePartial + liveDecoder.decode(buf, { stream: true });
      const lines = combined.split("\n");
      livePartial = lines.pop() ?? "";
      liveOffset += buf.byteLength;

      if (appendOutRows(baseTable.value, lines) > 0) refreshChartData();
      return;
    }

    if (res.ok) {
      // The server answered with the whole file; resynchronize.
      if (await fetchFullForLive()) refreshChartData();
    }
  } catch {
    // A dropped poll is retried on the next tick.
  }
};

const NEWER_RUN_POLL_MS = 30 * 1000;
let newerRunTimer: number | null = null;

const checkNewer = async () => {
  const req = fileStore.req;
  if (!req?.path) return;
  try {
    newerRun.value = await checkNewestRun(req.path);
  } catch {}
};

const startPolling = () => {
  if (liveTimer === null) {
    liveTimer = window.setInterval(livePoll, LIVE_POLL_MS);
  }
  if (newerRunTimer === null) {
    newerRunTimer = window.setInterval(checkNewer, NEWER_RUN_POLL_MS);
  }
};

const stopLive = () => {
  following.value = false;
  if (liveTimer !== null) {
    clearInterval(liveTimer);
    liveTimer = null;
  }
  if (newerRunTimer !== null) {
    clearInterval(newerRunTimer);
    newerRunTimer = null;
  }
};

const toggleLive = async () => {
  if (chainRequested.value) return;
  if (following.value) {
    stopLive();
    return;
  }
  following.value = true;
  if (await fetchFullForLive()) {
    chainData.value = null;
    refreshChartData();
    startPolling();
  } else {
    following.value = false;
  }
};

const onVisibility = () => {
  if (document.hidden) {
    if (liveTimer !== null) {
      clearInterval(liveTimer);
      liveTimer = null;
    }
    if (newerRunTimer !== null) {
      clearInterval(newerRunTimer);
      newerRunTimer = null;
    }
  } else if (following.value) {
    livePoll();
    startPolling();
  }
};

const fallbackToText = () => {
  if (!canOpenAsText.value || route.query.view === "plot") return false;
  router.replace({ query: { ...route.query, view: "text" } });
  return true;
};

const load = async () => {
  const req = fileStore.req;
  if (!req) return;

  loading.value = true;
  failure.value = null;
  let redirected = false;

  try {
    if (req.size > MAX_PLOT_BYTES) {
      failure.value = "outPlot.tooLarge";
      return;
    }

    // A file written to in the last few minutes is a run in flight: load it
    // through the live path so the plot keeps growing with the solver.
    if (isFresh() && !chainRequested.value) {
      if (await fetchFullForLive()) {
        following.value = true;
        startPolling();
        discoverForFile();
        return;
      }
    }

    let text = req.content;
    if (text === undefined) {
      const res = await fetch(rawUrl.value);
      if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
      text = await res.text();
    }

    baseTable.value = parseOutFile(text);
    if (baseTable.value.rowCount === 0 || baseTable.value.columns.length < 2) {
      redirected = fallbackToText();
      if (!redirected) failure.value = "outPlot.empty";
      return;
    }

    restoreSelection();
    discoverForFile();
  } catch {
    redirected = fallbackToText();
    if (!redirected) failure.value = "outPlot.loadError";
  } finally {
    if (!redirected) loading.value = false;
  }
};

const buildDatasets = (): ChartDataset<"line">[] => {
  const xs = table.value.values[xIndex.value] ?? [];

  return selected.value.map((columnIndex, slot) => {
    const column = table.value.columns[columnIndex];
    const ys = table.value.values[columnIndex] ?? [];
    const palette = isDark() ? SERIES_DARK : SERIES_LIGHT;

    return {
      label: columnLabel(column),
      data: xs.map((x, i) => ({ x, y: ys[i] })),
      borderColor: palette[slot % palette.length],
      backgroundColor: palette[slot % palette.length],
      borderWidth: 2,
      borderJoinStyle: "round" as const,
      borderCapStyle: "round" as const,
      pointRadius: 0,
      pointHoverRadius: 4,
      pointHitRadius: 8,
      parsing: false as const,
    };
  });
};

const buildChart = () => {
  if (!canvas.value) return;

  chart.value?.destroy();

  const dark = isDark();
  const grid = dark ? "#2c2c2a" : "#e1e0d9";
  const mutedInk = "#898781";
  const secondaryInk = dark ? "#c3c2b7" : "#52514e";
  const primaryInk = dark ? "#ffffff" : "#0b0b0b";

  const xColumn = table.value.columns[xIndex.value];
  const units = new Set(
    selected.value.map((c) => table.value.columns[c]?.unit ?? "")
  );
  const sharedUnit = units.size === 1 ? [...units][0] : "";

  chart.value = new Chart(canvas.value, {
    type: "line",
    data: { datasets: buildDatasets() },
    options: {
      animation: false,
      responsive: true,
      maintainAspectRatio: false,
      interaction: { mode: "index", intersect: false },
      scales: {
        x: {
          type: "linear",
          title: {
            display: true,
            text: xColumn ? columnLabel(xColumn) : "",
            color: secondaryInk,
          },
          ticks: { color: mutedInk, maxTicksLimit: 10 },
          grid: { color: grid, lineWidth: 1 },
          border: { color: grid },
        },
        y: {
          type: logScale.value ? "logarithmic" : "linear",
          title: {
            display: sharedUnit !== "",
            text: sharedUnit,
            color: secondaryInk,
          },
          ticks: {
            color: mutedInk,
            callback: (value) => formatOutValue(Number(value)),
          },
          grid: { color: grid, lineWidth: 1 },
          border: { color: grid },
        },
      },
      plugins: {
        decimation: {
          enabled: decimated.value,
          algorithm: "lttb",
          samples: 1500,
        },
        legend: {
          display: selected.value.length > 1,
          labels: { color: primaryInk, boxWidth: 12, boxHeight: 12 },
        },
        tooltip: {
          callbacks: {
            title: (items) => {
              if (items.length === 0) return "";
              const x = items[0].parsed.x ?? NaN;
              const base = `${xColumn?.name ?? "x"} ${formatOutValue(x)}`;
              const segment = segmentAt(x);
              return segment === null ? base : `${base} · ${runLabel(segment)}`;
            },
            label: (item) => {
              const column =
                table.value.columns[selected.value[item.datasetIndex]];
              const unit = column?.unit ? ` ${column.unit}` : "";
              return ` ${column?.name}: ${formatOutValue(item.parsed.y ?? NaN)}${unit}`;
            },
          },
        },
      },
    },
    plugins: [crosshair, chainSeams],
  });
};

watch(
  [xIndex, selected, logScale, chainActive],
  () => {
    if (selected.value.includes(xIndex.value)) {
      selected.value = selected.value.filter((c) => c !== xIndex.value);
      return;
    }
    persistSelection();
    buildChart();
  },
  { deep: true }
);

watch([chainRequested, chainDiscovered, chainCtx], ensureChain);

watch(
  () => fileStore.req?.path,
  (next, prev) => {
    if (!next || next === prev) return;
    const req = fileStore.req;
    if (!req || req.isDir || !isOutFileName(req.name)) return;
    stopLive();
    chainController?.abort();
    discoverController?.abort();
    chainData.value = null;
    chainCtx.value = null;
    chainNote.value = null;
    chainLoading.value = false;
    chainDiscovered.value = false;
    newerRun.value = null;
    load();
  }
);

watch([loading, failure, canvas], () => {
  if (!loading.value && failure.value === null && canvas.value) {
    buildChart();
  }
});

const themeObserver = new MutationObserver(() => {
  if (!loading.value && failure.value === null) buildChart();
});

const keyEvent = (event: KeyboardEvent) => {
  if (event.code === "Escape") close();
};

onMounted(() => {
  window.addEventListener("keydown", keyEvent);
  document.addEventListener("visibilitychange", onVisibility);
  themeObserver.observe(document.documentElement, {
    attributes: true,
    attributeFilter: ["class"],
  });
  load();
});

onBeforeUnmount(() => {
  window.removeEventListener("keydown", keyEvent);
  document.removeEventListener("visibilitychange", onVisibility);
  stopLive();
  themeObserver.disconnect();
  chart.value?.destroy();
});

const openAsText = () => {
  router.replace({ query: { ...route.query, view: "text" } });
};

const close = () => {
  const uri = url.removeLastDir(route.path) + "/";
  router.push({ path: uri });
};
</script>
