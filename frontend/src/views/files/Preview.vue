<template>
  <div
    id="previewer"
    class="fixed inset-0 z-9999 bg-black/95 overflow-hidden"
    @touchmove.prevent.stop
    @wheel.prevent.stop
    @mousemove="toggleNavigation"
    @touchstart="toggleNavigation"
  >
    <Transition
      enter-active-class="transition ease-out duration-200"
      enter-from-class="opacity-0"
      leave-active-class="transition ease-in duration-200"
      leave-to-class="opacity-0"
    >
      <header
        v-if="isPdf || is3d || showNav"
        class="absolute top-0 left-0 right-0 z-20 flex gap-3 items-center justify-between p-3 md:px-6 bg-linear-to-b from-black/70 to-transparent"
      >
        <div class="flex gap-2 items-center min-w-0">
          <button
            v-tooltip="$t('buttons.close')"
            type="button"
            class="w-9 h-9 shrink-0 flex items-center justify-center rounded-md text-white hover:bg-white/20 transition"
            :aria-label="$t('buttons.close')"
            @click="close()"
          >
            <i class="fa-solid fa-xmark text-lg"></i>
          </button>

          <span class="text-white font-medium truncate drop-shadow-md">{{
            name
          }}</span>
        </div>

        <div class="flex gap-1 items-center shrink-0">
          <button
            v-if="isResizeEnabled && fileStore.req?.type === 'image'"
            v-tooltip="
              fullSize ? $t('buttons.fitToScreen') : $t('buttons.fullSize')
            "
            type="button"
            class="w-9 h-9 flex items-center justify-center rounded-md text-white hover:bg-white/20 transition disabled:opacity-40"
            :disabled="layoutStore.loading"
            @click="toggleSize"
          >
            <i
              class="fa-solid"
              :class="fullSize ? 'fa-compress' : 'fa-expand'"
            ></i>
          </button>

          <button
            v-if="authStore.user?.perm.rename"
            v-tooltip="$t('buttons.rename')"
            type="button"
            class="w-9 h-9 flex items-center justify-center rounded-md text-white hover:bg-white/20 transition disabled:opacity-40"
            :disabled="layoutStore.loading"
            :aria-label="$t('buttons.rename')"
            @click="layoutStore.showHover('rename')"
          >
            <i class="fa-solid fa-pen-to-square"></i>
          </button>

          <button
            v-if="authStore.user?.perm.delete"
            id="delete-button"
            v-tooltip="$t('buttons.delete')"
            type="button"
            class="w-9 h-9 flex items-center justify-center rounded-md text-white hover:bg-red-500/80 transition disabled:opacity-40"
            :disabled="layoutStore.loading"
            :aria-label="$t('buttons.delete')"
            @click="deleteFile"
          >
            <i class="fa-solid" :class="buttonIcon('delete', 'fa-trash')"></i>
          </button>

          <button
            v-if="authStore.user?.perm.download"
            v-tooltip="$t('buttons.download')"
            type="button"
            class="w-9 h-9 flex items-center justify-center rounded-md text-white hover:bg-white/20 transition disabled:opacity-40"
            :disabled="layoutStore.loading"
            :aria-label="$t('buttons.download')"
            @click="download"
          >
            <i class="fa-solid fa-download"></i>
          </button>

          <button
            v-if="
              ['image', 'audio', 'video'].includes(fileStore.req?.type || '') &&
              authStore.user?.perm.download
            "
            v-tooltip="t('buttons.openDirect')"
            type="button"
            class="w-9 h-9 flex items-center justify-center rounded-md text-white hover:bg-white/20 transition disabled:opacity-40"
            :disabled="layoutStore.loading"
            :aria-label="t('buttons.openDirect')"
            @click="openDirect"
          >
            <i class="fa-solid fa-arrow-up-right-from-square"></i>
          </button>

          <button
            v-tooltip="$t('buttons.info')"
            type="button"
            class="w-9 h-9 flex items-center justify-center rounded-md text-white hover:bg-white/20 transition disabled:opacity-40"
            :disabled="layoutStore.loading"
            :aria-label="$t('buttons.info')"
            @click="layoutStore.showHover('info')"
          >
            <i class="fa-solid fa-circle-info"></i>
          </button>
        </div>
      </header>
    </Transition>

    <div
      v-if="layoutStore.loading"
      class="h-full flex items-center justify-center"
    >
      <i class="fa-solid fa-spinner fa-spin text-4xl text-white/80"></i>
    </div>

    <template v-else>
      <div class="h-full flex items-center justify-center text-center">
        <ModelViewer
          v-if="is3d && fileStore.req"
          :src="previewUrl"
          :extension="fileStore.req.extension"
          :size="fileStore.req.size"
        />
        <ExtendedImage
          v-else-if="fileStore.req?.type == 'image'"
          :src="previewUrl"
        />
        <audio
          v-else-if="fileStore.req?.type == 'audio'"
          ref="player"
          class="w-11/12"
          :src="previewUrl"
          controls
          :autoplay="autoPlay"
          @play="autoPlay = true"
        ></audio>
        <VideoPlayer
          v-else-if="fileStore.req?.type == 'video'"
          ref="player"
          :source="previewUrl"
          :options="videoOptions"
        />
        <object
          v-else-if="isPdf"
          class="w-full h-full pt-16"
          :data="previewUrl"
        ></object>

        <div
          v-else-if="fileStore.req?.type == 'blob'"
          class="flex flex-col items-center gap-6 text-white p-6"
        >
          <div class="flex flex-col items-center gap-3">
            <i
              class="fa-solid fa-circle-exclamation text-5xl text-white/70"
            ></i>
            <span class="text-lg">{{ $t("files.noPreview") }}</span>
          </div>

          <div class="flex flex-wrap gap-3 justify-center">
            <a
              target="_blank"
              :href="downloadUrl"
              class="btn btn-flex btn-blue"
            >
              <i class="fa-solid fa-download"></i>
              <span>{{ $t("buttons.download") }}</span>
            </a>
            <a
              v-if="!fileStore.req?.isDir"
              target="_blank"
              :href="previewUrl"
              class="btn btn-flex btn-white"
            >
              <i class="fa-solid fa-arrow-up-right-from-square"></i>
              <span>{{ $t("buttons.openFile") }}</span>
            </a>
          </div>
        </div>
      </div>
    </template>

    <button
      type="button"
      class="absolute left-2 top-1/2 -translate-y-1/2 z-10 w-12 h-12 flex items-center justify-center rounded-full bg-gray-800/60 text-white transition hover:bg-gray-800/90"
      :class="!hasPrevious || !showNav ? 'opacity-0 invisible' : ''"
      :aria-label="$t('buttons.previous')"
      @click="prev"
      @mouseover="hoverNav = true"
      @mouseleave="hoverNav = false"
    >
      <i class="fa-solid fa-chevron-left text-lg"></i>
    </button>

    <button
      type="button"
      class="absolute right-2 top-1/2 -translate-y-1/2 z-10 w-12 h-12 flex items-center justify-center rounded-full bg-gray-800/60 text-white transition hover:bg-gray-800/90"
      :class="!hasNext || !showNav ? 'opacity-0 invisible' : ''"
      :aria-label="$t('buttons.next')"
      @click="next"
      @mouseover="hoverNav = true"
      @mouseleave="hoverNav = false"
    >
      <i class="fa-solid fa-chevron-right text-lg"></i>
    </button>

    <link rel="prefetch" :href="previousRaw" />
    <link rel="prefetch" :href="nextRaw" />
  </div>
