<template>
  <Card class="flex flex-col gap-4 p-6">
    <div class="flex flex-wrap items-baseline justify-between gap-3">
      <h2 class="text-lg font-medium text-gray-900 dark:text-gray-100">
        {{ t("files.usageTitle", { name: dirName }) }}
      </h2>

      <button
        type="button"
        class="btn btn-blue btn-soft text-sm"
        @click="close"
        :aria-label="t('buttons.close')"
      >
        {{ t("buttons.close") }}
      </button>
    </div>

    <!--
      Both totals, always. On a compressed filesystem the on-disk number is the
      one that decides whether to clean up or pay to expand, but it disagrees
      with every other size the user has ever seen for these files, so the
      content size and the ratio have to sit next to it.
    -->
    <p
      v-if="breakdown"
      class="text-sm text-gray-600 dark:text-gray-300"
      data-testid="usage-summary"
    >
      {{ summary }}
    </p>

    <div v-if="loading" class="flex items-center gap-3 py-8 justify-center">
      <i class="fa-solid fa-spinner fa-spin text-2xl text-gray-400"></i>
      <span class="text-sm text-gray-600 dark:text-gray-300">
        {{ t("files.usageScanning") }}
      </span>
    </div>

    <p v-else-if="error" class="text-sm text-red-600 dark:text-red-400 py-4">
      {{ error }}
    </p>

    <p
      v-else-if="!breakdown?.children.length"
      class="text-sm text-gray-600 dark:text-gray-300 py-4"
    >
      {{ t("files.usageEmpty") }}
    </p>

    <template v-else>
      <ul class="flex flex-col">
        <li
          v-for="child in breakdown.children"
          :key="child.name"
          class="border-b border-gray-200 dark:border-gray-700 last:border-0"
        >
          <component
            :is="child.isDir ? 'button' : 'div'"
            :type="child.isDir ? 'button' : undefined"
            class="w-full flex items-center gap-3 py-2.5 text-left"
            :class="
              child.isDir
                ? 'cursor-pointer hover:bg-gray-100 dark:hover:bg-gray-700 px-2 -mx-2 rounded'
                : 'px-2 -mx-2'
            "
            @click="child.isDir ? descend(child) : undefined"
          >
            <i
              class="fa-solid w-4 shrink-0 text-center"
              :class="
                child.isDir
                  ? 'fa-folder text-blue-500'
                  : 'fa-file text-gray-400'
              "
            ></i>

            <span class="flex-1 min-w-0">
              <span class="flex items-baseline justify-between gap-3">
                <span
                  class="truncate"
                  :class="child.isDir ? 'font-medium' : ''"
                >
                  {{ child.name }}
                </span>
                <span
                  class="text-sm tabular-nums shrink-0 text-gray-600 dark:text-gray-300"
                  :title="rowTitle(child)"
                >
                  {{ filesize(child.size) }}
                </span>
              </span>

              <!--
                Bars are scaled against the biggest row, not the total, so a
                long tail of small directories stays legible instead of
                collapsing into invisible slivers under one dominant case.
              -->
              <span
                class="mt-1 block h-1.5 rounded-full bg-gray-200 dark:bg-gray-700 overflow-hidden"
              >
                <span
                  class="block h-full rounded-full"
                  :class="child.isDir ? 'bg-blue-500' : 'bg-gray-400'"
                  :style="{
                    width: `${usageFraction(child.size, largest) * 100}%`,
                  }"
                ></span>
              </span>
            </span>
          </component>
        </li>
      </ul>

      <!-- CONVERGE rollup: the same bytes sliced by output family. -->
      <template v-if="kinds.length">
        <div class="flex flex-col gap-1 pt-2">
          <h3 class="text-sm font-medium text-gray-900 dark:text-gray-100">
            {{ t("files.usageKinds") }}
          </h3>
          <p class="text-xs text-gray-600 dark:text-gray-300">
            {{ t("files.usageKindsHint") }}
          </p>
        </div>

        <!--
          The chips only become buttons inside a case directory. The clean
          endpoint works on one case at a time, so offering the handoff from a
          folder that merely contains cases would open a prompt that can only
          fail.
        -->
        <ul class="flex flex-wrap gap-2">
          <li v-for="kind in kinds" :key="kind.kind">
            <component
              :is="isCase ? 'button' : 'span'"
              :type="isCase ? 'button' : undefined"
              class="flex items-baseline gap-2 rounded-full border border-gray-200 dark:border-gray-700 px-3 py-1 text-sm"
              :class="
                isCase
                  ? 'hover:bg-gray-100 dark:hover:bg-gray-700 cursor-pointer'
                  : ''
              "
              :title="
                isCase
                  ? `${rowTitle(kind)}\n${t('files.usageCleanKind')}`
                  : rowTitle(kind)
              "
              @click="isCase ? cleanKind(kind) : undefined"
            >
              <span class="font-medium">{{
                convergeKindLabel(kind.kind)
              }}</span>
              <span class="tabular-nums text-gray-600 dark:text-gray-300">
                {{ filesize(kind.size) }}
              </span>
              <span class="text-xs text-gray-500">({{ kind.count }})</span>
            </component>
          </li>
        </ul>
      </template>
    </template>
  </Card>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";

