<template>
  <div
    class="item group relative cursor-pointer select-none transition"
    :class="[layoutClass, stateClass, muted ? 'opacity-40' : '']"
    role="button"
    tabindex="0"
    :draggable="isDraggable"
    :data-dir="isDir"
    :data-type="type"
    :aria-label="name"
    :aria-selected="isSelected"
    :data-ext="getExtension(name).toLowerCase()"
    @dragstart="dragStart"
    @dragover="dragOver"
    @drop="drop"
    @click="itemClick"
    @mousedown="handleMouseDown"
    @mouseup="handleMouseUp"
    @mouseleave="handleMouseLeave"
    @touchstart="handleTouchStart"
    @touchend="handleTouchEnd"
    @touchcancel="handleTouchCancel"
    @touchmove="handleTouchMove"
    @contextmenu="contextMenu"
  >
    <!-- Icon / thumbnail -->
    <div
      class="shrink-0 flex items-center justify-center"
      :class="iconWrapClass"
    >
      <img
        v-if="showThumbnail"
        v-lazy="thumbnailUrl"
        :alt="name"
        class="object-cover"
        :class="isGallery ? 'w-full h-full' : 'w-full h-full rounded-xs'"
      />
      <i
        v-else
        class="fa-solid"
        :class="[
          icon.icon,
          isSelected ? 'text-current' : icon.color,
          iconSizeClass,
        ]"
      ></i>
    </div>

    <div :class="metaWrapClass">
      <p
        class="name truncate"
        :class="[isDir ? 'font-semibold' : '', nameClass]"
      >
        {{ name }}
      </p>

      <p
        v-if="isDir"
        class="size text-sm tabular-nums"
        :class="[sizeClass, secondaryClass]"
      >
        <!--
          A folder's size is a full recursive walk, so it is never computed for
          a whole listing up front. The column offers it per row instead, and
          the answer lands in the shared usage store where the usage view and
          the storage card can reuse it.
        -->
        <span v-if="dirUsage" :title="dirUsageTitle">
          {{ filesize(dirUsage.size) }}
        </span>
        <i
          v-else-if="usageStore.pending.has(path ?? '')"
          class="fa-solid fa-spinner fa-spin opacity-60"
          :title="t('prompts.calculating')"
        ></i>
        <button
          v-else
          type="button"
          class="opacity-40 group-hover:opacity-100 transition cursor-pointer"
          :class="usageStore.failed.has(path ?? '') ? 'text-red-500' : ''"
          :aria-label="t('prompts.calculateSize')"
          :title="
            usageStore.failed.has(path ?? '')
              ? t('files.usageFailed')
              : t('prompts.calculateSize')
          "
          @click.stop="measureDir"
          @mousedown.stop
        >
          <i class="fa-solid fa-calculator"></i>
        </button>
      </p>
      <p
        v-else
        class="size text-sm tabular-nums"
        :class="[sizeClass, secondaryClass]"
      >
        {{ humanSize() }}
      </p>

      <p class="modified text-sm" :class="[modifiedClass, secondaryClass]">
        <time :datetime="modified">{{ humanTime() }}</time>
      </p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, inject, ref } from "vue";
import { useRouter } from "vue-router";
import { useI18n } from "vue-i18n";
import dayjs from "dayjs";

import { useAuthStore } from "@/stores/auth";
import { useFileStore } from "@/stores/file";
import { useLayoutStore } from "@/stores/layout";
import { useUsageStore } from "@/stores/usage";
import { compressionRatio } from "@/utils/usage";

import { enableThumbs } from "@/utils/constants";
import { filesize } from "@/utils";
import { files as api } from "@/api";
import * as upload from "@/utils/upload";
import { fileIcon, isMutedFile } from "@/utils/fileIcons";

/*
 * A single row/tile in the listing.
 *
 * The interaction model (multi-select, shift-range, drag-to-move, long-press) is
 * carried over unchanged; only presentation moved to utility classes. The `item`
 * class name is load-bearing — the drag handlers in FileListing.vue and in this
 * file walk up the DOM looking for it — so it stays even though nothing styles
 * it any more.
 */