</template>

<script setup lang="ts">
import { useAuthStore } from "@/stores/auth";
import { useFileStore } from "@/stores/file";
import { useLayoutStore } from "@/stores/layout";

import { files as api } from "@/api";
import { resizePreview } from "@/utils/constants";
import url from "@/utils/url";
import { throttle } from "@/utils/throttle";
import { buttonIcon } from "@/utils/buttons";
import ExtendedImage from "@/components/files/ExtendedImage.vue";
import VideoPlayer from "@/components/files/VideoPlayer.vue";
import {
  computed,
  defineAsyncComponent,
  inject,
  onBeforeUnmount,
  onMounted,
  ref,
  watch,
} from "vue";
import { useRoute, useRouter } from "vue-router";
import { useI18n } from "vue-i18n";

// three.js and its loaders are sizeable, so only fetch them when a 3D model is
// actually opened.
const ModelViewer = defineAsyncComponent(
  () => import("@/components/files/ModelViewer.vue")
);

const mediaTypes: ResourceType[] = ["image", "video", "audio", "blob", "model"];

const previousLink = ref<string>("");
const nextLink = ref<string>("");
const listing = ref<ResourceItem[] | null>(null);
const name = ref<string>("");
const fullSize = ref<boolean>(false);
const showNav = ref<boolean>(true);
const navTimeout = ref<null | number>(null);
const hoverNav = ref<boolean>(false);
const autoPlay = ref<boolean>(false);
const previousRaw = ref<string>("");
const nextRaw = ref<string>("");

const player = ref<HTMLVideoElement | HTMLAudioElement | null>(null);

