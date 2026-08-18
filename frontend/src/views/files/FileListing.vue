<template>
  <div class="flex flex-col gap-4">
    <!-- Loading -->
    <Card v-if="layoutStore.loading" class="p-10">
      <div
        class="flex flex-col items-center gap-3 text-gray-600 dark:text-gray-300"
      >
        <i class="fa-solid fa-spinner fa-spin text-3xl"></i>
        <span class="text-sm font-medium">{{ t("files.loading") }}</span>
      </div>
    </Card>

    <template v-else>
      <converge-case-card />

      <!-- Empty -->
      <Card v-if="isEmpty" class="p-10">
        <div
          class="flex flex-col items-center gap-2 text-center text-gray-600 dark:text-gray-300"
        >
          <i class="fa-solid fa-folder-open text-4xl"></i>
          <div class="text-sm font-medium">{{ t("files.lonely") }}</div>
        </div>
      </Card>

      <!-- Listing -->
      <div
        v-else
        id="listing"
        ref="listing"
        class="file-icons"
        data-clear-on-click="true"
        @click="handleEmptyAreaClick"
        @contextmenu="showContextMenu"
      >
        <Card class="overflow-hidden">
          <!--
            Column headers. Only meaningful in list mode; the tile modes have no
            columns to sort by position.
          -->
          <div
            v-if="isList"
            class="flex items-center gap-3 px-4 py-2.5 bg-slate-100 dark:bg-gray-800 border-b border-gray-200 dark:border-gray-700"
            data-clear-on-click="true"
          >
            <div class="w-8 shrink-0"></div>
            <div class="flex-1 min-w-0 flex items-center gap-3">
              <button
                type="button"
                class="flex-1 min-w-0 flex items-center gap-1.5 text-left text-xs font-medium uppercase tracking-wider transition"
                :class="sortHeaderClass(nameSorted)"
                :aria-label="t('files.sortByName')"
                @click.stop="sort('name')"
              >
                <span>{{ t("files.name") }}</span>
                <i
                  class="fa-solid text-[0.65rem]"
                  :class="[nameIcon, sortIconClass(nameSorted)]"
                ></i>
              </button>

              <button
                type="button"
                class="w-24 shrink-0 flex items-center justify-end gap-1.5 text-xs font-medium uppercase tracking-wider transition"
                :class="sortHeaderClass(sizeSorted)"
                :aria-label="t('files.sortBySize')"
                @click.stop="sort('size')"
              >
                <span>{{ t("files.size") }}</span>
                <i
                  class="fa-solid text-[0.65rem]"
                  :class="[sizeIcon, sortIconClass(sizeSorted)]"
                ></i>
              </button>

              <button
                type="button"
                class="w-40 shrink-0 hidden sm:flex items-center justify-end gap-1.5 text-xs font-medium uppercase tracking-wider transition"
                :class="sortHeaderClass(modifiedSorted)"
                :aria-label="t('files.sortByLastModified')"
                @click.stop="sort('modified')"
              >
                <span>{{ t("files.lastModified") }}</span>
                <i
                  class="fa-solid text-[0.65rem]"
                  :class="[modifiedIcon, sortIconClass(modifiedSorted)]"
                ></i>
              </button>
            </div>
          </div>

          <!-- Folders -->
          <template v-if="fileStore.req?.numDirs">
            <h2
              v-if="!isList"
              class="px-4 pt-3 pb-1 text-xs font-medium text-gray-600 dark:text-gray-300 uppercase tracking-wider"
              data-clear-on-click="true"
            >
              {{ t("files.folders") }}
            </h2>

            <div :class="groupClass" data-clear-on-click="true">
              <item
                v-for="item in dirs"
                :key="base64(item.name)"
                :index="item.index"
                :name="item.name"
                :isDir="item.isDir"
                :url="item.url"
                :modified="item.modified"
                :type="item.type"
                :size="item.size"
                :path="item.path"
              />
            </div>
          </template>

          <hr
            v-if="isList && fileStore.req?.numDirs && fileStore.req?.numFiles"
            class="border-gray-200 dark:border-gray-700"
          />

          <!-- Files -->
          <template v-if="fileStore.req?.numFiles">
            <h2
              v-if="!isList"
              class="px-4 pt-3 pb-1 text-xs font-medium text-gray-600 dark:text-gray-300 uppercase tracking-wider"
              data-clear-on-click="true"
            >
              {{ t("files.files") }}
            </h2>

            <div :class="groupClass" data-clear-on-click="true">
              <item
                v-for="item in files"
                :key="base64(item.name)"
                :index="item.index"
                :name="item.name"
                :isDir="item.isDir"
                :url="item.url"
                :modified="item.modified"
                :type="item.type"
                :size="item.size"
                :path="item.path"
              />
            </div>
          </template>
        </Card>
      </div>

      <!-- Right-click menu, driven by the same action list as the sidebar. -->
      <context-menu
        :show="isContextMenuVisible"
        :pos="contextMenuPos"
        @hide="hideContextMenu"
      >
        <button
          v-for="action in contextActions"
          :key="action.id"
          type="button"
          class="w-full text-left flex items-center gap-2 whitespace-nowrap px-3 py-2 text-sm transition hover:bg-blue-500 hover:text-white disabled:opacity-50 disabled:cursor-not-allowed disabled:hover:bg-transparent disabled:hover:text-inherit"
          :disabled="!action.enabled"
          @click="action.run"
        >
          <i class="fa-solid fa-fw" :class="action.icon"></i>
          <span>{{ action.label }}</span>
        </button>
      </context-menu>
    </template>

    <!-- Upload inputs, kept mounted so the sidebar's Upload action can click them. -->
    <input
      id="upload-input"
      class="hidden"
      type="file"
      multiple
      @change="uploadInput($event)"
    />
    <input
      id="upload-folder-input"
      class="hidden"
      type="file"
      webkitdirectory
      multiple
      @change="uploadInput($event)"
    />

    <!-- Multiple-selection mode indicator -->
    <Transition
      enter-active-class="transition ease-out duration-200"
      enter-from-class="translate-y-full"
      leave-active-class="transition ease-in duration-200"
      leave-to-class="translate-y-full"
    >
      <div
        v-if="fileStore.multiple"
        id="multiple-selection"
        class="fixed bottom-0 left-0 w-full z-[99999] bg-blue-500 dark:bg-teal-600 text-white dark:text-blue-900 flex items-center justify-between gap-4 px-4 py-3"
      >
        <p class="text-sm font-medium">
          {{ t("files.multipleSelectionEnabled") }}
        </p>
        <button
          type="button"
          class="w-8 h-8 flex items-center justify-center rounded-md hover:bg-black/10 transition"
          :aria-label="t('buttons.clear')"
          @click="fileStore.multiple = false"
        >
          <i class="fa-solid fa-xmark"></i>
        </button>
      </div>
    </Transition>
  </div>