const touches = ref<number>(0);

const longPressTimer = ref<number | null>(null);
const longPressTriggered = ref<boolean>(false);
const longPressDelay = ref<number>(500);
const startPosition = ref<{ x: number; y: number } | null>(null);
const moveThreshold = ref<number>(10);

const $showError = inject<IToastError>("$showError")!;
const router = useRouter();

const props = defineProps<{
  name: string;
  isDir: boolean;
  url: string;
  type: string;
  size: number;
  modified: string;
  index: number;
  readOnly?: boolean;
  path?: string;
}>();

const authStore = useAuthStore();
const fileStore = useFileStore();
const layoutStore = useLayoutStore();
const usageStore = useUsageStore();
const { t } = useI18n();

// --- folder size --------------------------------------------------------

const dirUsage = computed(() =>
  props.path ? usageStore.sizes.get(props.path) : undefined
);

/*
 * The tooltip carries the logical size too. Without it the column looks broken
 * on a compressed filesystem: a folder reads 3.9 GB while the files inside it
 * visibly add up to 11.6 GB, and there is nothing on screen to explain why.
 */
const dirUsageTitle = computed(() => {
  const usage = dirUsage.value;
  if (!usage) return "";

  const parts = [
    t("files.usageOnDisk", { size: filesize(usage.size) }),
    t("files.usageLogical", { size: filesize(usage.logicalSize) }),
  ];

  const ratio = compressionRatio(usage);
  if (ratio) parts.push(t("files.usageRatio", { ratio }));

  parts.push(
    t("prompts.numberFiles") + " " + usage.numFiles,
    t("prompts.numberDirs") + " " + usage.numDirs
  );

  return parts.join("\n");
});

const measureDir = () => {
  if (props.path) usageStore.measure(props.path);
};

// --- presentation -------------------------------------------------------

const viewMode = computed(() => authStore.user?.viewMode ?? "list");
const isList = computed(() => viewMode.value === "list");
const isGallery = computed(() => viewMode.value === "mosaic gallery");

const icon = computed(() =>
  fileIcon({
    isDir: props.isDir,
    type: props.type,
    extension: getExtension(props.name).toLowerCase(),
    name: props.name,
  })
);

const muted = computed(() =>
  isMutedFile({
    name: props.name,
    extension: getExtension(props.name).toLowerCase(),
  })
);

const layoutClass = computed(() => {
  if (isList.value) {
    return "flex items-center gap-3 w-full px-4 py-2.5 border-b border-gray-200 dark:border-gray-700 last:border-0";
  }
  if (isGallery.value) {
    return "flex flex-col w-full h-48 rounded-lg overflow-hidden shadow-xs ring-1 ring-black/5 dark:ring-white/10 bg-white dark:bg-gray-800";
  }
  return "flex items-center gap-3 w-full p-3 rounded-lg shadow-xs ring-1 ring-black/5 dark:ring-white/10 bg-white dark:bg-gray-800 hover:shadow-md";
});

const stateClass = computed(() => {
  if (isSelected.value) {
    // Horizon's selection treatment: a translucent wash rather than a solid
    // fill, so the file-type icon colours stay legible.
    return "bg-blue-500/20 dark:bg-blue-500/30 text-blue-900 dark:text-blue-50";
  }
  return isList.value
    ? "hover:bg-gray-100 dark:hover:bg-gray-700"
    : "hover:bg-gray-50 dark:hover:bg-gray-700";
});

const iconWrapClass = computed(() => {
  if (isList.value) return "w-8 h-8";
  if (isGallery.value)
    return "w-full flex-1 min-h-0 bg-gray-50 dark:bg-gray-900";
  return "w-12 h-12";
});

const iconSizeClass = computed(() => {
  if (isList.value) return "text-xl";
  if (isGallery.value) return "text-5xl";
  return "text-3xl";
});

const metaWrapClass = computed(() => {
  if (isList.value) return "flex-1 min-w-0 flex items-center gap-3";
  if (isGallery.value) return "w-full px-3 py-2 min-w-0 shrink-0";
  return "flex-1 min-w-0";
});

