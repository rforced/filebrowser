<template>
  <div class="flex flex-col">
    <div class="card-title">
      <h2>{{ t("prompts.fileInfo") }}</h2>
    </div>

    <div class="px-6 py-4 flex flex-col gap-3">
      <p v-if="fileStore.selected.length > 1">
        {{ t("prompts.filesSelected", { count: fileStore.selected.length }) }}
      </p>

      <p class="break-word" v-if="fileStore.selected.length < 2">
        <strong>{{ t("prompts.displayName") }}</strong> {{ displayName }}
      </p>

      <div
        v-if="fileStore.selected.length < 2"
        class="flex items-start gap-2 min-w-0"
      >
        <strong class="shrink-0 py-1">{{ t("prompts.path") }}:</strong>
        <code
          class="flex-1 min-w-0 py-1 text-sm break-all text-gray-600 dark:text-gray-300"
          v-text="fullPath"
        ></code>
        <IconAction
          icon="fa-copy"
          size="sm"
          :title="t('buttons.copyToClipboard')"
          @action="copyPath"
        />
      </div>

      <p v-if="!dir || fileStore.selected.length > 1">
        <strong>{{ t("prompts.size") }}:</strong>
        <span id="content_length"></span> {{ humanSize }}
      </p>

      <template v-if="dir && fileStore.selected.length <= 1">
        <p>
          <strong>{{ t("prompts.size") }}: </strong>
          <a
            v-if="!folderSizeCalculated"
            class="action-link"
            @click="calculateDirSize"
            @keypress.enter="calculateDirSize"
            tabindex="2"
          >
            {{
              calculatingSize
                ? t("prompts.calculating")
                : t("prompts.calculateSize")
            }}
          </a>
          <span v-else>{{ folderSize }}</span>
        </p>
        <!--
          A folder's size is what it occupies, which on these compressed
          filesystems is well below the length of the files inside it. Showing
          the content size next to it stops the two from looking contradictory.
        -->
        <p v-if="folderSizeCalculated && folderLogicalSize">
          <strong>{{ t("prompts.contentSize") }}:</strong>
          {{ folderLogicalSize }}
          <span v-if="folderRatio" class="text-gray-500">
            ({{ t("files.usageRatio", { ratio: folderRatio }) }})
          </span>
        </p>
        <p v-if="folderSizeCalculated">
          <strong>{{ t("prompts.numberFiles") }}:</strong> {{ folderNumFiles }}
        </p>
        <p v-if="folderSizeCalculated">
          <strong>{{ t("prompts.numberDirs") }}:</strong> {{ folderNumDirs }}
        </p>
      </template>

      <p v-if="fileStore.selected.length < 2" :title="modTime">
        <strong>{{ t("prompts.lastModified") }}:</strong> {{ humanTime }}
      </p>

      <template v-if="dir && fileStore.selected.length === 0">
        <p>
          <strong>{{ t("prompts.numberFiles") }}:</strong>
          {{ fileStore.req?.numFiles }}
        </p>
        <p>
          <strong>{{ t("prompts.numberDirs") }}:</strong>
          {{ fileStore.req?.numDirs }}
        </p>
      </template>

      <template v-if="!dir">
        <p>
          <strong>MD5:</strong>
          <a
            class="action-link"
            @click="checksum($event, 'md5')"
            @keypress.enter="checksum($event, 'md5')"
            tabindex="2"
            >{{ t("prompts.show") }}</a
          >
        </p>
      </template>
    </div>

    <div
      class="flex flex-wrap justify-end items-center gap-2 px-6 py-4 bg-gray-50 dark:bg-gray-900 rounded-b-lg"
    >
      <button
        id="focus-prompt"
        type="submit"
        @click="layoutStore.closeHovers"
        class="btn btn-blue btn-soft"
        :aria-label="t('buttons.ok')"
        :title="t('buttons.ok')"
      >
        {{ t("buttons.ok") }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, inject, ref } from "vue";
