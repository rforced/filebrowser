<template>
  <div class="flex flex-col">
    <div class="card-title">
      <h2>{{ t("prompts.convergeClean") }}</h2>
    </div>

    <div class="px-6 py-4 flex flex-col gap-3">
      <p>{{ t("prompts.convergeCleanMessage") }}</p>

      <p
        v-if="caseRunning"
        class="flex gap-2 items-start text-sm text-amber-700 dark:text-amber-400"
      >
        <i class="fa-solid fa-triangle-exclamation mt-0.5"></i>
        <span>{{ t("prompts.convergeCleanRunning") }}</span>
      </p>

      <table class="converge-kinds">
        <tbody>
          <template v-for="kind in convergeKinds" :key="kind.key">
            <tr :class="{ 'converge-kind--empty': counts[kind.key] === 0 }">
              <td class="converge-check">
                <input
                  :id="`converge-kind-${kind.key}`"
                  v-model="checked[kind.key]"
                  type="checkbox"
                  class="form-checkbox cursor-pointer"
                  :disabled="scanning || cleaning || counts[kind.key] === 0"
                />
              </td>
              <td>
                <label
                  :for="`converge-kind-${kind.key}`"
                  class="cursor-pointer"
                >
                  {{ t(`prompts.convergeKinds.${kind.key}`) }}
                </label>
              </td>
              <td>
                <code>{{ kind.glob }}</code>
              </td>
              <td class="converge-count">
                <span v-if="scanning">&hellip;</span>
                <span v-else>{{ counts[kind.key] }}</span>
              </td>
            </tr>
            <tr
              v-if="
                kind.key === 'restart' && checked.restart && counts.restart > 0
              "
            >
              <td></td>
              <td colspan="3" class="converge-keep">
                <label class="flex gap-2 items-center flex-wrap">
                  <span>{{ t("prompts.convergeKeepRestarts") }}</span>
                  <input
                    v-model.number="keepRestarts"
                    type="number"
                    class="form-control !w-20 !py-1 text-sm"
                    min="0"
                    :max="restarts.length"
                    :disabled="scanning || cleaning || outputsSelected"
                  />
                  <span v-if="outputsSelected" class="converge-keep-note">
                    {{ t("prompts.convergeKeepRestartsSubsumed") }}
                  </span>
                </label>
              </td>
            </tr>
          </template>
        </tbody>
      </table>

      <p class="converge-summary" v-if="scanning">
        {{ t("prompts.convergeCleanScanning") }}
      </p>
      <p class="converge-summary" v-else-if="cleaning">
        {{ t("prompts.convergeCleaning") }}
      </p>
      <p class="converge-summary" v-else-if="total === 0">
        {{ t("prompts.convergeCleanEmpty") }}
      </p>
      <p class="converge-summary" v-else-if="deletedCount === 0">
        {{ t("prompts.convergeCleanNothingSelected") }}
      </p>
      <p class="converge-summary" v-else>
        {{
          t("prompts.convergeCleanTotal", {
            count: deletedCount,
            size: humanSize,
          })
        }}
      </p>
    </div>

    <div
      class="flex flex-wrap justify-end items-center gap-2 px-6 py-4 bg-gray-50 dark:bg-gray-900 rounded-b-lg"
    >
      <button
        @click="layoutStore.closeHovers"
        class="btn btn-white btn-soft"
        :disabled="cleaning"
        :aria-label="t('buttons.cancel')"
        :title="t('buttons.cancel')"
        tabindex="2"
      >
        {{ t("buttons.cancel") }}
      </button>
      <button
        id="focus-prompt"
        @click="submit"
        class="btn btn-red btn-soft"
        :disabled="scanning || cleaning || deletedCount === 0"
        :aria-label="t('buttons.delete')"
        :title="t('buttons.delete')"
        tabindex="1"
      >
        {{ t("buttons.delete") }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, inject, onActivated, onUnmounted, ref } from "vue";
import { useRoute } from "vue-router";
import { useI18n } from "vue-i18n";
import { files as api } from "@/api";
import type { ConvergeKind, ConvergeRestartInfo } from "@/api/files";
import buttons from "@/utils/buttons";
import { filesize } from "@/utils";
import { cachedConvergeSummary } from "@/utils/convergeSummaryCache";
import { useFileStore } from "@/stores/file";
import { useLayoutStore } from "@/stores/layout";