</template>

<script setup lang="ts">
import {
  computed,
  inject,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  watch,
} from "vue";
import { useRoute, onBeforeRouteUpdate } from "vue-router";
import { useI18n } from "vue-i18n";
import { storeToRefs } from "pinia";

import { useAuthStore } from "@/stores/auth";
import { useClipboardStore } from "@/stores/clipboard";
import { useFileStore } from "@/stores/file";
import { useLayoutStore } from "@/stores/layout";
import { useUsageStore } from "@/stores/usage";

import { users, files as api } from "@/api";
import * as upload from "@/utils/upload";
import buttons from "@/utils/buttons";
import { throttle } from "@/utils/throttle";
import { base64url } from "@/utils";
import { removePrefix } from "@/api/utils";
import { useFileActions } from "@/composables/useFileActions";

import Item from "@/components/files/ListingItem.vue";
import ContextMenu from "@/components/ContextMenu.vue";
import ConvergeCaseCard from "@/components/files/ConvergeCaseCard.vue";
import Card from "@/components/ui/Card.vue";

const showLimit = ref<number>(50);
const dragCounter = ref<number>(0);
const itemWeight = ref<number>(0);
const isContextMenuVisible = ref<boolean>(false);
const contextMenuPos = ref<{ x: number; y: number }>({ x: 0, y: 0 });

const $showError = inject<IToastError>("$showError")!;

const clipboardStore = useClipboardStore();
const authStore = useAuthStore();
const fileStore = useFileStore();
const layoutStore = useLayoutStore();
const usageStore = useUsageStore();

