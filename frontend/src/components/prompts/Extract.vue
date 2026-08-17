<template>
  <div class="flex flex-col">
    <div class="card-title">
      <h2>{{ t("prompts.extract") }}</h2>
    </div>

    <div class="px-6 py-4 flex flex-col gap-3">
      <p>{{ t("prompts.extractMessage") }}</p>

      <label class="form-label">{{ t("prompts.extractDestination") }}</label>
      <input
        id="focus-prompt"
        class="form-control"
        type="text"
        v-model.trim="destination"
        :placeholder="t('prompts.extractDestinationPlaceholder')"
        @keyup.enter="submit()"
      />

      <div class="extract-options">
        <label class="checkbox-label">
          <input type="checkbox" v-model="deleteAfter" />
          {{ t("prompts.extractDeleteAfter") }}
        </label>
      </div>
    </div>

    <div
      class="flex flex-wrap justify-end items-center gap-2 px-6 py-4 bg-gray-50 dark:bg-gray-900 rounded-b-lg"
    >
      <button
        class="btn btn-white btn-soft"
        @click="layoutStore.closeHovers"
        :aria-label="t('buttons.cancel')"
        :title="t('buttons.cancel')"
      >
        {{ t("buttons.cancel") }}
      </button>
      <button
        @click="submit()"
        class="btn btn-blue btn-soft"
        type="submit"
        :disabled="submitting"
        :aria-label="t('prompts.extract')"
        :title="t('prompts.extract')"
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
import { useExtractStore } from "@/stores/extract";

const $showError = inject<IToastError>("$showError")!;

const fileStore = useFileStore();
const layoutStore = useLayoutStore();
const extractStore = useExtractStore();
const { t } = useI18n();

const destination = ref(layoutStore.currentPrompt?.props?.destination ?? "");
const deleteAfter = ref(false);
const submitting = ref(false);

const getFile = (): { url: string; name: string } | null => {
  if (!fileStore.isListing) {
    return fileStore.req
      ? { url: fileStore.req.url, name: fileStore.req.name }
      : null;
  }
  if (fileStore.selectedCount === 1 && fileStore.req) {
    const item = fileStore.req.items[fileStore.selected[0]];
    return { url: item.url, name: item.name };
  }
  return null;
};

const submit = async (overwrite = false) => {
  const file = getFile();
  if (!file || submitting.value) return;

  submitting.value = true;
  try {
    await extractStore.start(file.url, file.name, {
      destination: destination.value,
      overwrite,
      deleteAfter: deleteAfter.value,
    });
    layoutStore.closeHovers();
  } catch (e: any) {
    if (
      !overwrite &&
      (e.message?.includes("409") ||
        e.message?.includes("destination already exists"))
    ) {
      if (confirm(t("prompts.extractOverwrite"))) {
        submitting.value = false;
        await submit(true);
        return;
      }
    } else {
      $showError(e as Error);
    }
  } finally {
    submitting.value = false;
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
</style>