import { useRoute } from "vue-router";
import { useI18n } from "vue-i18n";
import IconAction from "@/components/ui/IconAction.vue";
import { useFileStore } from "@/stores/file";
import { useLayoutStore } from "@/stores/layout";
import { filesize } from "@/utils";
import { copy } from "@/utils/clipboard";
import { compressionRatio } from "@/utils/usage";
import dayjs from "dayjs";
import { files as api } from "@/api";

const $showError = inject<IToastError>("$showError")!;
const $showSuccess = inject<IToastSuccess>("$showSuccess")!;

const fileStore = useFileStore();
const layoutStore = useLayoutStore();
const route = useRoute();
const { t } = useI18n();

const folderSize = ref("");
const folderLogicalSize = ref("");
const folderRatio = ref<string | null>(null);
const folderNumFiles = ref(0);
const folderNumDirs = ref(0);
const folderSizeCalculated = ref(false);
const calculatingSize = ref(false);

const humanSize = computed(() => {
  if (fileStore.selectedCount === 0 || !fileStore.isListing) {
    return filesize(fileStore.req?.size ?? 0);
  }

  let sum = 0;

  for (const selected of fileStore.selected) {
    sum += fileStore.req!.items[selected].size;
  }

  return filesize(sum);
});

const humanTime = computed(() => {
  if (fileStore.selectedCount === 0) {
    return dayjs(fileStore.req?.modified).fromNow();
  }

  return dayjs(fileStore.req!.items[fileStore.selected[0]].modified).fromNow();
});

const modTime = computed(() => {
  if (fileStore.selectedCount === 0) {
    return new Date(Date.parse(fileStore.req?.modified ?? "")).toLocaleString();
  }

  return new Date(
    Date.parse(fileStore.req!.items[fileStore.selected[0]].modified)
  ).toLocaleString();
});

const displayName = computed(() => {
  return fileStore.selectedCount === 0
    ? (fileStore.req?.name ?? "")
    : fileStore.req!.items[fileStore.selected[0]].name;
});

const fullPath = computed(() =>
  fileStore.selectedCount === 0
    ? (fileStore.req?.path ?? "")
    : fileStore.req!.items[fileStore.selected[0]].path
);

// Same fallback dance as the share links: the plain write is refused in some
// browsers until the permission has been asked for explicitly.
const copyPath = () => {
  const text = fullPath.value;
  copy({ text }).then(
    () => $showSuccess(t("success.pathCopied")),
    () =>
      copy({ text }, { permission: true }).then(
        () => $showSuccess(t("success.pathCopied")),
        (e: Error) => $showError(e)
      )
  );
};

const dir = computed(() => {
  return (
    fileStore.selectedCount > 1 ||
    (fileStore.selectedCount === 0
      ? (fileStore.req?.isDir ?? false)
      : fileStore.req!.items[fileStore.selected[0]].isDir)
  );
});

const calculateDirSize = async () => {
  if (calculatingSize.value) return;
  calculatingSize.value = true;

  // A resource path, not the router URL: dirSize addresses the API directly
  // and would mangle a /files/... path.
  let link;
  if (fileStore.selectedCount) {
    link = fileStore.req!.items[fileStore.selected[0]].path;
  } else {
    link = fileStore.req!.path;
  }

  try {
    const info = await api.dirSize(link);
    folderSize.value = filesize(info.size);
    folderLogicalSize.value = filesize(info.logicalSize);
    folderRatio.value = compressionRatio(info);
    folderNumFiles.value = info.numFiles;
    folderNumDirs.value = info.numDirs;
    folderSizeCalculated.value = true;
  } catch (e) {
    $showError(e as Error);
  } finally {
    calculatingSize.value = false;
  }
};

const checksum = async (event: Event, algo: ChecksumAlg) => {
  event.preventDefault();

  let link;

  if (fileStore.selectedCount) {
    link = fileStore.req!.items[fileStore.selected[0]].url;
  } else {
    link = route.path;
  }

  try {
    const hash = await api.checksum(link, algo);
    (event.target as HTMLElement).textContent = hash;
  } catch (e) {
    $showError(e as Error);
  }
};
</script>

<style scoped>
.action-link {
  cursor: pointer;
  text-decoration: underline;
  color: var(--blue);
}

.action-link:hover {
  opacity: 0.8;
}
</style>
