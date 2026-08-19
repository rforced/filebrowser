<template>
  <div class="flex flex-col">
    <div class="card-title">
      <h2>{{ t("prompts.convergeCombine") }}</h2>
    </div>

    <div class="px-6 py-4 flex flex-col gap-3">
      <p>{{ t("prompts.convergeCombineMessage", { name: targetName }) }}</p>

      <p
        v-if="caseRunning"
        class="flex gap-2 items-start text-sm text-amber-700 dark:text-amber-400"
      >
        <i class="fa-solid fa-triangle-exclamation mt-0.5"></i>
        <span>{{ t("prompts.convergeCombineRunning") }}</span>
      </p>

      <p
        v-if="preview?.exists"
        class="flex gap-2 items-start text-sm text-amber-700 dark:text-amber-400"
      >
        <i class="fa-solid fa-triangle-exclamation mt-0.5"></i>
        <span>{{ t("converge.combineExists", { name: targetName }) }}</span>
      </p>

      <ol v-if="legs.length > 0" class="combine-legs">
        <li v-for="(leg, index) in legs" :key="leg.name">
          <span class="combine-order">{{ index + 1 }}</span>
          <code>{{ leg.name }}</code>
          <span class="combine-leg-files">
            {{ t("converge.files", { count: leg.files }) }}
          </span>
          <span class="combine-leg-size">{{ filesize(leg.bytes) }}</span>
        </li>
      </ol>

      <p class="converge-summary" v-if="scanning">
        {{ t("prompts.convergeCombineScanning") }}
      </p>
      <p class="converge-summary" v-else-if="legs.length < 2">
        {{ t("converge.combineNeedsRuns") }}
      </p>
      <p class="converge-summary" v-else-if="fileCount === 0">
        {{ t("prompts.convergeCombineEmpty") }}
      </p>
      <p class="converge-summary" v-else>
        {{
          t("prompts.convergeCombineTotal", {
            count: fileCount,
            name: targetName,
            size: filesize(preview?.bytes ?? 0),
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
        :aria-label="t('buttons.cancel')"
        :title="t('buttons.cancel')"
        tabindex="2"
      >
        {{ t("buttons.cancel") }}
      </button>
      <button
        id="focus-prompt"
        @click="submit"
        class="btn btn-blue btn-soft"
        :disabled="!canCombine"
        :aria-label="t('buttons.combineConvergeOutput')"
        :title="t('buttons.combineConvergeOutput')"
        tabindex="1"
      >
        {{ t("buttons.combineConvergeOutput") }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, inject, onActivated, onUnmounted, ref } from "vue";
import { useRoute } from "vue-router";
import { useI18n } from "vue-i18n";
import { files as api } from "@/api";
import type { ConvergeCombinePreview } from "@/api/files";
import { filesize } from "@/utils";
import { cachedConvergeSummary } from "@/utils/convergeSummaryCache";
import { useFileActions } from "@/composables/useFileActions";
import { useLayoutStore } from "@/stores/layout";

const $showError = inject<IToastError>("$showError")!;

const layoutStore = useLayoutStore();
const route = useRoute();
const { t } = useI18n();
const { combineOutput } = useFileActions();

const scanning = ref(false);
const caseRunning = ref(false);
const preview = ref<ConvergeCombinePreview | null>(null);

let previewController = new AbortController();

const legs = computed(() => preview.value?.legs ?? []);
const fileCount = computed(() => preview.value?.files ?? 0);
const targetName = computed(() => preview.value?.name ?? "outputs_combined");

// A combine needs two legs to join and somewhere to put the result. Both are
// the server's rules; asking it up front is what lets the prompt say so before
// the work starts rather than after it fails.
const canCombine = computed(
  () =>
    !scanning.value &&
    !preview.value?.exists &&
    legs.value.length > 1 &&
    fileCount.value > 0
);

// The prompt sits in a <keep-alive> and is reused across opens, so the scan
// belongs on activation: the folder being shown may not be the one it was
// opened on last.
onActivated(() => {
  scan();
});

onUnmounted(() => {
  previewController.abort();
});

const closeIfCurrent = () => {
  if (layoutStore.currentPromptName === "converge-combine") {
    layoutStore.closeHovers();
  }
};

const scan = async () => {
  scanning.value = true;
  caseRunning.value = false;
  preview.value = null;

  previewController.abort();
  previewController = new AbortController();

  try {
    preview.value = await api.convergeCombinePreview(
      route.path,
      previewController.signal
    );
  } catch (e) {
    const error = e as Error & { is_canceled?: boolean };
    if (error.is_canceled) return;
    $showError(error);
    closeIfCurrent();
    return;
  } finally {
    scanning.value = false;
  }

  // Advisory only, as in the clean prompt: a leg still being written grows
  // after its rows are read, so the combine captures a moment rather than the
  // finished run.
  try {
    const summary = await cachedConvergeSummary(
      route.path,
      previewController.signal
    );
    caseRunning.value = summary.status === "running";
  } catch {
    // No summary, no warning.
  }
};

// The combine outlives this prompt on purpose: it can run for a while, and the
// toast it reports through is not tied to a modal the user has to sit in front
// of. Closing first is what frees them to keep browsing.
const submit = () => {
  if (!canCombine.value) return;

  closeIfCurrent();
  combineOutput();
};
</script>

<style scoped>
.combine-legs {
  margin: 0.25em 0;
  padding: 0;
  list-style: none;
  font-size: 0.9em;
}

.combine-legs li {
  display: flex;
  gap: 0.6em;
  align-items: baseline;
  padding: 0.15em 0;
}

.combine-legs code {
  color: var(--textPrimary);
}

.combine-order {
  min-width: 1.4em;
  text-align: right;
  font-variant-numeric: tabular-nums;
  color: var(--textSecondary);
}

.combine-leg-files {
  color: var(--textSecondary);
}

.combine-leg-size {
  margin-left: auto;
  font-variant-numeric: tabular-nums;
  opacity: 0.75;
}

.converge-summary {
  margin-top: 0.5em;
  color: var(--textSecondary);
}
</style>