// Every family the server can report, so each tally it sends has a slot to land
// in whether or not the prompt lists it.
const allConvergeKinds: ConvergeKind[] = [
  "echo",
  "restart",
  "map",
  "out",
  "post",
  "log",
  "run",
  "nfs",
  "outputs",
];

// The rows the prompt shows, in the order the server reports them. The globs are
// the patterns themselves, so they stay verbatim in every locale — only the
// description beside them is translated.
//
// "nfs" is missing on purpose: the .nfs* stubs are an NFS implementation detail,
// and naming them here raises more questions than it answers. They ride along
// with any sweep, and are counted in the total below.
const convergeKinds: { key: ConvergeKind; glob: string }[] = [
  { key: "echo", glob: "*.echo" },
  { key: "restart", glob: "restart*.rst" },
  { key: "map", glob: "map_*.h5" },
  { key: "out", glob: "*.out" },
  { key: "post", glob: "post*.h5, post*.cgns" },
  { key: "log", glob: "*.log" },
  { key: "run", glob: "horizon.json, hosts" },
  { key: "outputs", glob: "outputs_*/" },
];

const $showError = inject<IToastError>("$showError")!;
const $showSuccess = inject<IToastSuccess>("$showSuccess")!;

const fileStore = useFileStore();
const layoutStore = useLayoutStore();
const route = useRoute();
const { t } = useI18n();

const scanning = ref(false);
const cleaning = ref(false);
const total = ref(0);
const size = ref(0);
const caseRunning = ref(false);
const restarts = ref<ConvergeRestartInfo[]>([]);
const keepRestarts = ref(0);

const emptyCounts = (): Record<ConvergeKind, number> =>
  Object.fromEntries(allConvergeKinds.map((k) => [k, 0])) as Record<
    ConvergeKind,
    number
  >;
const emptySizes = emptyCounts;
const allChecked = (): Record<ConvergeKind, boolean> =>
  Object.fromEntries(allConvergeKinds.map((k) => [k, true])) as Record<
    ConvergeKind,
    boolean
  >;

const counts = ref<Record<ConvergeKind, number>>(emptyCounts());
const sizes = ref<Record<ConvergeKind, number>>(emptySizes());
const rootCounts = ref<Record<ConvergeKind, number>>(emptyCounts());
const rootSizes = ref<Record<ConvergeKind, number>>(emptySizes());
const checked = ref<Record<ConvergeKind, boolean>>(allChecked());

let scanController = new AbortController();

// Taking the outputs folders whole subsumes the files inside them: the other
// kinds then only contribute their case-root share, and keep-newest cannot
// spare a restart that goes down with its folder.
const outputsSelected = computed(
  () => checked.value.outputs && counts.value.outputs > 0
);

// keepRestarts clamped to what actually exists, so the arithmetic below and
// the request stay honest whatever gets typed.
const keptRestarts = computed(() => {
  if (!checked.value.restart || outputsSelected.value) return 0;
  const wanted = Number.isFinite(keepRestarts.value)
    ? Math.max(0, Math.floor(keepRestarts.value))
    : 0;
  return Math.min(wanted, restarts.value.length);
});

const keptRestartSize = computed(() =>
  restarts.value
    .slice(0, keptRestarts.value)
    .reduce((sum, restart) => sum + restart.size, 0)
);

const selectedKinds = computed(() =>
  convergeKinds
    .filter((kind) => checked.value[kind.key] && counts.value[kind.key] > 0)
    .map((kind) => kind.key)
);

const tallyDeleted = (
  full: Record<ConvergeKind, number>,
  root: Record<ConvergeKind, number>
) => {
  if (selectedKinds.value.length === 0) return 0;

  let sum = 0;
  for (const kind of allConvergeKinds) {
    if (kind === "outputs") continue;
    if (kind !== "nfs" && !(checked.value[kind] && counts.value[kind] > 0)) {
      continue;
    }
    sum += outputsSelected.value ? root[kind] : full[kind];
  }
  if (outputsSelected.value) sum += full.outputs;
  return sum;
};