const { req } = storeToRefs(fileStore);

const route = useRoute();
onBeforeRouteUpdate(() => {
  hideContextMenu();
});

const { t } = useI18n();

const { actions } = useFileActions();

const contextActions = computed(() =>
  actions.value.filter((action) => action.id !== "converge-clean")
);

const listing = ref<HTMLElement | null>(null);

const viewMode = computed(() => authStore.user?.viewMode ?? "list");

const isList = computed(() => viewMode.value === "list");

const groupClass = computed(() =>
  isList.value
    ? "flex flex-col"
    : "grid gap-3 p-3 grid-cols-[repeat(auto-fill,minmax(min(100%,14rem),1fr))]"
);

const isEmpty = computed(
  () => (fileStore.req?.numDirs ?? 0) + (fileStore.req?.numFiles ?? 0) === 0
);

const nameSorted = computed(() =>
  fileStore.req ? fileStore.req.sorting.by === "name" : false
);

const sizeSorted = computed(() =>
  fileStore.req ? fileStore.req.sorting.by === "size" : false
);

const modifiedSorted = computed(() =>
  fileStore.req ? fileStore.req.sorting.by === "modified" : false
);

const ascOrdered = computed(() =>
  fileStore.req ? fileStore.req.sorting.asc : false
);

const dirs = computed(() => items.value.dirs.slice(0, showLimit.value));

const items = computed(() => {
  const dirs: any[] = [];
  const files: any[] = [];

  fileStore.req?.items.forEach((item) => {
    if (item.isDir) {
      dirs.push(item);
    } else {
      files.push(item);
    }
  });

  /*
   * Sorting is done by the server, which pushes every directory to one end
   * because a directory's own st_size says nothing about the tree under it. A
   * size the user has since calculated lives only in the client, so the sort
   * would ignore it. Reordering the folders here is safe precisely because
   * they render as their own section: the server's ordering of the files is
   * untouched. Folders with no size yet keep their existing order, after the
   * measured ones.
   */
  if (sizeSorted.value && dirs.some((d) => usageStore.sizes.has(d.path))) {
    const measured = (item: any) => usageStore.sizes.get(item.path)?.size;

    dirs.sort((a, b) => {
      const sa = measured(a);
      const sb = measured(b);
      if (sa === undefined && sb === undefined) return 0;
      if (sa === undefined) return 1;
      if (sb === undefined) return -1;
      return ascOrdered.value ? sa - sb : sb - sa;
    });
  }

  return { dirs, files };
});

const files = computed((): Resource[] => {
  let _showLimit = showLimit.value - items.value.dirs.length;

  if (_showLimit < 0) _showLimit = 0;

  return items.value.files.slice(0, _showLimit);
});

const nameIcon = computed(() =>
  nameSorted.value && !ascOrdered.value
    ? "fa-arrow-up-a-z"
    : "fa-arrow-down-a-z"
);

const sizeIcon = computed(() =>
  sizeSorted.value && ascOrdered.value
    ? "fa-arrow-down-wide-short"
    : "fa-arrow-up-short-wide"
);

const modifiedIcon = computed(() =>
  modifiedSorted.value && ascOrdered.value
    ? "fa-arrow-down-wide-short"
    : "fa-arrow-up-short-wide"
);

/* The active sort column is emphasised; the others read as secondary. */
const sortHeaderClass = (active: boolean) =>
  active
    ? "text-gray-900 dark:text-white font-semibold"
    : "text-gray-700 dark:text-gray-200 hover:text-gray-900 dark:hover:text-white";

/* Inactive sort arrows only appear on hover, as they did before. */
const sortIconClass = (active: boolean) =>
  active
    ? "opacity-100 text-blue-500 dark:text-teal"
    : "opacity-0 group-hover:opacity-60 transition-opacity";

watch(req, () => {
  // Reset the show value
  showLimit.value = 50;

  nextTick(() => {
    // Ensures that the listing is displayed
    // How much every listing item affects the window height
    setItemWeight();

    // Scroll to the item opened previously
    if (!revealPreviousItem()) {
      // Fill and fit the window with listing items
      fillWindow(true);
    }
  });
});

