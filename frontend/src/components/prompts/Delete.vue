<template>
  <div class="flex flex-col">
    <div class="px-6 py-4 flex flex-col gap-3">
      <p v-if="!fileStore.isListing || fileStore.selectedCount === 1">
        {{ t("prompts.deleteMessageSingle") }}
      </p>
      <p v-else>
        {{
          t("prompts.deleteMessageMultiple", { count: fileStore.selectedCount })
        }}
      </p>

      <prompt-targets :items="targets" />
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
        class="btn btn-red btn-soft"
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
import { computed, inject } from "vue";
import { useRoute } from "vue-router";
import { useI18n } from "vue-i18n";
import { files as api } from "@/api";
import buttons from "@/utils/buttons";
import { useFileStore } from "@/stores/file";
import { useLayoutStore } from "@/stores/layout";
import PromptTargets from "@/components/prompts/PromptTargets.vue";

const $showError = inject<IToastError>("$showError")!;

const fileStore = useFileStore();
const layoutStore = useLayoutStore();
const route = useRoute();
const { t } = useI18n();

const targets = computed(() => {
  if (!fileStore.isListing) {
    return fileStore.req
      ? [{ name: fileStore.req.name, isDir: fileStore.req.isDir }]
      : [];
  }
  return fileStore.selected.map((index) => ({
    name: fileStore.req!.items[index].name,
    isDir: fileStore.req!.items[index].isDir,
  }));
});

const submit = async () => {
  buttons.loading("delete");

  try {
    if (!fileStore.isListing) {
      await api.remove(route.path);
      buttons.success("delete");

      layoutStore.currentPrompt?.confirm();
      layoutStore.closeHovers();
      return;
    }

    layoutStore.closeHovers();

    if (fileStore.selectedCount === 0) {
      return;
    }

    const promises = [];
    for (const index of fileStore.selected) {
      promises.push(api.remove(fileStore.req!.items[index].url));
    }

    await Promise.all(promises);
    buttons.success("delete");

    const nearbyItem =
      fileStore.req!.items[Math.max(0, Math.min(...fileStore.selected) - 1)];

    fileStore.preselect = nearbyItem?.path;

    fileStore.reload = true;
  } catch (e) {
    buttons.done("delete");
    $showError(e as Error);
    if (fileStore.isListing) fileStore.reload = true;
  }
};
</script>
