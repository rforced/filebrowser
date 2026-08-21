<template>
  <div class="flex flex-col">
    <div class="card-title">
      <h2>{{ t("prompts.udfCompile") }}</h2>
    </div>

    <div class="px-6 py-4 flex flex-col gap-3">
      <p>{{ t("prompts.udfCompileMessage", { name: dirName }) }}</p>

      <p
        v-if="!loading && info && !info.hasSource"
        class="flex gap-2 items-start text-sm text-amber-700 dark:text-amber-400"
      >
        <i class="fa-solid fa-triangle-exclamation mt-0.5"></i>
        <span>{{ t("prompts.udfNoSource") }}</span>
      </p>

      <label v-if="versions.length > 0" class="flex flex-col gap-1">
        <span class="form-label">{{ t("prompts.udfVersion") }}</span>
        <select v-model="version" class="form-control" tabindex="1">
          <option
            v-for="item in versions"
            :key="item.version"
            :value="item.version"
          >
            {{ item.version }}
          </option>
        </select>
      </label>

      <p class="converge-summary" v-if="loading">
        {{ t("prompts.udfScanning") }}
      </p>
      <p class="converge-summary" v-else-if="!info?.package">
        {{ t("prompts.udfNotPackage") }}
      </p>
      <p class="converge-summary" v-else-if="versions.length === 0">
        {{ t("prompts.udfNoVersions") }}
      </p>
      <p class="converge-summary" v-else>
        {{ t("prompts.udfCompileTotal", { version, name: LIB_NAME }) }}
      </p>
    </div>

    <div
      class="flex flex-wrap justify-end items-center gap-2 px-6 py-4 bg-gray-50 dark:bg-gray-900 rounded-b-lg"
    >
      <button
        @click="layoutStore.closeHovers"
        class="btn btn-white btn-soft"
        :aria-label="t('buttons.cancel')"
        :title="t('buttons.cancel')"
        tabindex="3"
      >
        {{ t("buttons.cancel") }}
      </button>
      <button
        id="focus-prompt"
        @click="submit"
        class="btn btn-blue btn-soft"
        :disabled="!canCompile"
        :aria-label="t('buttons.compileUdf')"
        :title="t('buttons.compileUdf')"
        tabindex="2"
      >
        {{ t("buttons.compileUdf") }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, inject, onActivated, onUnmounted, ref } from "vue";
import { useRoute } from "vue-router";
import { useI18n } from "vue-i18n";
import { files as api } from "@/api";
import type { UdfInfo } from "@/api/files";
import { useUdfStore, udfStartFailure } from "@/stores/udf";
import { useLayoutStore } from "@/stores/layout";

const LIB_NAME = "libconverge_udf.so";

const $showError = inject<IToastError>("$showError")!;

const layoutStore = useLayoutStore();
const udfStore = useUdfStore();
const route = useRoute();
const { t } = useI18n();

const loading = ref(false);
const info = ref<UdfInfo | null>(null);
const version = ref("");

let controller = new AbortController();

const versions = computed(() => info.value?.versions ?? []);

const dirName = computed(() => {
  const parts = route.path.replace(/\/+$/, "").split("/");
  return decodeURIComponent(parts[parts.length - 1] || "/");
});

const canCompile = computed(
  () => !loading.value && !!info.value?.package && version.value !== ""
);

// The prompt lives in a <keep-alive> and is reused across opens, so the lookup
// belongs on activation: the folder being shown may not be the one it was
// opened on last.
onActivated(() => {
  load();
});

onUnmounted(() => {
  controller.abort();
});

const closeIfCurrent = () => {
  if (layoutStore.currentPromptName === "converge-udf") {
    layoutStore.closeHovers();
  }
};

const load = async () => {
  loading.value = true;
  info.value = null;
  version.value = "";

  controller.abort();
  controller = new AbortController();

  try {
    info.value = await api.udfInfo(route.path, controller.signal);
  } catch (e) {
    const error = e as Error & { is_canceled?: boolean };
    if (error.is_canceled) return;
    $showError(error);
    closeIfCurrent();
    return;
  } finally {
    loading.value = false;
  }

  // Rebuilding against what the package was last built against is the common
  // case; the list is newest-first otherwise.
  const last = info.value?.lastVersion;
  const available = info.value?.versions ?? [];
  version.value =
    (last && available.some((item) => item.version === last) ? last : "") ||
    available[0]?.version ||
    "";
};

// The build outlives this prompt: it can run for minutes, and the card it
// reports through is not tied to a modal the user has to sit in front of.
const submit = async () => {
  if (!canCompile.value) return;

  const path = route.path;
  const name = dirName.value;

  try {
    await udfStore.start(path, name, version.value);
  } catch (e) {
    $showError(udfStartFailure(e));
    return;
  }

  closeIfCurrent();
};
</script>