onMounted(() => {
  // How much every listing item affects the window height
  setItemWeight();

  // Scroll to the item opened previously
  if (!revealPreviousItem()) {
    // Fill and fit the window with listing items
    fillWindow(true);
  }

  // Add the needed event listeners to the window and document.
  window.addEventListener("keydown", keyEvent);
  window.addEventListener("scroll", scrollEvent);
  window.addEventListener("resize", windowsResize);

  if (!authStore.user?.perm.create) return;
  document.addEventListener("dragover", preventDefault);
  document.addEventListener("dragenter", dragEnter);
  document.addEventListener("dragleave", dragLeave);
  document.addEventListener("drop", drop);
});

onBeforeUnmount(() => {
  // Remove event listeners before destroying this page.
  window.removeEventListener("keydown", keyEvent);
  window.removeEventListener("scroll", scrollEvent);
  window.removeEventListener("resize", windowsResize);

  if (authStore.user && !authStore.user?.perm.create) return;
  document.removeEventListener("dragover", preventDefault);
  document.removeEventListener("dragenter", dragEnter);
  document.removeEventListener("dragleave", dragLeave);
  document.removeEventListener("drop", drop);
});

const base64 = (name: string) => base64url(name);

const keyEvent = (event: KeyboardEvent) => {
  // No prompts are shown
  if (layoutStore.currentPrompt !== null) {
    return;
  }

  if (event.key === "Escape") {
    // Reset files selection.
    fileStore.selected = [];
  }

  if (event.key === "Delete") {
    if (!authStore.user?.perm.delete || fileStore.selectedCount == 0) return;

    // Show delete prompt.
    layoutStore.showHover("delete");
  }

  if (event.key === "F2") {
    if (!authStore.user?.perm.rename || fileStore.selectedCount !== 1) return;

    // Show rename prompt.
    layoutStore.showHover("rename");
  }

  // Ctrl is pressed
  if (!event.ctrlKey && !event.metaKey) {
    return;
  }

  switch (event.key) {
    case "f":
    case "F":
      if (event.shiftKey) {
        event.preventDefault();
        layoutStore.showHover("search");
      }
      break;
    case "c":
    case "x":
      copyCut(event);
      break;
    case "v":
      paste(event);
      break;
    case "a":
      event.preventDefault();
      for (const file of items.value.files) {
        if (fileStore.selected.indexOf(file.index) === -1) {
          fileStore.selected.push(file.index);
        }
      }
      for (const dir of items.value.dirs) {
        if (fileStore.selected.indexOf(dir.index) === -1) {
          fileStore.selected.push(dir.index);
        }
      }
      break;
    case "s":
      event.preventDefault();
      document.getElementById("download-button")?.click();
      break;
  }
};

const preventDefault = (event: Event) => {
  // Wrapper around prevent default.
  event.preventDefault();
};

const copyCut = (event: Event | KeyboardEvent): void => {
  if ((event.target as HTMLElement).tagName?.toLowerCase() === "input") return;

  if (fileStore.req === null) return;

  const items = [];

  for (const i of fileStore.selected) {
    items.push({
      from: fileStore.req.items[i].url,
      name: fileStore.req.items[i].name,
      size: fileStore.req.items[i].size,
      isDir: fileStore.req.items[i].isDir,
      modified: fileStore.req.items[i].modified,
    });
  }

  if (items.length === 0) {
    return;
  }

  clipboardStore.$patch({
    key: (event as KeyboardEvent).key,
    items,
    path: route.path,
  });
};

