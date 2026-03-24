<template>
  <div class="card floating">
    <div class="card-title">
      <h2>{{ t("prompts.copy") }}</h2>
    </div>

    <div class="card-content">
      <p>{{ t("prompts.copyMessage") }}</p>
      <file-list
        ref="fileList"
        @update:selected="(val: string) => (dest = val)"
        tabindex="1"
      />
    </div>

    <div
      class="card-action"
      style="display: flex; align-items: center; justify-content: space-between"
    >
      <template v-if="authStore.user?.perm.create">
        <button
          class="button button--flat"
          @click="fileList?.createDir()"
          :aria-label="t('sidebar.newFolder')"
          :title="t('sidebar.newFolder')"
          style="justify-self: left"
        >
          <span>{{ t("sidebar.newFolder") }}</span>
        </button>
      </template>
      <div>
        <button
          class="button button--flat button--grey"
          @click="layoutStore.closeHovers"
          :aria-label="t('buttons.cancel')"
          :title="t('buttons.cancel')"
          tabindex="3"
        >
          {{ t("buttons.cancel") }}
        </button>
        <button
          id="focus-prompt"
          class="button button--flat"
          @click="copy"
          :aria-label="t('buttons.copy')"
          :title="t('buttons.copy')"
          tabindex="2"
        >
          {{ t("buttons.copy") }}
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, inject } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useI18n } from "vue-i18n";
import { useFileStore } from "@/stores/file";
import { useLayoutStore } from "@/stores/layout";
import { useAuthStore } from "@/stores/auth";
import FileList from "./FileList.vue";
import { files as api } from "@/api";
import buttons from "@/utils/buttons";
import * as upload from "@/utils/upload";
import { removePrefix } from "@/api/utils";

const $showError = inject<IToastError>("$showError")!;

const fileStore = useFileStore();
const layoutStore = useLayoutStore();
const authStore = useAuthStore();
const route = useRoute();
const router = useRouter();
const { t } = useI18n();

const fileList = ref<InstanceType<typeof FileList> | null>(null);
const dest = ref<string | null>(null);

const copy = async (event: Event) => {
  event.preventDefault();
  const items: {
    from: string;
    to: string;
    name: string;
    size: number;
    modified: string;
    overwrite: boolean;
    rename: boolean;
  }[] = [];

  // Create a new promise for each file.
  for (const item of fileStore.selected) {
    items.push({
      from: fileStore.req!.items[item].url,
      to: dest.value + encodeURIComponent(fileStore.req!.items[item].name),
      name: fileStore.req!.items[item].name,
      size: fileStore.req!.items[item].size,
      modified: fileStore.req!.items[item].modified,
      overwrite: false,
      rename: route.path === dest.value,
    });
  }

  const action = async (overwrite?: boolean, rename?: boolean) => {
    buttons.loading("copy");

    await api
      .copy(items, overwrite, rename)
      .then(() => {
        buttons.success("copy");
        fileStore.preselect = removePrefix(items[0].to);

        if (route.path === dest.value) {
          fileStore.reload = true;

          return;
        }

        if (authStore.user?.redirectAfterCopyMove)
          router.push({ path: dest.value! });
      })
      .catch((e) => {
        buttons.done("copy");
        $showError(e as Error);
      });
  };

  const dstItems = (await api.fetch(dest.value!)).items;
  const conflict = upload.checkConflict(items, dstItems);

  if (conflict.length > 0) {
    layoutStore.showHover({
      prompt: "resolve-conflict",
      props: {
        conflict: conflict,
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
