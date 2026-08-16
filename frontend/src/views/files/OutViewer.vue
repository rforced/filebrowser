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

        <span class="text-xs text-gray-500 dark:text-gray-400">
          {{ t("outPlot.rows", { count: table.rowCount }) }}
          <template v-if="decimated"> · {{ t("outPlot.decimated") }}</template>
          <template v-if="table.skippedRows > 0">
            · {{ t("outPlot.skippedRows", { count: table.skippedRows }) }}
          </template>
        </span>
      </div>

      <div
        class="flex flex-wrap gap-1.5 items-center px-3 md:px-6 py-2 bg-white dark:bg-gray-800 border-b border-gray-200 dark:border-gray-700 shrink-0"
      >
        <span
          class="text-sm font-medium text-gray-700 dark:text-gray-200 mr-1.5"
        >
          {{ t("outPlot.series") }}
        </span>

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

        <span
          v-if="selected.length >= maxSeries"
          class="text-xs text-gray-500 dark:text-gray-400"
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
  columnLabel,
  formatOutValue,
  isMonotonic,
  parseOutFile,
  type OutTable,
} from "@/utils/convergeOut";

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
const table = ref<OutTable>({
  columns: [],
  values: [],
  rowCount: 0,
  skippedRows: 0,
});
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

const load = async () => {
  const req = fileStore.req;
  if (!req) return;

  loading.value = true;
  failure.value = null;

  try {
    let text = req.content;
    if (text === undefined) {
      if (req.size > MAX_PLOT_BYTES) {
        failure.value = "outPlot.tooLarge";
        return;
      }
      const res = await fetch(api.getDownloadURL(req, true));
      if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
      text = await res.text();
    }

    table.value = parseOutFile(text);
    if (table.value.rowCount === 0 || table.value.columns.length < 2) {
      failure.value = "outPlot.empty";
      return;
    }

    restoreSelection();
  } catch {
    failure.value = "outPlot.loadError";
  } finally {
    loading.value = false;
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
            title: (items) =>
              items.length > 0
                ? `${xColumn?.name ?? "x"} ${formatOutValue(items[0].parsed.x ?? NaN)}`
                : "",
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
    plugins: [crosshair],
  });
};

watch(
  [xIndex, selected, logScale],
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
  themeObserver.observe(document.documentElement, {
    attributes: true,
    attributeFilter: ["class"],
  });
  load();
});

onBeforeUnmount(() => {
  window.removeEventListener("keydown", keyEvent);
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