const paste = async (event: Event) => {
  if ((event.target as HTMLElement).tagName?.toLowerCase() === "input") return;

  const items: any[] = [];

  for (const item of clipboardStore.items) {
    const from = item.from.endsWith("/") ? item.from.slice(0, -1) : item.from;
    const to = route.path + encodeURIComponent(item.name);
    items.push({
      from,
      to,
      name: item.name,
      size: item.size,
      isDir: item.isDir,
      modified: item.modified,
      overwrite: false,
      rename: clipboardStore.path == route.path,
    });
  }

  if (items.length === 0) {
    return;
  }

  const preselect = removePrefix(route.path) + items[0].name;

  let action = (overwrite?: boolean, rename?: boolean) => {
    api
      .copy(items, overwrite, rename)
      .then(() => {
        fileStore.preselect = preselect;
        fileStore.reload = true;
      })
      .catch($showError);
  };

  if (clipboardStore.key === "x") {
    action = (overwrite, rename) => {
      api
        .move(items, overwrite, rename)
        .then(() => {
          clipboardStore.resetClipboard();
          fileStore.preselect = preselect;
          fileStore.reload = true;
        })
        .catch($showError);
    };
  }

  const path = route.path.endsWith("/") ? route.path : route.path + "/";
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

const scrollEvent = throttle(() => {
  const totalItems =
    (fileStore.req?.numDirs ?? 0) + (fileStore.req?.numFiles ?? 0);

  // All items are displayed
  if (showLimit.value >= totalItems) return;

  const currentPos = window.innerHeight + window.scrollY;

  // Trigger at the 75% of the window height
  const triggerPos = document.body.offsetHeight - window.innerHeight * 0.25;

  if (currentPos > triggerPos) {
    // Quantity of items needed to fill 2x of the window height
    const showQuantity = Math.ceil((window.innerHeight * 2) / itemWeight.value);

    // Increase the number of displayed items
    showLimit.value += showQuantity;
  }
}, 100);

const dragEnter = () => {
  dragCounter.value++;

  // When the user starts dragging an item, put every
  // file on the listing with 50% opacity.
  const items = document.getElementsByClassName("item");

  Array.from(items).forEach((file: Element) => {
    (file as HTMLElement).style.opacity = "0.5";
  });
};

const dragLeave = () => {
  dragCounter.value--;

  if (dragCounter.value == 0) {
    resetOpacity();
  }
};

const drop = async (event: DragEvent) => {
  event.preventDefault();
  dragCounter.value = 0;
  resetOpacity();

  const dt = event.dataTransfer;
  let el: HTMLElement | null = event.target as HTMLElement;

  if (fileStore.req === null || dt === null || dt.files.length <= 0) return;

  for (let i = 0; i < 5; i++) {
    if (el !== null && !el.classList.contains("item")) {
      el = el.parentElement;
    }
  }

  const files: UploadList = (await upload.scanFiles(dt)) as UploadList;
  let path = route.path.endsWith("/") ? route.path : route.path + "/";

  if (
    el !== null &&
    el.classList.contains("item") &&
    el.dataset.dir === "true"
  ) {
    // Get url from ListingItem instance
    path = el.__vue__.url;
  }

  // Checking the destination hits the server, so show it is working rather
  // than leaving the action looking inert until the upload starts.
  buttons.loading("upload");
  const conflict = await upload.checkConflict(files, path);

  const preselect = removePrefix(path) + (files[0].fullPath || files[0].name);

  if (conflict.length > 0) {
    buttons.done("upload");
    layoutStore.showHover({
      prompt: "resolve-conflict",
      props: {
        conflict: conflict,
        isUploadAction: true,
      },
      confirm: (event: Event, result: Array<ConflictingResource>) => {
        event.preventDefault();
        layoutStore.closeHovers();
        for (let i = result.length - 1; i >= 0; i--) {
          const item = result[i];
          if (item.checked.length == 2) {
            continue;
          } else if (item.checked.length == 1 && item.checked[0] == "origin") {
            files[item.index].overwrite = true;
          } else {
            files.splice(item.index, 1);
          }
        }
        if (files.length > 0) {
          upload.handleFiles(files, path);
          fileStore.preselect = preselect;
        }
      },
    });

    return;
  }

  upload.handleFiles(files, path);
  fileStore.preselect = preselect;
};

const uploadInput = async (event: Event) => {
  const files = (event.currentTarget as HTMLInputElement)?.files;
  if (files === null) return;

  const folder_upload = !!files[0].webkitRelativePath;

  const uploadFiles: UploadList = [];
  for (let i = 0; i < files.length; i++) {
    const file = files[i];
    const fullPath = folder_upload ? file.webkitRelativePath : undefined;
    uploadFiles.push({
      file,
      name: file.name,
      size: file.size,
      isDir: false,
      fullPath,
    });
  }

  const path = route.path.endsWith("/") ? route.path : route.path + "/";

  // Checking the destination hits the server, so show it is working rather
  // than leaving the action looking inert until the upload starts.
  buttons.loading("upload");
  const conflict = await upload.checkConflict(uploadFiles, path);

  if (conflict.length > 0) {
    buttons.done("upload");
    layoutStore.showHover({
      prompt: "resolve-conflict",
      props: {
        conflict: conflict,
        isUploadAction: true,
      },
      confirm: (event: Event, result: Array<ConflictingResource>) => {
        event.preventDefault();
        layoutStore.closeHovers();
        for (let i = result.length - 1; i >= 0; i--) {
          const item = result[i];
          if (item.checked.length == 2) {
            continue;
          } else if (item.checked.length == 1 && item.checked[0] == "origin") {
            uploadFiles[item.index].overwrite = true;
          } else {
            uploadFiles.splice(item.index, 1);
          }
        }
        if (uploadFiles.length > 0) {
          upload.handleFiles(uploadFiles, path);
        }
      },
    });

    return;
  }

  upload.handleFiles(uploadFiles, path);
};

const resetOpacity = () => {
  const items = document.getElementsByClassName("item");

  Array.from(items).forEach((file: Element) => {
    (file as HTMLElement).style.opacity = "1";
  });
};

const sort = async (by: string) => {
  let asc = false;

  if (by === "name") {
    if (nameIcon.value === "fa-arrow-up-a-z") {
      asc = true;
    }
  } else if (by === "size") {
    if (sizeIcon.value === "fa-arrow-up-short-wide") {
      asc = true;
    }
  } else if (by === "modified") {
    if (modifiedIcon.value === "fa-arrow-up-short-wide") {
      asc = true;
    }
  }

  try {
    if (authStore.user?.id) {
      await users.update({ id: authStore.user?.id, sorting: { by, asc } }, [
        "sorting",
      ]);
    }
  } catch (e: any) {
    $showError(e);
  }

  fileStore.reload = true;
};

const windowsResize = throttle(() => {
  // Listing element is not displayed
  if (listing.value == null) return;

  // How much every listing item affects the window height
  setItemWeight();

  // Fill but not fit the window
  fillWindow();
}, 100);

const setItemWeight = () => {
  // Listing element is not displayed
  if (listing.value === null || fileStore.req === null) return;

  let itemQuantity = fileStore.req.numDirs + fileStore.req.numFiles;
  if (itemQuantity > showLimit.value) itemQuantity = showLimit.value;

  // How much every listing item affects the window height
  itemWeight.value = listing.value.offsetHeight / itemQuantity;
};

const fillWindow = (fit = false) => {
  if (fileStore.req === null) return;

  const totalItems = fileStore.req.numDirs + fileStore.req.numFiles;

  // More items are displayed than the total
  if (showLimit.value >= totalItems && !fit) return;

  const windowHeight = window.innerHeight;

  // Quantity of items needed to fill 2x of the window height
  const showQuantity = Math.ceil(
    (windowHeight + windowHeight * 2) / itemWeight.value
  );

  // Less items to display than current
  if (showLimit.value > showQuantity && !fit) return;

  // Set the number of displayed items
  showLimit.value = showQuantity > totalItems ? totalItems : showQuantity;
};

const revealPreviousItem = () => {
  if (!fileStore.req || !fileStore.oldReq) return;

  const index = fileStore.selected[0];
  if (index === undefined) return;

  showLimit.value =
    index + Math.ceil((window.innerHeight * 2) / itemWeight.value);

  nextTick(() => {
    const items = document.querySelectorAll("#listing .item");
    items[index]?.scrollIntoView({ block: "center" });
  });

  return true;
};

const showContextMenu = (event: MouseEvent) => {
  event.preventDefault();
  isContextMenuVisible.value = true;
  contextMenuPos.value = {
    x: event.clientX + 8,
    y: event.clientY + Math.floor(window.scrollY),
  };
};

const hideContextMenu = () => {
  isContextMenuVisible.value = false;
};

const handleEmptyAreaClick = (e: MouseEvent) => {
  const target = e.target;
  if (!(target instanceof HTMLElement)) return;

  if (target.dataset.clearOnClick === "true") {
    fileStore.selected = [];
  }
};
</script>
