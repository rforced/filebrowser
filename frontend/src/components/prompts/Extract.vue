<template>
  <div class="card floating">
    <div class="card-title">
      <h2>{{ t("prompts.extract") }}</h2>
    </div>

    <div class="card-content" v-if="!extracting">
      <p>{{ t("prompts.extractMessage") }}</p>

      <label class="input-label">{{ t("prompts.extractDestination") }}</label>
      <input
        id="focus-prompt"
        class="input input--block"
        type="text"
        v-model.trim="destination"
        @keyup.enter="submit"
      />

      <div class="extract-options">
        <label class="checkbox-label">
          <input type="checkbox" v-model="deleteAfter" />
          {{ t("prompts.extractDeleteAfter") }}
        </label>
      </div>
    </div>

    <div class="card-content" v-else>
      <p>{{ t("prompts.extracting") }}</p>
      <div class="extract-progress">
        <div class="progress-bar">
          <div
            class="progress-bar-fill"
            :style="{ width: progressPercent + '%' }"
          ></div>
        </div>
        <p class="extract-status">
          {{ currentFile }}
          <span v-if="fileCount > 0">({{ fileCount }} {{ t("prompts.extractFiles") }})</span>
        </p>
      </div>
    </div>

    <div class="card-action" v-if="!extracting">
      <button
        class="button button--flat button--grey"
        @click="layoutStore.closeHovers"
        :aria-label="t('buttons.cancel')"
        :title="t('buttons.cancel')"
      >
        {{ t("buttons.cancel") }}
      </button>
      <button
        @click="submit"
        class="button button--flat"
        type="submit"
        :aria-label="t('prompts.extract')"
        :title="t('prompts.extract')"
        :disabled="destination === ''"
      >
        {{ t("prompts.extract") }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { inject, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useFileStore } from "@/stores/file";
import { useLayoutStore } from "@/stores/layout";
import { files as api } from "@/api";
import type { ExtractProgress } from "@/api/files";

const $showError = inject<IToastError>("$showError")!;

const fileStore = useFileStore();
const layoutStore = useLayoutStore();
const { t } = useI18n();

const destination = ref(layoutStore.currentPrompt?.props?.destination ?? "");
const deleteAfter = ref(false);
const extracting = ref(false);
const progressPercent = ref(0);
const currentFile = ref("");
const fileCount = ref(0);

const getFileUrl = (): string => {
  if (!fileStore.isListing) {
    return fileStore.req?.url ?? "";
  }
  if (fileStore.selectedCount === 1 && fileStore.req) {
    return fileStore.req.items[fileStore.selected[0]].url;
  }
  return "";
};

const submit = async () => {
  const fileUrl = getFileUrl();
  if (!fileUrl || destination.value === "") return;

  extracting.value = true;
  progressPercent.value = 0;
  currentFile.value = "";
  fileCount.value = 0;

  try {
    await api.extract(
      fileUrl,
      {
        destination: destination.value,
        overwrite: false,
        deleteAfter: deleteAfter.value,
      },
      (progress: ExtractProgress) => {
        fileCount.value = progress.current;
        currentFile.value = progress.currentFile;
        if (progress.total > 0) {
          progressPercent.value = Math.round(
            (progress.current / progress.total) * 100
          );
        } else {
          progressPercent.value = Math.min(
            99,
            progressPercent.value + 1
          );
        }
        if (progress.done && !progress.error) {
          progressPercent.value = 100;
        }
      }
    );

    progressPercent.value = 100;
    fileStore.reload = true;
    layoutStore.closeHovers();
  } catch (e: any) {
    extracting.value = false;
    if (e.message?.includes("409") || e.message?.includes("destination already exists")) {
      if (confirm(t("prompts.extractOverwrite"))) {
        extracting.value = true;
        try {
          await api.extract(
            fileUrl,
            {
              destination: destination.value,
              overwrite: true,
              deleteAfter: deleteAfter.value,
            },
            (progress: ExtractProgress) => {
              fileCount.value = progress.current;
              currentFile.value = progress.currentFile;
              if (progress.total > 0) {
                progressPercent.value = Math.round(
                  (progress.current / progress.total) * 100
                );
              } else {
                progressPercent.value = Math.min(
                  99,
                  progressPercent.value + 1
                );
              }
              if (progress.done && !progress.error) {
                progressPercent.value = 100;
              }
            }
          );
          progressPercent.value = 100;
          fileStore.reload = true;
          layoutStore.closeHovers();
        } catch (retryErr) {
          $showError(retryErr as Error);
          extracting.value = false;
        }
      }
    } else {
      $showError(e as Error);
    }
  }
};
</script>

<style scoped>
.extract-options {
  margin-top: 1em;
}

.checkbox-label {
  display: flex;
  align-items: center;
  gap: 0.5em;
  cursor: pointer;
}

.input-label {
  display: block;
  margin-bottom: 0.25em;
  font-weight: 500;
  font-size: 0.9em;
}

.extract-progress {
  margin-top: 1em;
}

.progress-bar {
  width: 100%;
  height: 6px;
  background: var(--divider);
  border-radius: 3px;
  overflow: hidden;
}

.progress-bar-fill {
  height: 100%;
  background: var(--blue);
  transition: width 0.3s ease;
  border-radius: 3px;
}

.extract-status {
  margin-top: 0.5em;
  font-size: 0.85em;
  color: var(--textSecondary);
  word-break: break-all;
}
</style>
