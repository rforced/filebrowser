<template>
  <div class="flex flex-col">
    <div class="card-title">
      <h2>{{ t("prompts.convergeClean") }}</h2>
    </div>

    <div class="px-6 py-4 flex flex-col gap-3">
      <p>{{ t("prompts.convergeCleanMessage") }}</p>

      <table class="converge-kinds">
        <tbody>
          <tr
            v-for="kind in convergeKinds"
            :key="kind.key"
            :class="{ 'converge-kind--empty': counts[kind.key] === 0 }"
          >
            <td>{{ t(`prompts.convergeKinds.${kind.key}`) }}</td>
            <td>
              <code>{{ kind.glob }}</code>
            </td>
            <td class="converge-count">
              <span v-if="scanning">&hellip;</span>
              <span v-else>{{ counts[kind.key] }}</span>
            </td>
          </tr>
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
      <p class="converge-summary" v-else>
        {{ t("prompts.convergeCleanTotal", { count: total, size: humanSize }) }}
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
        :disabled="scanning || cleaning || total === 0"
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
import type { ConvergeKind } from "@/api/files";
import buttons from "@/utils/buttons";
import { filesize } from "@/utils";
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
// and naming them here raises more questions than it answers. They are still
// swept, and still counted in the total below.
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
const emptyCounts = (): Record<ConvergeKind, number> =>
  Object.fromEntries(allConvergeKinds.map((k) => [k, 0])) as Record<
    ConvergeKind,
    number
  >;

const counts = ref<Record<ConvergeKind, number>>(emptyCounts());

let scanController = new AbortController();

const humanSize = computed(() => filesize(size.value));

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

  scanController.abort();
  scanController = new AbortController();

  try {
    const result = await api.convergeScan(route.path, scanController.signal);
    for (const group of result.groups) {
      counts.value[group.kind] = group.count;
    }
    total.value = result.count;
    size.value = result.size;
  } catch (e) {
    const error = e as Error & { is_canceled?: boolean };
    if (error.is_canceled) return;
    $showError(error);
    closeIfCurrent();
    return;
  } finally {
    scanning.value = false;
  }
};

const submit = async () => {
  if (scanning.value || cleaning.value || total.value === 0) return;

  cleaning.value = true;
  buttons.loading("converge-clean");

  try {
    const result = await api.convergeClean(route.path);

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

.converge-count {
  text-align: right;
  font-variant-numeric: tabular-nums;
}

.converge-kind--empty {
  opacity: 0.45;
}

.converge-summary {
  margin-top: 0.5em;
  color: var(--textSecondary);
}
</style>
