<template>
  <div
    id="log-viewer-container"
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
      class="flex flex-wrap gap-x-4 gap-y-2 items-center px-3 md:px-6 py-2 bg-white dark:bg-gray-800 border-b border-gray-200 dark:border-gray-700 shrink-0"
    >
      <button
        type="button"
        class="btn btn-sm btn-flex btn-white btn-soft"
        :aria-label="following ? t('buttons.pause') : t('buttons.follow')"
        @click="toggleFollow"
      >
        <i class="fa-solid" :class="following ? 'fa-pause' : 'fa-play'"></i>
        <span>{{ following ? t("buttons.pause") : t("buttons.follow") }}</span>
      </button>

      <span
        class="inline-flex items-center gap-1.5 text-xs font-medium"
        :class="
          following
            ? 'text-green-700 dark:text-green-400'
            : 'text-gray-500 dark:text-gray-400'
        "
      >
        <i
          class="fa-solid fa-circle text-[0.5rem]"
          :class="{ 'animate-pulse': following }"
        ></i>
        {{ following ? t("logView.live") : t("logView.paused") }}
      </span>

      <span class="text-xs text-gray-500 dark:text-gray-400 tabular-nums">
        {{ filesize(total) }}
        <template v-if="truncatedHead">
          · {{ t("logView.showingLast", { size: filesize(text.length) }) }}
        </template>
      </span>

      <span v-if="gone" class="text-xs text-amber-700 dark:text-amber-400">
        {{ t("logView.fileGone") }}
      </span>
    </div>

    <div
      ref="scroller"
      class="flex-1 min-h-0 overflow-y-auto overscroll-contain"
      @scroll.passive="onScroll"
    >
      <pre
        class="px-3 md:px-6 py-3 font-mono text-xs leading-5 whitespace-pre-wrap break-all text-gray-800 dark:text-gray-200"
        >{{ text }}</pre>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, nextTick, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";

import { files as api } from "@/api";
import { useAuthStore } from "@/stores/auth";
import { useFileStore } from "@/stores/file";
import { filesize } from "@/utils";
import url from "@/utils/url";
import {
  capBuffer,
  parseContentRange,
  parseUnsatisfiedRange,
  trimToLine,
} from "@/utils/logTail";

const TAIL_BYTES = 64 * 1024;
const BUFFER_CAP = 2 * 1024 * 1024;
const POLL_MS = 2000;

const authStore = useAuthStore();
const fileStore = useFileStore();
const route = useRoute();
const router = useRouter();
const { t } = useI18n();

const text = ref("");
const total = ref(0);
const truncatedHead = ref(false);
const following = ref(true);
const gone = ref(false);

const scroller = ref<HTMLDivElement | null>(null);

// Byte offset of the next unread chunk; string lengths cannot track this, so
// it is advanced from the raw buffers.
let offset = 0;
let timer: number | null = null;
// One streaming decoder across appends, so a UTF-8 sequence split between two
// polls still decodes whole.
let decoder = new TextDecoder("utf-8");
let atBottom = true;

const downloadUrl = computed(() =>
  fileStore.req ? api.getDownloadURL(fileStore.req, false) : ""
);

const rawUrl = computed(() =>
  fileStore.req ? api.getDownloadURL(fileStore.req, true) : ""
);

const canOpenAsText = computed(
  () =>
    fileStore.req?.type === "text" || fileStore.req?.type === "textImmutable"
);

const fetchRange = (range: string) =>
  fetch(rawUrl.value, { headers: { Range: range }, cache: "no-store" });

const stickToBottom = async () => {
  if (!atBottom) return;
  await nextTick();
  const el = scroller.value;
  if (el) el.scrollTop = el.scrollHeight;
};

const loadTail = async () => {
  const res = await fetchRange(`bytes=-${TAIL_BYTES}`);
  if (res.status === 404) {
    gone.value = true;
    stopPolling();
    return;
  }
  if (!res.ok && res.status !== 206) return;

  const buf = await res.arrayBuffer();
  decoder = new TextDecoder("utf-8");
  let tail = decoder.decode(buf, { stream: true });

  const range = parseContentRange(res.headers.get("Content-Range"));
  if (range) {
    offset = range.end + 1;
    total.value = range.total;
    truncatedHead.value = range.start > 0;
    if (range.start > 0) tail = trimToLine(tail);
  } else {
    offset = buf.byteLength;
    total.value = buf.byteLength;
    truncatedHead.value = false;
  }

  text.value = tail;
  gone.value = false;
  stickToBottom();
};

const poll = async () => {
  try {
    const res = await fetchRange(`bytes=${offset}-`);

    if (res.status === 404) {
      gone.value = true;
      stopPolling();
      return;
    }

    if (res.status === 416) {
      const size = parseUnsatisfiedRange(res.headers.get("Content-Range"));
      if (size !== null && size < offset) {
        // The file shrank — a fresh run truncated it. Start over from its
        // tail rather than appending from beyond its end.
        await loadTail();
      } else if (size !== null) {
        total.value = size;
      }
      return;
    }

    if (res.status === 206) {
      const range = parseContentRange(res.headers.get("Content-Range"));
      if (range && range.start !== offset) {
        await loadTail();
        return;
      }

      const buf = await res.arrayBuffer();
      if (buf.byteLength === 0) return;

      text.value += decoder.decode(buf, { stream: true });
      offset += buf.byteLength;
      if (range) total.value = range.total;

      if (text.value.length > BUFFER_CAP) {
        text.value = capBuffer(text.value, BUFFER_CAP);
        truncatedHead.value = true;
      }
      stickToBottom();
      return;
    }

    if (res.ok) {
      // The server answered with the whole file; resynchronize from its tail.
      await loadTail();
    }
  } catch {
    // A dropped poll is retried on the next tick.
  }
};

const startPolling = () => {
  if (timer !== null) return;
  timer = window.setInterval(poll, POLL_MS);
};

const stopPolling = () => {
  if (timer !== null) {
    clearInterval(timer);
    timer = null;
  }
};

const toggleFollow = () => {
  following.value = !following.value;
  if (following.value && !gone.value) {
    atBottom = true;
    poll();
    startPolling();
    stickToBottom();
  } else {
    stopPolling();
  }
};

const onScroll = () => {
  const el = scroller.value;
  if (!el) return;
  atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
};

// Polling an idle background tab is wasted work; the next poll after
// returning catches everything up in one range request.
const onVisibility = () => {
  if (document.hidden) {
    stopPolling();
  } else if (following.value && !gone.value) {
    poll();
    startPolling();
  }
};

const keyEvent = (event: KeyboardEvent) => {
  if (event.code === "Escape") close();
};

onMounted(async () => {
  window.addEventListener("keydown", keyEvent);
  document.addEventListener("visibilitychange", onVisibility);
  await loadTail();
  if (following.value && !gone.value) startPolling();
});

onBeforeUnmount(() => {
  window.removeEventListener("keydown", keyEvent);
  document.removeEventListener("visibilitychange", onVisibility);
  stopPolling();
});

const openAsText = () => {
  router.replace({ query: { ...route.query, view: undefined } });
};

const close = () => {
  const uri = url.removeLastDir(route.path) + "/";
  router.push({ path: uri });
};
</script>
