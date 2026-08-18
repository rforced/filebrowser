<template>
  <div class="flex flex-col">
    <div class="card-title">
      <h2>{{ t("prompts.move") }}</h2>
    </div>

    <div class="px-6 py-4 flex flex-col gap-3">
      <prompt-targets
        v-if="targets.length > 0"
        :items="targets"
        :label="t('prompts.moving')"
      />

      <p>{{ t("prompts.moveMessage") }}</p>
      <file-list
        ref="fileList"
        @update:selected="(val: string) => (dest = val)"
        :exclude="excludedFolders"
        tabindex="1"
      />
    </div>

    <div
      class="flex flex-wrap items-center gap-2 px-6 py-4 bg-gray-50 dark:bg-gray-900 rounded-b-lg"
    >
      <button
        v-if="authStore.user?.perm.create"
        class="btn btn-blue btn-soft"
        @click="fileList?.createDir()"
        :aria-label="t('sidebar.newFolder')"
        :title="t('sidebar.newFolder')"
      >
        <span>{{ t("sidebar.newFolder") }}</span>
      </button>
      <div class="flex items-center gap-2 ml-auto">
        <button
          class="btn btn-white btn-soft"
          @click="layoutStore.closeHovers"
          :aria-label="t('buttons.cancel')"
          :title="t('buttons.cancel')"
          tabindex="3"
        >
          {{ t("buttons.cancel") }}
        </button>
        <button
          id="focus-prompt"
          class="btn btn-blue btn-soft"
          @click="move"
          :disabled="route.path === dest"
          :aria-label="t('buttons.move')"
          :title="t('buttons.move')"
          tabindex="2"
        >
          {{ t("buttons.move") }}
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, inject } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useI18n } from "vue-i18n";
import { useFileStore } from "@/stores/file";
import { useLayoutStore } from "@/stores/layout";
import { useAuthStore } from "@/stores/auth";
import FileList from "./FileList.vue";
import PromptTargets from "./PromptTargets.vue";
import { files as api } from "@/api";
import buttons from "@/utils/buttons";
import * as upload from "@/utils/upload";
import { removePrefix } from "@/api/utils";
import { usePromptTargets } from "@/composables/usePromptTargets";

const $showError = inject<IToastError>("$showError")!;

const fileStore = useFileStore();
const layoutStore = useLayoutStore();
const authStore = useAuthStore();
const route = useRoute();
const router = useRouter();
const { t } = useI18n();

const fileList = ref<InstanceType<typeof FileList> | null>(null);
const dest = ref<string | null>(null);

const { selectedTargets: targets } = usePromptTargets();

const excludedFolders = computed(() => {
  return fileStore.selected
    .filter((idx: number) => fileStore.req!.items[idx].isDir)
    .map((idx: number) => fileStore.req!.items[idx].url);
});

const move = async (event: Event) => {
  event.preventDefault();
  const items: {
    from: string;
    to: string;
    name: string;
    size: number;
    isDir: boolean;
    modified: string;
    overwrite: boolean;
    rename: boolean;
  }[] = [];

  for (const item of fileStore.selected) {
    items.push({
      from: fileStore.req!.items[item].url,
      to: dest.value + encodeURIComponent(fileStore.req!.items[item].name),
      name: fileStore.req!.items[item].name,
      size: fileStore.req!.items[item].size,
      isDir: fileStore.req!.items[item].isDir,
      modified: fileStore.req!.items[item].modified,
      overwrite: false,
      rename: false,
    });
  }

  const action = async (overwrite?: boolean, rename?: boolean) => {
    buttons.loading("move");

    await api
      .move(items, overwrite, rename)
      .then(() => {
        buttons.success("move");
        fileStore.preselect = removePrefix(items[0].to);
        if (authStore.user?.redirectAfterCopyMove)
          router.push({ path: dest.value! });
        else fileStore.reload = true;
      })
      .catch((e) => {
        buttons.done("move");
        $showError(e as Error);
      });
  };

  const conflict = await upload.checkConflict(items, dest.value!, true);

  if (conflict.length > 0) {
    layoutStore.showHover({
      prompt: "resolve-conflict",
      props: {
        conflict: conflict,
        files: items,
      },
      confirm: (event: Event, result: any[]) => {
        event.preventDefault();
        layoutStore.closeHovers();
        for (let i = result.length - 1; i >= 0; i--) {
          const item = result[i];
          if (item.checked.length == 2) {
            items[item.index].rename = true;
          } else if (item.checked.length == 1 && item.checked[0] == "origin") {
            items[item.index].overwrite = true;
          } else {
            items.splice(item.index, 1);
          }
        }
        if (items.length > 0) {
          action();
        }
      },
    });

    return;
  }

  action(false, false);
};
</script>
