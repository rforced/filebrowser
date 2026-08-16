<template>
  <component :is="currentView" v-if="isFullBleed" />

  <main v-else class="flex flex-col gap-4 p-4">
    <Banner :title="title" :subtitle="subtitle">
      <div class="flex gap-2 items-center">
        <IconAction
          v-tooltip="t('buttons.switchView')"
          :icon="viewIcon"
          :title="t('buttons.switchView')"
          @action="switchView"
        />
        <IconAction
          :icon="fileStore.multiple ? 'fa-circle-check' : 'fa-square-check'"
          :title="t('buttons.selectMultiple')"
          @action="toggleMultipleSelection"
        />
      </div>
    </Banner>

    <hr class="border-gray-200 dark:border-gray-700" />

    <errors v-if="error" :errorCode="error.status" />

    <template v-else>
      <file-actions v-if="showSidebar" variant="rail" />

      <two-columns v-if="showSidebar">
        <template #main>
          <breadcrumbs base="/files" />
          <component :is="currentView" v-if="currentView" />
          <Card v-else class="p-10">
            <div
              class="flex flex-col items-center gap-3 text-gray-600 dark:text-gray-300"
            >
              <i class="fa-solid fa-spinner fa-spin text-3xl"></i>
              <span class="text-sm font-medium">{{ t("files.loading") }}</span>
            </div>
          </Card>
        </template>

        <template #sidebar>
          <storage-card v-if="!disableUsedPercentage" />
          <file-actions variant="stack" />
          <HelpBox v-if="domain" />
        </template>
      </two-columns>

      <template v-else>
        <breadcrumbs base="/files" />
        <Card class="p-10">
          <div
            class="flex flex-col items-center gap-3 text-gray-600 dark:text-gray-300"
          >
            <i class="fa-solid fa-spinner fa-spin text-3xl"></i>
            <span class="text-sm font-medium">{{ t("files.loading") }}</span>
          </div>
        </Card>
      </template>
    </template>
  </main>
</template>

<script setup lang="ts">
import {
  computed,
  defineAsyncComponent,
  onBeforeUnmount,
  onMounted,
  onUnmounted,
  ref,
  watch,
} from "vue";
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";
import { storeToRefs } from "pinia";

import { files as api } from "@/api";
import { useFileStore } from "@/stores/file";
import { useLayoutStore } from "@/stores/layout";
import { StatusError } from "@/api/utils";
import { name, disableUsedPercentage, domain } from "@/utils/constants";
import { isOutFileName } from "@/utils/convergeOut";
import { useFileActions } from "@/composables/useFileActions";

import Breadcrumbs from "@/components/Breadcrumbs.vue";
import Errors from "@/views/Errors.vue";
import FileListing from "@/views/files/FileListing.vue";
import TwoColumns from "@/components/layout/TwoColumns.vue";
import FileActions from "@/components/files/FileActions.vue";
import StorageCard from "@/components/files/StorageCard.vue";
import Banner from "@/components/ui/Banner.vue";
import Card from "@/components/ui/Card.vue";
import HelpBox from "@/components/ui/HelpBox.vue";
import IconAction from "@/components/ui/IconAction.vue";

const Editor = defineAsyncComponent(() => import("@/views/files/Editor.vue"));
const Preview = defineAsyncComponent(() => import("@/views/files/Preview.vue"));
const OutViewer = defineAsyncComponent(
  () => import("@/views/files/OutViewer.vue")
);

const layoutStore = useLayoutStore();
const fileStore = useFileStore();

const { reload } = storeToRefs(fileStore);

const route = useRoute();

const { t } = useI18n({});
const { viewIcon, switchView } = useFileActions();

let fetchDataController = new AbortController();

const error = ref<StatusError | null>(null);

const currentView = computed(() => {
  if (fileStore.req?.type === undefined) {
    return null;
  }

  if (fileStore.req.isDir) {
    return FileListing;
  } else if (
    isOutFileName(fileStore.req.name) &&
    (route.query.view === "plot" || fileStore.req.type === "blob")
  ) {
    // CONVERGE time series open as text by default; ?view=plot is the graph.
    // Oversized .out files have no text view, so the graph is their default.
    return OutViewer;
  } else if (
    fileStore.req.type === "text" ||
    fileStore.req.type === "textImmutable"
  ) {
    return Editor;
  } else {
    return Preview;
  }
});

const isFullBleed = computed(
  () =>
    currentView.value === Editor ||
    currentView.value === Preview ||
    currentView.value === OutViewer
);

const showSidebar = computed(
  () => !error.value && fileStore.req?.isDir !== false
);

const title = computed(() => fileStore.req?.name || t("sidebar.myFiles"));

const subtitle = computed(() => {
  if (!fileStore.req?.isDir) return undefined;

  const dirs = fileStore.req.numDirs;
  const files = fileStore.req.numFiles;
  const parts: string[] = [];

  if (dirs) parts.push(`${dirs} ${t("files.folders").toLowerCase()}`);
  if (files) parts.push(`${files} ${t("files.files").toLowerCase()}`);

  return parts.length ? parts.join(" · ") : t("files.lonely");
});

const toggleMultipleSelection = () => {
  fileStore.toggleMultiple();
  layoutStore.closeHovers();
};

onMounted(() => {
  fetchData();
  fileStore.isFiles = true;
  window.addEventListener("keydown", keyEvent);
});

onBeforeUnmount(() => {
  window.removeEventListener("keydown", keyEvent);
});

onUnmounted(() => {
  fileStore.isFiles = false;
  fileStore.updateRequest(null);
  fetchDataController.abort();
});

watch(route, () => {
  fetchData();
});
watch(reload, (newValue) => {
  newValue && fetchData();
});

// Define functions

const applyPreSelection = () => {
  const preselect = fileStore.preselect;
  fileStore.preselect = null;

  if (!fileStore.req?.isDir || fileStore.oldReq === null) return;

  let index = -1;
  if (preselect) {
    // Find item with the specified path
    index = fileStore.req.items.findIndex((item) => item.path === preselect);
  } else if (fileStore.oldReq.path.startsWith(fileStore.req.path)) {
    // Get immediate child folder of the previous path
    const name = fileStore.oldReq.path
      .substring(fileStore.req.path.length)
      .split("/")
      .shift();

    index = fileStore.req.items.findIndex(
      (val) => val.path == fileStore.req!.path + name
    );
  }

  if (index === -1) return;
  fileStore.selected.push(index);
};

const fetchData = async () => {
  // Reset view information.
  fileStore.reload = false;
  fileStore.selected = [];
  fileStore.multiple = false;
  layoutStore.closeHovers();

  // Set loading to true and reset the error.
  layoutStore.loading = true;
  error.value = null;

  let url = route.path;
  if (url === "") url = "/";
  if (url[0] !== "/") url = "/" + url;
  // Cancel the ongoing request
  fetchDataController.abort();
  fetchDataController = new AbortController();
  try {
    const res = await api.fetch(url, fetchDataController.signal);
    fileStore.updateRequest(res);
    document.title = `${res.name || t("sidebar.myFiles")} - ${t("files.files")} - ${name}`;
    layoutStore.loading = false;

    // Selects the post-reload target item or the previously visited child folder
    applyPreSelection();
  } catch (err) {
    if (err instanceof StatusError && err.is_canceled) {
      return;
    }
    if (err instanceof Error) {
      error.value = err;
    }
    layoutStore.loading = false;
  }
};
const keyEvent = (event: KeyboardEvent) => {
  if (event.key === "F1") {
    event.preventDefault();
    layoutStore.showHover("help");
  }
};
</script>
