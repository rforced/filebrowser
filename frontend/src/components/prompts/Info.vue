<template>
  <div class="card floating">
    <div class="card-title">
      <h2>{{ t("prompts.fileInfo") }}</h2>
    </div>

    <div class="card-content">
      <p v-if="fileStore.selected.length > 1">
        {{ t("prompts.filesSelected", { count: fileStore.selected.length }) }}
      </p>

      <p class="break-word" v-if="fileStore.selected.length < 2">
        <strong>{{ t("prompts.displayName") }}</strong> {{ displayName }}
      </p>

      <p v-if="!dir || fileStore.selected.length > 1">
        <strong>{{ t("prompts.size") }}:</strong>
        <span id="content_length"></span> {{ humanSize }}
      </p>

      <template v-if="dir && fileStore.selected.length <= 1">
        <p>
          <strong>{{ t("prompts.size") }}:</strong>
          <code v-if="!folderSizeCalculated">
            <a
              @click="calculateDirSize"
              @keypress.enter="calculateDirSize"
              tabindex="2"
              >{{
                calculatingSize
                  ? t("prompts.calculating")
                  : t("prompts.calculateSize")
              }}</a
            >
          </code>
          <span v-else>{{ folderSize }}</span>
        </p>
        <p v-if="folderSizeCalculated">
          <strong>{{ t("prompts.numberFiles") }}:</strong> {{ folderNumFiles }}
        </p>
        <p v-if="folderSizeCalculated">
          <strong>{{ t("prompts.numberDirs") }}:</strong> {{ folderNumDirs }}
        </p>
      </template>

      <div v-if="resolution">
        <strong>{{ t("prompts.resolution") }}:</strong>
        {{ resolution.width }} x {{ resolution.height }}
      </div>

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
          <strong>MD5: </strong
          ><code
            ><a
              @click="checksum($event, 'md5')"
              @keypress.enter="checksum($event, 'md5')"
              tabindex="2"
              >{{ t("prompts.show") }}</a
            ></code
          >
        </p>
        <p>
          <strong>SHA1: </strong
          ><code
            ><a
              @click="checksum($event, 'sha1')"
              @keypress.enter="checksum($event, 'sha1')"
              tabindex="3"
              >{{ t("prompts.show") }}</a
            ></code
          >
        </p>
        <p>
          <strong>SHA256: </strong
          ><code
            ><a
              @click="checksum($event, 'sha256')"
              @keypress.enter="checksum($event, 'sha256')"
              tabindex="4"
              >{{ t("prompts.show") }}</a
            ></code
          >
        </p>
        <p>
          <strong>SHA512: </strong
          ><code
            ><a
              @click="checksum($event, 'sha512')"
              @keypress.enter="checksum($event, 'sha512')"
              tabindex="5"
              >{{ t("prompts.show") }}</a
            ></code
          >
        </p>
      </template>
    </div>

    <div class="card-action">
      <button
        id="focus-prompt"
        type="submit"
        @click="layoutStore.closeHovers"
        class="button button--flat"
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
import { useFileStore } from "@/stores/file";
import { useLayoutStore } from "@/stores/layout";
import { filesize } from "@/utils";
import dayjs from "dayjs";
import { files as api } from "@/api";

const $showError = inject<IToastError>("$showError")!;

const fileStore = useFileStore();
const layoutStore = useLayoutStore();
const route = useRoute();
const { t } = useI18n();

const folderSize = ref("");
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

const dir = computed(() => {
  return (
    fileStore.selectedCount > 1 ||
    (fileStore.selectedCount === 0
      ? (fileStore.req?.isDir ?? false)
      : fileStore.req!.items[fileStore.selected[0]].isDir)
  );
});

const resolution = computed(() => {
  if (fileStore.selectedCount === 1) {
    const selectedItem = fileStore.req?.items[fileStore.selected[0]];
    if (selectedItem && selectedItem.type === "image") {
      return selectedItem.resolution ?? null;
    }
  } else if (fileStore.req && fileStore.req.type === "image") {
    return fileStore.req.resolution ?? null;
  }
  return null;
});

const calculateDirSize = async () => {
  if (calculatingSize.value) return;
  calculatingSize.value = true;

  let link;
  if (fileStore.selectedCount) {
    link = fileStore.req!.items[fileStore.selected[0]].url;
  } else {
    link = route.path;
  }

  try {
    const info = await api.dirSize(link);
    folderSize.value = filesize(info.size);
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