const $showError = inject<IToastError>("$showError")!;

const authStore = useAuthStore();
const fileStore = useFileStore();
const layoutStore = useLayoutStore();

const { t } = useI18n();

const route = useRoute();
const router = useRouter();

const hasPrevious = computed(() => previousLink.value !== "");

const hasNext = computed(() => nextLink.value !== "");

const downloadUrl = computed(() =>
  fileStore.req ? api.getDownloadURL(fileStore.req, false) : ""
);

const directUrl = computed(() =>
  fileStore.req ? api.getDownloadURL(fileStore.req, true) : ""
);

const previewUrl = computed(() => {
  if (!fileStore.req) {
    return "";
  }

  if (fileStore.req.type === "image" && !fullSize.value) {
    return api.getPreviewURL(fileStore.req, "big");
  }

  return api.getDownloadURL(fileStore.req, true);
});

const isPdf = computed(() => fileStore.req?.extension.toLowerCase() == ".pdf");
const is3d = computed(() => fileStore.req?.type === "model");

const isResizeEnabled = computed(() => resizePreview);

const videoOptions = computed(() => {
  return { autoplay: autoPlay.value };
});

watch(route, () => {
  updatePreview();
  toggleNavigation();
});

// Specify hooks
onMounted(async () => {
  window.addEventListener("keydown", key);
  listing.value = fileStore.oldReq?.items ?? null;
  updatePreview();
});

onBeforeUnmount(() => window.removeEventListener("keydown", key));

// Specify methods
const deleteFile = () => {
  layoutStore.showHover({
    prompt: "delete",
    confirm: () => {
      if (listing.value === null) {
        return;
      }

      const index = listing.value.findIndex((item) => item.name == name.value);
      listing.value.splice(index, 1);

      if (hasNext.value) {
        next();
      } else if (!hasPrevious.value && !hasNext.value) {
        const nearbyItem = listing.value[Math.max(0, index - 1)];
        fileStore.preselect = nearbyItem?.path;

        close();
      } else {
        prev();
      }
    },
  });
};

const prev = () => {
  hoverNav.value = false;
  router.replace({ path: previousLink.value });
};

const next = () => {
  hoverNav.value = false;
  router.replace({ path: nextLink.value });
};

const key = (event: KeyboardEvent) => {
  if (layoutStore.currentPrompt !== null) {
    return;
  }
  if (event.which === 13 || event.which === 39) {
    // right arrow
    if (hasNext.value) next();
  } else if (event.which === 37) {
    // left arrow
    if (hasPrevious.value) prev();
  } else if (event.which === 27) {
    // esc
    close();
  }
};
const updatePreview = async () => {
  if (player.value && player.value.paused && !player.value.ended) {
    autoPlay.value = false;
  }

  const dirs = route.fullPath.split("/");
  name.value = decodeURIComponent(dirs[dirs.length - 1]);

  if (!listing.value) {
    try {
      const path = url.removeLastDir(route.path);
      const res = await api.fetch(path);
      listing.value = res.items;
    } catch (e: any) {
      $showError(e);
    }
  }

  previousLink.value = "";
  nextLink.value = "";
  if (listing.value) {
    for (let i = 0; i < listing.value.length; i++) {
      if (listing.value[i].name !== name.value) {
        continue;
      }

      for (let j = i - 1; j >= 0; j--) {
        if (mediaTypes.includes(listing.value[j].type)) {
          previousLink.value = listing.value[j].url;
          previousRaw.value = prefetchUrl(listing.value[j]);
          break;
        }
      }
      for (let j = i + 1; j < listing.value.length; j++) {
        if (mediaTypes.includes(listing.value[j].type)) {
          nextLink.value = listing.value[j].url;
          nextRaw.value = prefetchUrl(listing.value[j]);
          break;
        }
      }

      return;
    }
  }
};

const prefetchUrl = (item: ResourceItem) => {
  if (item.type !== "image") {
    return "";
  }

  return fullSize.value
    ? api.getDownloadURL(item, true)
    : api.getPreviewURL(item, "big");
};

const toggleSize = () => (fullSize.value = !fullSize.value);

const toggleNavigation = throttle(function () {
  showNav.value = true;

  if (navTimeout.value) {
    clearTimeout(navTimeout.value);
  }

  navTimeout.value = window.setTimeout(() => {
    showNav.value = false || hoverNav.value;
    navTimeout.value = null;
  }, 1500);
}, 500);

const close = () => {
  const uri = url.removeLastDir(route.path) + "/";
  router.push({ path: uri });
};

const download = () => window.open(downloadUrl.value);
const openDirect = () => window.open(directUrl.value);
</script>
