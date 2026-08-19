<template>
  <component :is="currentView" v-if="isFullBleed" />

  <main v-else class="flex flex-col gap-4 p-4">
    <template v-if="error">
      <breadcrumbs base="/files">
        <template #actions>
          <IconAction
            icon="fa-arrows-rotate"
            :title="t('buttons.refresh')"
            size="lg"
            @action="refresh"
          />
        </template>
      </breadcrumbs>

      <errors :errorCode="error.status">
        <router-link to="/files/" class="btn btn-flex btn-blue btn-soft">
          <i class="fa-solid fa-folder-open"></i>
          <span>{{ t("errors.backToFiles") }}</span>
        </router-link>
      </errors>
    </template>

    <template v-else>
      <file-actions v-if="showSidebar" variant="rail" />

      <two-columns v-if="showSidebar">
        <template #main>
          <breadcrumbs base="/files">
            <template #actions>
              <IconAction
                icon="fa-arrows-rotate"
                :title="t('buttons.refresh')"
                size="lg"
                @action="refresh"
              />
              <IconAction
                :icon="viewIcon"
                :title="t('buttons.switchView')"
                size="lg"
                @action="switchView"
              />
              <IconAction
                :icon="
                  fileStore.multiple ? 'fa-circle-check' : 'fa-square-check'
                "
                :title="t('buttons.selectMultiple')"
                size="lg"
                @action="toggleMultipleSelection"
              />
            </template>
          </breadcrumbs>
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
          <Card v-if="embedded" class="p-3">
            <search />
          </Card>
          <storage-card v-if="!disableUsedPercentage" />
          <file-actions variant="stack" />
          <HelpBox v-if="domain" />
        </template>
      </two-columns>

      <template v-else>
        <breadcrumbs base="/files">
          <template #actions>
            <IconAction
              icon="fa-arrows-rotate"
              :title="t('buttons.refresh')"
              size="lg"
              @action="refresh"
            />
            <IconAction
              :icon="viewIcon"
              :title="t('buttons.switchView')"
              size="lg"
              @action="switchView"
            />
            <IconAction
              :icon="fileStore.multiple ? 'fa-circle-check' : 'fa-square-check'"
              :title="t('buttons.selectMultiple')"
              size="lg"
              @action="toggleMultipleSelection"
            />
          </template>
        </breadcrumbs>
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
import { embedded } from "@/utils/embedded";
import { isOutFileName } from "@/utils/convergeOut";
import { isSurfaceDatFile } from "@/utils/convergeSurface";
import { isH5FileName } from "@/utils/convergeH5";
import { isLogFileName } from "@/utils/logTail";
import { useFileActions } from "@/composables/useFileActions";

import Breadcrumbs from "@/components/Breadcrumbs.vue";
import Search from "@/components/Search.vue";
import Errors from "@/views/Errors.vue";
import FileListing from "@/views/files/FileListing.vue";
import TwoColumns from "@/components/layout/TwoColumns.vue";
import FileActions from "@/components/files/FileActions.vue";
import StorageCard from "@/components/files/StorageCard.vue";
import Card from "@/components/ui/Card.vue";
import HelpBox from "@/components/ui/HelpBox.vue";
import IconAction from "@/components/ui/IconAction.vue";

const Editor = defineAsyncComponent(() => import("@/views/files/Editor.vue"));
const Preview = defineAsyncComponent(() => import("@/views/files/Preview.vue"));
const OutViewer = defineAsyncComponent(
  () => import("@/views/files/OutViewer.vue")
);
const LogViewer = defineAsyncComponent(
  () => import("@/views/files/LogViewer.vue")
);
const SurfaceViewer = defineAsyncComponent(
  () => import("@/views/files/SurfaceViewer.vue")
);
const H5Viewer = defineAsyncComponent(
  () => import("@/views/files/H5Viewer.vue")
);
const DiskUsage = defineAsyncComponent(
  () => import("@/views/files/DiskUsage.vue")
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
    // A directory has one other view: where its space went.
    return route.query.view === "usage" ? DiskUsage : FileListing;
  } else if (
    isOutFileName(fileStore.req.name) &&
    (fileStore.req.type === "blob" ||
      ((fileStore.req.type === "text" ||
        fileStore.req.type === "textImmutable") &&
        route.query.view !== "text"))
  ) {
    return OutViewer;
  } else if (
    isLogFileName(fileStore.req.name) &&
    (fileStore.req.type === "blob" ||
      ((fileStore.req.type === "text" ||
        fileStore.req.type === "textImmutable") &&
        route.query.view !== "text"))
  ) {
    return LogViewer;
  } else if (
    isSurfaceDatFile(
      fileStore.req.name,
      fileStore.req.type,
      fileStore.req.content
    ) &&
    route.query.view !== "text"
  ) {
    return SurfaceViewer;
  } else if (isH5FileName(fileStore.req.name) && route.query.view !== "raw") {
    return H5Viewer;
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
    currentView.value === OutViewer ||
    currentView.value === LogViewer ||
    currentView.value === SurfaceViewer ||
    currentView.value === H5Viewer
);

const showSidebar = computed(
  () => !error.value && fileStore.req?.isDir !== false
);

const toggleMultipleSelection = () => {
  fileStore.toggleMultiple();
  layoutStore.closeHovers();
};

const refresh = () => {
  fileStore.reload = true;
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
