<template>
  <div ref="root" class="relative">
    <div class="relative">
      <i
        class="fa-solid absolute left-3 top-1/2 -translate-y-1/2 text-gray-500 dark:text-gray-400 text-sm pointer-events-none"
        :class="ongoing ? 'fa-spinner fa-spin' : 'fa-search'"
      ></i>

      <input
        ref="input"
        v-model.trim="prompt"
        type="search"
        class="form-control pl-9"
        :class="active && results.length ? 'rounded-b-none' : ''"
        :aria-label="t('search.search')"
        :placeholder="t('search.search')"
        @focus="open"
        @keyup.exact="keyup"
        @keyup.enter="submit"
      />

      <button
        v-if="ongoing"
        v-tooltip="t('buttons.stopSearch')"
        type="button"
        class="absolute right-2 top-1/2 -translate-y-1/2 w-6 h-6 flex items-center justify-center rounded-md text-gray-500 dark:text-gray-400 hover:text-gray-800 dark:hover:text-gray-100"
        :aria-label="t('buttons.stopSearch')"
        @click="stop"
      >
        <i class="fa-solid fa-circle-stop"></i>
      </button>
    </div>

    <!-- Results panel, styled after Horizon's dropdown panels. -->
    <div
      v-if="active"
      class="absolute left-0 right-0 top-full max-h-[70svh] overflow-y-auto overscroll-contain border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-900 text-gray-900 dark:text-gray-100 rounded-b-md shadow-lg z-50"
    >
      <ul v-if="results.length">
        <li
          v-for="(result, index) in filteredResults"
          :key="index"
          class="border-b border-gray-100 dark:border-gray-800 last:border-0"
        >
          <router-link
            :to="result.url"
            class="flex items-center gap-2.5 px-3 py-2 text-sm hover:bg-blue-500 hover:text-white transition"
            @click="close"
          >
            <i
              class="fa-solid fa-fw shrink-0"
              :class="result.dir ? 'fa-folder' : 'fa-file'"
            ></i>
            <span class="truncate">./{{ result.path }}</span>
          </router-link>
        </li>

        <li
          v-if="results.length > filteredResults.length"
          class="px-3 py-2 text-xs text-gray-500 dark:text-gray-400"
        >
          {{
            t("search.showingResults", {
              shown: filteredResults.length,
              total: results.length,
            })
          }}
        </li>
      </ul>

      <div v-else class="p-3 flex flex-col gap-3">
        <p v-if="text" class="text-sm text-gray-600 dark:text-gray-300">
          {{ text }}
        </p>

        <p
          class="flex items-center gap-1.5 text-xs text-gray-500 dark:text-gray-400"
        >
          <i class="fa-solid fa-folder-open fa-fw shrink-0"></i>
          <span class="truncate">{{
            t("search.scope", { path: scopeLabel })
          }}</span>
        </p>

        <div v-if="prompt.length === 0" class="flex flex-col gap-2">
          <h3
            class="text-xs font-medium text-gray-700 dark:text-gray-200 uppercase tracking-wider"
          >
            {{ t("search.types") }}
          </h3>
          <div class="flex flex-wrap gap-2">
            <button
              v-for="(box, key) in BOXES"
              :key="key"
              type="button"
              class="btn btn-flex btn-white btn-soft btn-sm"
              :aria-label="t('search.' + box.label)"
              @click="init('type:' + key)"
            >
              <i class="fa-solid" :class="box.icon"></i>
              <span>{{ t("search." + box.label) }}</span>
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, inject, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";
import { storeToRefs } from "pinia";

import { useFileStore } from "@/stores/file";
import { useLayoutStore } from "@/stores/layout";
import url from "@/utils/url";
import { search } from "@/api";
import { StatusError } from "@/api/utils";

const BOXES = {
  inputs: { label: "inputs", icon: "fa-file-pen" },
  outputs: { label: "outputs", icon: "fa-chart-line" },
  logs: { label: "logs", icon: "fa-file-lines" },
  restarts: { label: "restarts", icon: "fa-rotate-right" },
  image: { label: "images", icon: "fa-image" },
};

const layoutStore = useLayoutStore();
const fileStore = useFileStore();
const { currentPromptName } = storeToRefs(layoutStore);

const { t } = useI18n();
const route = useRoute();
const $showError = inject<IToastError>("$showError")!;

const root = ref<HTMLElement | null>(null);
const input = ref<HTMLInputElement | null>(null);

const prompt = ref("");
const active = ref(false);
const ongoing = ref(false);
const results = ref<any[]>([]);
const reload = ref(false);
const resultsCount = ref(50);

let controller = new AbortController();

const filteredResults = computed(() =>
  results.value.slice(0, resultsCount.value)
);

const text = computed(() => {
  if (ongoing.value) return "";
  return prompt.value === ""
    ? t("search.typeToSearch")
    : t("search.pressToSearch");
});

const scopePath = computed(() =>
  fileStore.isListing ? route.path : url.removeLastDir(route.path) + "/"
);

const scopeLabel = computed(() => {
  const parts = scopePath.value.split("/").filter((part) => part !== "");
  return parts.length > 1
    ? decodeURIComponent(parts[parts.length - 1])
    : t("files.home");
});

watch(currentPromptName, (newVal, oldVal) => {
  if (newVal === "search") {
    active.value = true;
    reload.value = false;
    input.value?.focus();
  } else if (oldVal === "search") {
    if (reload.value) fileStore.reload = true;
    reset();
    prompt.value = "";
    active.value = false;
    input.value?.blur();
  }
});

watch(prompt, () => reset());

const open = () => {
  if (!active.value) layoutStore.showHover("search");
};

const close = () => {
  if (active.value) layoutStore.closeHovers();
};

const stop = () => {
  abort();
  ongoing.value = false;
};

const keyup = (event: KeyboardEvent) => {
  if (event.key === "Escape") {
    event.stopPropagation();
    close();
    return;
  }
  results.value.length = 0;
};

const init = (value: string) => {
  prompt.value = `${value} `;
  input.value?.focus();
};

const abort = () => controller.abort();

const reset = () => {
  abort();
  ongoing.value = false;
  resultsCount.value = 50;
  results.value = [];
};

const submit = async (event: Event) => {
  event.preventDefault();

  if (prompt.value === "") return;

  ongoing.value = true;

  try {
    abort();
    controller = new AbortController();
    results.value = [];
    await search(scopePath.value, prompt.value, controller.signal, (item) =>
      results.value.push(item)
    );
  } catch (error: any) {
    if (error instanceof StatusError && error.is_canceled) {
      return;
    }
    $showError(error);
  }

  ongoing.value = false;
};

const onPointerDown = (event: PointerEvent) => {
  if (active.value && !root.value?.contains(event.target as Node)) {
    close();
  }
};

onMounted(() => document.addEventListener("pointerdown", onPointerDown));

onBeforeUnmount(() => {
  document.removeEventListener("pointerdown", onPointerDown);
  abort();
});
</script>
