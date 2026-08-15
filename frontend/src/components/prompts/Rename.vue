<template>
  <div class="flex flex-col">
    <div class="text-lg font-medium text-gray-900 dark:text-gray-100">
      <h2>{{ t("prompts.rename") }}</h2>
    </div>

    <div class="px-6 py-4 flex flex-col gap-3">
      <p>
        {{ t("prompts.renameMessage") }} <code>{{ oldName }}</code
        >:
      </p>
      <input
        id="focus-prompt"
        class="form-control"
        type="text"
        @keyup.enter="submit"
        v-model.trim="name"
      />
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
        @click="submit"
        class="btn btn-blue btn-soft"
        type="submit"
        :aria-label="t('buttons.rename')"
        :title="t('buttons.rename')"
        :disabled="name === '' || name === oldName"
      >
        {{ t("buttons.rename") }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, inject, ref } from "vue";
import { useRouter } from "vue-router";
import { useI18n } from "vue-i18n";
import { useFileStore } from "@/stores/file";
import { useLayoutStore } from "@/stores/layout";
import url from "@/utils/url";
import { files as api } from "@/api";
import { removePrefix } from "@/api/utils";

const $showError = inject<IToastError>("$showError")!;

const fileStore = useFileStore();
const layoutStore = useLayoutStore();
const router = useRouter();
const { t } = useI18n();

const oldName = computed(() => {
  if (!fileStore.isListing) {
    return fileStore.req?.name ?? "";
  }

  if (fileStore.selectedCount === 0 || fileStore.selectedCount > 1) {
    // This shouldn't happen.
    return "";
  }

  return fileStore.req!.items[fileStore.selected[0]].name;
});

const name = ref(oldName.value);

const submit = async () => {
  if (name.value === "" || name.value === oldName.value) {
    return;
  }
  let oldLink = "";
  let newLink = "";

  if (!fileStore.isListing) {
    oldLink = fileStore.req!.url;
  } else {
    oldLink = fileStore.req!.items[fileStore.selected[0]].url;
  }

  newLink = url.removeLastDir(oldLink) + "/" + encodeURIComponent(name.value);

  try {
    await api.move([{ from: oldLink, to: newLink }]);
    if (!fileStore.isListing) {
      router.push({ path: newLink });
      return;
    }

    fileStore.preselect = removePrefix(newLink);

    fileStore.reload = true;
  } catch (e) {
    $showError(e as Error);
  }

  layoutStore.closeHovers();
};
</script>