const deletedCount = computed(
  () => tallyDeleted(counts.value, rootCounts.value) - keptRestarts.value
);

const deletedSize = computed(
  () => tallyDeleted(sizes.value, rootSizes.value) - keptRestartSize.value
);

const humanSize = computed(() => filesize(deletedSize.value));

// The prompt lives in a <keep-alive>, so onActivated is what runs both on the
// first open and on every one after it. Rescanning each time keeps the tally
// tied to the folder actually being shown.
onActivated(() => {
  scan();
});

onUnmounted(() => {
  scanController.abort();
});

// Closing pops whatever sits on top of the prompt stack, and a sweep can outlive
// a dismissal, so only close while this prompt is still the one on top.
const closeIfCurrent = () => {
  if (layoutStore.currentPromptName === "converge-clean") {
    layoutStore.closeHovers();
  }
};

const scan = async () => {
  scanning.value = true;
  total.value = 0;
  size.value = 0;
  counts.value = emptyCounts();
  sizes.value = emptySizes();
  rootCounts.value = emptyCounts();
  rootSizes.value = emptySizes();
  checked.value = allChecked();
  restarts.value = [];
  keepRestarts.value = 0;
  caseRunning.value = false;

  scanController.abort();
  scanController = new AbortController();

  try {
    const result = await api.convergeScan(route.path, scanController.signal);
    for (const group of result.groups) {
      counts.value[group.kind] = group.count;
      sizes.value[group.kind] = group.size;
      rootCounts.value[group.kind] = group.rootCount ?? 0;
      rootSizes.value[group.kind] = group.rootSize ?? 0;
    }
    total.value = result.count;
    size.value = result.size;
    restarts.value = result.restarts;
  } catch (e) {
    const error = e as Error & { is_canceled?: boolean };
    if (error.is_canceled) return;
    $showError(error);
    closeIfCurrent();
    return;
  } finally {
    scanning.value = false;
  }

  // Advisory only: a case that still looks live gets a warning, not a block —
  // the mtime heuristic can lag a queue pause.
  try {
    const summary = await cachedConvergeSummary(
      route.path,
      scanController.signal
    );
    caseRunning.value = summary.status === "running";
  } catch {
    // No summary, no warning.
  }
};

const submit = async () => {
  if (scanning.value || cleaning.value || deletedCount.value === 0) return;

  cleaning.value = true;
  buttons.loading("converge-clean");

  try {
    // The unlisted nfs stubs ride along with any sweep.
    const kinds: ConvergeKind[] = [...selectedKinds.value, "nfs"];
    const result = await api.convergeClean(route.path, {
      kinds,
      keepRestarts: keptRestarts.value,
    });

    if (result.failed > 0) {
      // A partial sweep is reported as one: the files that stayed behind are
      // still on disk and the listing is about to show them again.
      buttons.done("converge-clean");
      $showError(
        new Error(t("prompts.convergeCleanPartial", { count: result.failed }))
      );
    } else {
      buttons.success("converge-clean");
      $showSuccess(
        t("prompts.convergeCleanDone", {
          count: result.deleted,
          size: filesize(result.size),
        })
      );
    }

    closeIfCurrent();
    fileStore.reload = true;
  } catch (e) {
    buttons.done("converge-clean");
    $showError(e as Error);
    closeIfCurrent();
    fileStore.reload = true;
  } finally {
    cleaning.value = false;
  }
};
</script>

<style scoped>
.converge-kinds {
  width: 100%;
  margin: 1em 0 0.5em;
  border-collapse: collapse;
  font-size: 0.9em;
}

.converge-kinds td {
  padding: 0.25em 0;
  vertical-align: baseline;
}

.converge-kinds code {
  color: var(--textSecondary);
  white-space: nowrap;
}

.converge-check {
  width: 1.75em;
}

.converge-count {
  text-align: right;
  font-variant-numeric: tabular-nums;
}

.converge-kind--empty {
  opacity: 0.45;
}

.converge-keep {
  padding-bottom: 0.5em;
  color: var(--textSecondary);
}

.converge-keep-note {
  font-size: 0.85em;
  opacity: 0.8;
}

.converge-summary {
  margin-top: 0.5em;
  color: var(--textSecondary);
}
</style>