import { useFileStore } from "@/stores/file";
import { useLayoutStore } from "@/stores/layout";
import { useUsageStore } from "@/stores/usage";
import { filesize } from "@/utils";
import { compressionRatio, usageFraction } from "@/utils/usage";
import { convergeKindLabel } from "@/utils/convergeKinds";
import { cachedConvergeSummary } from "@/utils/convergeSummaryCache";
import Card from "@/components/ui/Card.vue";

const { t } = useI18n();
const route = useRoute();
const router = useRouter();

const fileStore = useFileStore();
const layoutStore = useLayoutStore();
const usageStore = useUsageStore();

const breakdown = ref<UsageBreakdown | null>(null);
const loading = ref(false);
const error = ref("");
const isCase = ref(false);

let controller = new AbortController();

const dirName = computed(() => fileStore.req?.name || "/");

const largest = computed(() =>
  breakdown.value?.children.length ? breakdown.value.children[0].size : 0
);

/*
 * The kind rollup is only meaningful where there is CONVERGE output to roll up.
 * A directory of ordinary files would produce a single "other" chip, which is
 * noise, so it is suppressed.
 */
const kinds = computed(() => {
  const all = breakdown.value?.kinds ?? [];
  const named = all.filter((k) => k.kind !== "other");
  return named.length ? all : [];
});

const summary = computed(() => {
  const b = breakdown.value;
  if (!b) return "";

  const ratio = compressionRatio(b);
  if (ratio) {
    return t("files.usageSummary", {
      size: filesize(b.size),
      logical: filesize(b.logicalSize),
      ratio,
    });
  }

  return t("files.usageSummaryPlain", {
    size: filesize(b.size),
    files: b.numFiles,
    dirs: b.numDirs,
  });
});

const rowTitle = (row: { size: number; logicalSize: number }) => {
  const parts = [
    t("files.usageOnDisk", { size: filesize(row.size) }),
    t("files.usageLogical", { size: filesize(row.logicalSize) }),
  ];
  const ratio = compressionRatio(row);
  if (ratio) parts.push(t("files.usageRatio", { ratio }));
  return parts.join("\n");
};

/*
 * Drop the rows the moment the route changes rather than waiting for the new
 * listing to arrive. The store's path lags the router by a fetch, and rows left
 * on screen in that gap belong to the directory we just left — descending
 * through one of them would append its name to the path we already moved to.
 */
const clear = () => {
  controller.abort();
  breakdown.value = null;
  error.value = "";
  isCase.value = false;
  loading.value = true;
};

const load = async () => {
  const path = fileStore.req?.path;
  if (!path) return;

  controller.abort();
  controller = new AbortController();

  loading.value = true;
  error.value = "";
  isCase.value = false;

  // Cheap and cached, and it only decides whether the kind chips are
  // actionable, so a failure here must not take the breakdown down with it.
  cachedConvergeSummary(route.path, controller.signal)
    .then((summary) => {
      isCase.value = summary.isCase;
    })
    .catch(() => {
      isCase.value = false;
    });

  try {
    breakdown.value = await usageStore.breakdown(path, {
      kinds: true,
      signal: controller.signal,
    });
  } catch (e) {
    if (!controller.signal.aborted) {
      error.value = (e as Error).message || t("files.usageFailed");
    }
  } finally {
    if (!controller.signal.aborted) loading.value = false;
  }
};

const descend = (child: UsageEntry) => {
  const base = route.path.endsWith("/") ? route.path : `${route.path}/`;
  router.push({
    path: `${base}${encodeURIComponent(child.name)}/`,
    query: { view: "usage" },
  });
};

const close = () => {
  const query = { ...route.query };
  delete query.view;
  router.push({ path: route.path, query });
};

// Hands the selected family to the clean prompt, which already knows how to
// price and execute a sweep by kind.
const cleanKind = (kind: UsageKind) => {
  layoutStore.showHover({
    prompt: "converge-clean",
    props: { kind: kind.kind },
  });
};

watch(() => route.path, clear);
watch(() => fileStore.req?.path, load, { immediate: true });

onUnmounted(() => controller.abort());
</script>