const nameClass = computed(() => (isList.value ? "flex-1 min-w-0" : ""));

const sizeClass = computed(() =>
  isList.value ? "w-24 text-right shrink-0" : isGallery.value ? "hidden" : ""
);

const modifiedClass = computed(() =>
  isList.value
    ? "w-40 text-right shrink-0 hidden sm:block"
    : isGallery.value
      ? "hidden"
      : ""
);

const secondaryClass = computed(() =>
  isSelected.value
    ? "text-blue-800 dark:text-blue-100"
    : "text-gray-600 dark:text-gray-300"
);

const showThumbnail = computed(
  () => !props.readOnly && props.type === "image" && enableThumbs
);

// --- behaviour (unchanged) ---------------------------------------------

const singleClick = computed(
  () => !props.readOnly && authStore.user?.singleClick
);
const isSelected = computed(
  () => fileStore.selected.indexOf(props.index) !== -1
);
const isDraggable = computed(
  () => !props.readOnly && authStore.user?.perm.rename
);

const canDrop = computed(() => {
  if (!props.isDir || props.readOnly) return false;

  for (const i of fileStore.selected) {
    if (fileStore.req?.items[i].url === props.url) {
      return false;
    }
  }

  return true;
});

const thumbnailUrl = computed(() => {
  const file = {
    path: props.path,
    modified: props.modified,
  };

  return api.getPreviewURL(file as Resource, "thumb");
});

const humanSize = () => {
  return props.type == "invalid_link" ? "invalid link" : filesize(props.size);
};

const humanTime = () => {
  if (!props.readOnly && authStore.user?.dateFormat) {
    return dayjs(props.modified).format("L LT");
  }
  return dayjs(props.modified).fromNow();
};

const dragStart = () => {
  if (fileStore.selectedCount === 0) {
    fileStore.selected.push(props.index);
    return;
  }

  if (!isSelected.value) {
    fileStore.selected = [];
    fileStore.selected.push(props.index);
  }
};

const dragOver = (event: Event) => {
  if (!canDrop.value) return;

  event.preventDefault();
  let el = event.target as HTMLElement | null;
  if (el !== null) {
    for (let i = 0; i < 5; i++) {
      if (!el?.classList.contains("item")) {
        el = el?.parentElement ?? null;
      }
    }

    if (el !== null) el.style.opacity = "1";
  }
};

