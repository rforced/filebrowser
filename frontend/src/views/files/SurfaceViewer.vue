<template>
  <div
    id="surface-viewer-container"
    class="fixed inset-0 z-9998 flex flex-col bg-gray-50 dark:bg-gray-900"
  >
    <header
      class="flex gap-3 items-center justify-between bg-gray-200 dark:bg-gray-900 border-b border-gray-300 dark:border-gray-700 p-3 md:px-6 shrink-0"
    >
      <div class="flex gap-2 items-center min-w-0">
        <button
          v-tooltip="t('buttons.close')"
          type="button"
          class="action shrink-0"
          :aria-label="t('buttons.close')"
          @click="close()"
        >
          <i class="fa-solid fa-xmark text-lg"></i>
        </button>

        <span class="font-medium text-gray-900 dark:text-gray-100 truncate">
          {{ fileStore.req?.name ?? "" }}
        </span>
      </div>

      <div class="flex gap-2 items-center shrink-0">
        <button
          v-if="canOpenAsText"
          type="button"
          class="btn btn-flex btn-white btn-soft"
          :aria-label="t('buttons.openAsText')"
          @click="openAsText"
        >
          <i class="fa-solid fa-file-lines"></i>
          <span class="hidden md:inline">{{ t("buttons.openAsText") }}</span>
        </button>

        <a
          v-if="authStore.user?.perm.download"
          :href="downloadUrl"
          target="_blank"
          class="btn btn-flex btn-blue btn-soft"
          :aria-label="t('buttons.download')"
        >
          <i class="fa-solid fa-download"></i>
          <span class="hidden md:inline">{{ t("buttons.download") }}</span>
        </a>
      </div>
    </header>

    <div
      v-if="boundaries.length > 0"
      class="flex gap-1.5 items-start px-3 md:px-6 py-2 bg-white dark:bg-gray-800 border-b border-gray-200 dark:border-gray-700 shrink-0"
    >
      <span
        class="text-sm font-medium text-gray-700 dark:text-gray-200 mr-1.5 shrink-0 py-0.5"
      >
        {{ t("surfaceView.boundaries") }}
      </span>

      <div
        class="flex flex-wrap gap-1.5 items-center min-w-0 max-h-24 overflow-y-auto"
      >
        <button
          v-for="boundary in boundaries"
          :key="boundary.id"
          v-tooltip="
            t('surfaceView.triangles', { count: boundary.triangleCount })
          "
          type="button"
          class="px-2 py-0.5 rounded-full text-xs font-medium border transition"
          :class="
            hidden.has(boundary.id)
              ? 'border-gray-300 dark:border-gray-600 text-gray-400 dark:text-gray-500 hover:bg-gray-100 dark:hover:bg-gray-700 line-through'
              : 'border-transparent text-white'
          "
          :style="
            hidden.has(boundary.id) ? {} : { backgroundColor: boundary.color }
          "
          :aria-pressed="!hidden.has(boundary.id)"
          @click="toggleBoundary(boundary.id)"
        >
          {{ boundaryLabel(boundary.id) }}
        </button>
      </div>
    </div>

    <div class="flex-1 min-h-0 bg-gray-800 dark:bg-gray-950">
      <ModelViewer
        v-if="fileStore.req"
        ref="viewer"
        :src="rawUrl"
        extension=".dat"
        :size="fileStore.req.size"
        :content="fileStore.req.content"
        :name="fileStore.req.name"
        @boundaries="onBoundaries"
        @failed="onFailed"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";

import { files as api } from "@/api";
import { createURL } from "@/api/utils";
import ModelViewer from "@/components/files/ModelViewer.vue";
import { useAuthStore } from "@/stores/auth";
import { useFileStore } from "@/stores/file";
import {
  parseBoundaryNames,
  type SurfaceBoundaryInfo,
} from "@/utils/convergeSurface";
import url from "@/utils/url";

const authStore = useAuthStore();
const fileStore = useFileStore();
const route = useRoute();
const router = useRouter();
const { t } = useI18n();

const viewer = ref<InstanceType<typeof ModelViewer> | null>(null);
const boundaries = ref<SurfaceBoundaryInfo[]>([]);
const hidden = ref<Set<number>>(new Set());
const names = ref<Map<number, string>>(new Map());

const rawUrl = computed(() =>
  fileStore.req ? api.getDownloadURL(fileStore.req, true) : ""
);

const downloadUrl = computed(() =>
  fileStore.req ? api.getDownloadURL(fileStore.req, false) : ""
);

const canOpenAsText = computed(
  () =>
    fileStore.req?.type === "text" || fileStore.req?.type === "textImmutable"
);

const boundaryLabel = (id: number) =>
  names.value.get(id) ?? t("surfaceView.boundary", { id });

const toggleBoundary = (id: number) => {
  if (hidden.value.has(id)) {
    hidden.value.delete(id);
  } else {
    hidden.value.add(id);
  }
  viewer.value?.setBoundaryVisible(id, !hidden.value.has(id));
};

// Boundary names live in the case's boundary.in. Snapshots written during a
// run sit inside outputs dirs, so walk a few levels up towards the case root.
const fetchNames = async () => {
  let dir = url.removeLastDir(fileStore.req?.path ?? "");
  for (let depth = 0; depth < 3; depth++) {
    try {
      const res = await fetch(
        createURL("api/raw" + dir + "/boundary.in", {
          auth: authStore.token,
          inline: "true",
        })
      );
      if (res.ok) {
        names.value = parseBoundaryNames(await res.text());
        return;
      }
    } catch {
      return;
    }
    if (dir === "") break;
    dir = url.removeLastDir(dir);
  }
};

const onBoundaries = (info: SurfaceBoundaryInfo[]) => {
  boundaries.value = info;
  hidden.value = new Set();
  if (info.length > 0 && names.value.size === 0) {
    fetchNames();
  }
};

// A sniffed file that fails the strict parse opens as text instead, unless
// the 3D view was explicitly requested — then the viewer's error stands.
const onFailed = () => {
  if (canOpenAsText.value && route.query.view !== "3d") {
    router.replace({ query: { ...route.query, view: "text" } });
  }
};

const openAsText = () => {
  router.replace({ query: { ...route.query, view: "text" } });
};

const close = () => {
  const uri = url.removeLastDir(route.path) + "/";
  router.push({ path: uri });
};

const keyEvent = (event: KeyboardEvent) => {
  if (event.code === "Escape") close();
};

onMounted(() => {
  window.addEventListener("keydown", keyEvent);
});

onBeforeUnmount(() => {
  window.removeEventListener("keydown", keyEvent);
});
</script>
