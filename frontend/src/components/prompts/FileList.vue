<template>
  <div class="flex flex-col gap-3">
    <ul
      v-if="items.length"
      class="file-list rounded-md border border-gray-200 dark:border-gray-700 divide-y divide-gray-200 dark:divide-gray-700 max-h-64 overflow-y-auto"
    >
      <li
        v-for="item in items"
        :key="item.name"
        role="button"
        tabindex="0"
        :aria-label="item.name"
        :aria-selected="selected == item.url"
        :data-url="item.url"
        class="group flex items-center gap-3 px-3 py-2 text-sm cursor-pointer select-none transition"
        :class="
          selected == item.url
            ? 'bg-blue-500/20 dark:bg-blue-500/30 text-blue-900 dark:text-blue-50'
            : 'hover:bg-gray-100 dark:hover:bg-gray-700'
        "
        @click="itemClick"
        @touchstart="touchstart"
        @dblclick="next"
      >
        <i
          class="fa-solid fa-fw shrink-0"
          :class="
            item.name === '..'
              ? 'fa-arrow-turn-up text-gray-500 dark:text-gray-400'
              : [
                  'fa-folder',
                  selected == item.url
                    ? 'text-current'
                    : 'text-blue-500 dark:text-blue-100',
                ]
          "
        ></i>
        <span class="flex-1 min-w-0 truncate">{{ item.name }}</span>
        <button
          type="button"
          class="action shrink-0 opacity-60 group-hover:opacity-100"
          :data-url="item.url"
          :aria-label="$t('buttons.openFolder')"
          :title="$t('buttons.openFolder')"
          @click.stop="next"
          @dblclick.stop
        >
          <i class="fa-solid fa-chevron-right text-xs"></i>
        </button>
      </li>
    </ul>

    <p class="text-sm text-gray-600 dark:text-gray-300">
      {{ $t("prompts.currentlyNavigating") }}
      <code
        class="text-xs px-1.5 py-0.5 rounded bg-gray-100 dark:bg-gray-900"
        >{{ nav }}</code
      >.
    </p>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, inject } from "vue";
import { useRoute } from "vue-router";
import { useAuthStore } from "@/stores/auth";
import { useFileStore } from "@/stores/file";
import { useLayoutStore } from "@/stores/layout";

import urlUtil from "@/utils/url";
import { files } from "@/api";
import { StatusError } from "@/api/utils.js";

const props = withDefaults(
  defineProps<{
    exclude?: string[];
  }>(),
  {
    exclude: () => [],
  }
);

const emit = defineEmits<{
  "update:selected": [value: string];
}>();

const route = useRoute();
const authStore = useAuthStore();
const fileStore = useFileStore();
const layoutStore = useLayoutStore();
const $showError = inject<IToastError>("$showError")!;

interface FileListItem {
  name: string;
  url: string;
}

const items = ref<FileListItem[]>([]);
const touches = ref({ id: "", count: 0 });
const selected = ref<string | null>(null);
const current = ref(window.location.pathname);
let nextAbortController = new AbortController();

const nav = computed(() => decodeURIComponent(current.value));

const fillOptions = (req: Resource) => {
  // Sets the current path and resets
  // the current items.
  current.value = req.url;
  items.value = [];

  emit("update:selected", current.value);

  // If the path isn't the root path,
  // show a button to navigate to the previous
  // directory.
  if (req.url !== "/files/") {
    items.value.push({
      name: "..",
      url: urlUtil.removeLastDir(req.url) + "/",
    });
  }

  // If this folder is empty, finish here.
  if (req.items === null) return;

  // Otherwise we add every directory to the
  // move options.
  for (const item of req.items) {
    if (!item.isDir) continue;
    if (props.exclude?.includes(item.url)) continue;

    items.value.push({
      name: item.name,
      url: item.url,
    });
  }
};

const abortOngoingNext = () => {
  nextAbortController.abort();
};

const next = (event: Event) => {
  // Retrieves the URL of the directory the user
  // just clicked in and fill the options with its
  // content.
  const uri = (event.currentTarget as HTMLElement).dataset.url;
  abortOngoingNext();
  nextAbortController = new AbortController();
  files
    .fetch(uri!, nextAbortController.signal)
    .then(fillOptions)
    .catch((e: unknown) => {
      if (e instanceof StatusError && e.is_canceled) {
        return;
      }
      $showError(e as Error);
    });
};

const touchstart = (event: Event) => {
  const url = (event.currentTarget as HTMLElement).dataset.url;

  // In 300 milliseconds, we shall reset the count.
  setTimeout(() => {
    touches.value.count = 0;
  }, 300);

  // If the element the user is touching
  // is different from the last one he touched,
  // reset the count.
  if (touches.value.id !== url) {
    touches.value.id = url!;
    touches.value.count = 1;
    return;
  }

  touches.value.count++;

  // If there is more than one touch already,
  // open the next screen.
  if (touches.value.count > 1) {
    next(event);
  }
};

const itemClick = (event: Event) => {
  if (authStore.user?.singleClick) next(event);
  else select(event);
};

const select = (event: Event) => {
  const url = (event.currentTarget as HTMLElement).dataset.url;
  // If the element is already selected, unselect it.
  if (selected.value === url) {
    selected.value = null;
    emit("update:selected", current.value);
    return;
  }

  // Otherwise select the element.
  selected.value = url!;
  emit("update:selected", selected.value);
};

const createDir = () => {
  layoutStore.showHover({
    prompt: "newDir",
    action: undefined,
    confirm: (url: string) => {
      const paths = url.split("/");
      items.value.push({
        name: paths[paths.length - 2],
        url: url,
      });
    },
    props: {
      redirect: false,
      base: current.value === route.path ? null : current.value,
    },
  });
};

onMounted(() => {
  if (fileStore.req) {
    fillOptions(fileStore.req);
  }
});

onUnmounted(() => {
  abortOngoingNext();
});

defineExpose({ createDir });
</script>