const drop = async (event: Event) => {
  if (!canDrop.value) return;
  event.preventDefault();

  if (fileStore.selectedCount === 0) return;

  let el = event.target as HTMLElement | null;
  for (let i = 0; i < 5; i++) {
    if (el !== null && !el.classList.contains("item")) {
      el = el.parentElement;
    }
  }

  const items: any[] = [];

  for (const i of fileStore.selected) {
    if (fileStore.req) {
      items.push({
        from: fileStore.req?.items[i].url,
        to: props.url + encodeURIComponent(fileStore.req?.items[i].name),
        name: fileStore.req?.items[i].name,
        size: fileStore.req?.items[i].size,
        isDir: fileStore.req?.items[i].isDir,
        modified: fileStore.req?.items[i].modified,
        overwrite: false,
        rename: false,
      });
    }
  }

  // Get url from ListingItem instance
  if (el === null) {
    return;
  }
  const path = el.__vue__.url;

  const action = (overwrite?: boolean, rename?: boolean) => {
    api
      .move(items, overwrite, rename)
      .then(() => {
        fileStore.reload = true;
      })
      .catch($showError);
  };

  const conflict = await upload.checkConflict(items, path, true);

  if (conflict.length > 0) {
    layoutStore.showHover({
      prompt: "resolve-conflict",
      props: {
        conflict: conflict,
      },
      confirm: (event: Event, result: Array<ConflictingResource>) => {
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

const itemClick = (event: Event | KeyboardEvent) => {
  // If long press was triggered, prevent normal click behavior
  if (longPressTriggered.value) {
    longPressTriggered.value = false;
    return;
  }

  if (
    singleClick.value &&
    !(event as KeyboardEvent).ctrlKey &&
    !(event as KeyboardEvent).metaKey &&
    !(event as KeyboardEvent).shiftKey &&
    !fileStore.multiple
  )
    open();
  else click(event);
};

const contextMenu = (event: MouseEvent) => {
  event.preventDefault();
  if (
    fileStore.selected.length === 0 ||
    event.ctrlKey ||
    fileStore.selected.indexOf(props.index) === -1
  ) {
    click(event);
  }
};

const click = (event: Event | KeyboardEvent) => {
  if (!singleClick.value && fileStore.selectedCount !== 0)
    event.preventDefault();

  setTimeout(() => {
    touches.value = 0;
  }, 300);

  touches.value++;
  if (touches.value > 1) {
    open();
  }

  if (fileStore.selected.indexOf(props.index) !== -1) {
    if (
      (event as KeyboardEvent).ctrlKey ||
      (event as KeyboardEvent).metaKey ||
      fileStore.multiple
    ) {
      fileStore.removeSelected(props.index);
    } else {
      fileStore.selected = [props.index];
    }
    return;
  }

  if ((event as KeyboardEvent).shiftKey && fileStore.selected.length > 0) {
    let fi = 0;
    let la = 0;

    if (props.index > fileStore.selected[0]) {
      fi = fileStore.selected[0] + 1;
      la = props.index;
    } else {
      fi = props.index;
      la = fileStore.selected[0] - 1;
    }

    for (; fi <= la; fi++) {
      if (fileStore.selected.indexOf(fi) == -1) {
        fileStore.selected.push(fi);
      }
    }

    return;
  }

  if (
    !(event as KeyboardEvent).ctrlKey &&
    !(event as KeyboardEvent).metaKey &&
    !fileStore.multiple
  ) {
    fileStore.selected = [];
  }
  fileStore.selected.push(props.index);
};

const open = () => {
  router.push({ path: props.url });
};

const getExtension = (fileName: string): string => {
  const lastDotIndex = fileName.lastIndexOf(".");
  if (lastDotIndex === -1) {
    return fileName;
  }
  return fileName.substring(lastDotIndex);
};

// Long-press helper functions
const startLongPress = (clientX: number, clientY: number) => {
  startPosition.value = { x: clientX, y: clientY };
  longPressTimer.value = window.setTimeout(() => {
    handleLongPress();
  }, longPressDelay.value);
};

const cancelLongPress = () => {
  if (longPressTimer.value !== null) {
    window.clearTimeout(longPressTimer.value);
    longPressTimer.value = null;
  }
  startPosition.value = null;
};

const handleLongPress = () => {
  if (singleClick.value) {
    longPressTriggered.value = true;
    click(new Event("longpress"));
  }
  cancelLongPress();
};

const checkMovement = (clientX: number, clientY: number): boolean => {
  if (!startPosition.value) return false;

  const deltaX = Math.abs(clientX - startPosition.value.x);
  const deltaY = Math.abs(clientY - startPosition.value.y);

  return deltaX > moveThreshold.value || deltaY > moveThreshold.value;
};

// Event handlers
const handleMouseDown = (event: MouseEvent) => {
  if (event.button === 0) {
    startLongPress(event.clientX, event.clientY);
  }
};

const handleMouseUp = () => {
  cancelLongPress();
};

const handleMouseLeave = () => {
  cancelLongPress();
};

const handleTouchStart = (event: TouchEvent) => {
  if (event.touches.length === 1) {
    const touch = event.touches[0];
    startLongPress(touch.clientX, touch.clientY);
  }
};

const handleTouchEnd = () => {
  cancelLongPress();
};

const handleTouchCancel = () => {
  cancelLongPress();
};

const handleTouchMove = (event: TouchEvent) => {
  if (event.touches.length === 1 && startPosition.value) {
    const touch = event.touches[0];
    if (checkMovement(touch.clientX, touch.clientY)) {
      cancelLongPress();
    }
  }
};
</script>
