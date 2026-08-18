<template>
  <div class="flex flex-col gap-1.5">
    <button
      type="button"
      class="flex items-center gap-1.5 text-xs font-medium text-gray-600 dark:text-gray-300 hover:text-gray-900 dark:hover:text-gray-100 transition self-start"
      @click="toggle"
    >
      <i
        class="fa-solid text-[0.6rem]"
        :class="open ? 'fa-angle-down' : 'fa-angle-right'"
      ></i>
      {{ t("converge.restartPoints", { count: restarts.length }) }}
    </button>

    <div v-if="open" class="flex flex-col gap-1">
      <p
        v-if="mismatch"
        class="text-xs text-amber-700 dark:text-amber-400 flex items-start gap-1.5"
      >
        <i class="fa-solid fa-triangle-exclamation mt-0.5 shrink-0"></i>
        <span>{{ mismatch }}</span>
      </p>

      <p
        v-if="restarts.length > probed.length"
        class="text-xs text-gray-500 dark:text-gray-400"
      >
        {{
          t("converge.restartsShown", {
            shown: probed.length,
            total: restarts.length,
          })
        }}
      </p>

      <button
        v-for="entry in entries"
        :key="entry.info.path"
        type="button"
        class="flex flex-wrap items-center gap-x-3 gap-y-0.5 px-2 py-1.5 rounded-md text-xs border border-gray-200 dark:border-gray-700 hover:bg-gray-100 dark:hover:bg-gray-700 transition text-left"
        @click="openRestart(entry.info.path)"
      >
        <i
          class="fa-solid fa-rotate-right text-[0.6rem] text-yellow-700 dark:text-yellow-500 shrink-0"
        ></i>

        <span class="font-medium text-gray-900 dark:text-gray-100">
          {{ entry.info.name }}
        </span>

        <span
          v-if="entry.meta?.time"
          class="tabular-nums text-gray-600 dark:text-gray-300"
        >
          {{
            formatSimTime(entry.meta.time.value, entry.meta.time.unit ?? unit)
          }}
        </span>

        <span
          v-if="entry.meta?.cycle !== undefined"
          class="tabular-nums text-gray-500 dark:text-gray-400"
        >
          {{ t("converge.cycleN", { n: formatCount(entry.meta.cycle) }) }}
        </span>

        <span
          v-if="entry.meta?.solver"
          class="text-gray-500 dark:text-gray-400"
          :class="entry.solverOdd ? 'text-amber-700 dark:text-amber-400' : ''"
        >
          {{ entry.meta.solver }}
        </span>

        <span
          v-if="entry.meta?.ranks !== undefined"
          class="tabular-nums"
          :class="
            entry.ranksOdd
              ? 'text-amber-700 dark:text-amber-400'
              : 'text-gray-500 dark:text-gray-400'
          "
        >
          {{ t("converge.ranksN", { n: entry.meta.ranks }) }}
        </span>

        <span class="tabular-nums text-gray-400 dark:text-gray-500 ml-auto">
          {{ filesize(entry.info.size) }}
        </span>

        <i
          v-if="entry.loading"
          class="fa-solid fa-spinner fa-spin text-[0.6rem] text-gray-400"
        ></i>
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRouter } from "vue-router";

import type { ConvergeRestartInfo } from "@/api/files";
import * as h5 from "@/api/h5";
import { filesize } from "@/utils";
import { formatCount, formatSimTime } from "@/utils/convergeH5";

// Reading a restart's root attributes touches the superblock and one object
// header, not the hundreds of megabytes behind them, so probing a handful is
// cheap. Still bounded: a long chain can accumulate many.
const MAX_PROBED = 12;

const props = defineProps<{
  restarts: ConvergeRestartInfo[];
  // Restarts record a sim time but no unit, so the deck's crank_flag — which
  // the case summary already resolved — is the only thing that can label it.
  unit?: "s" | "deg";
}>();

const { t } = useI18n({});
const router = useRouter();

const open = ref(false);
const meta = ref<Map<string, h5.H5Summary>>(new Map());
const pending = ref<Set<string>>(new Set());

const probed = computed(() => props.restarts.slice(0, MAX_PROBED));

const entries = computed(() =>
  probed.value.map((info) => {
    const m = meta.value.get(info.path);
    return {
      info,
      meta: m,
      loading: pending.value.has(info.path),
      // A restart written by a different build or for a different rank count
      // than its siblings is the thing worth noticing before resubmitting.
      solverOdd: m?.solver !== undefined && m.solver !== majority.value.solver,
      ranksOdd: m?.ranks !== undefined && m.ranks !== majority.value.ranks,
    };
  })
);

const majority = computed(() => {
  const solvers = new Map<string, number>();
  const ranks = new Map<number, number>();
  for (const m of meta.value.values()) {
    if (m.solver) solvers.set(m.solver, (solvers.get(m.solver) ?? 0) + 1);
    if (m.ranks !== undefined)
      ranks.set(m.ranks, (ranks.get(m.ranks) ?? 0) + 1);
  }
  const top = <T,>(counts: Map<T, number>): T | undefined =>
    [...counts.entries()].sort((a, b) => b[1] - a[1])[0]?.[0];
  return { solver: top(solvers), ranks: top(ranks) };
});

const mismatch = computed(() => {
  const solvers = new Set(
    [...meta.value.values()].map((m) => m.solver).filter(Boolean)
  );
  const ranks = new Set(
    [...meta.value.values()].map((m) => m.ranks).filter((r) => r !== undefined)
  );
  if (solvers.size > 1) {
    return t("converge.restartSolverMismatch", {
      versions: [...solvers].join(", "),
    });
  }
  if (ranks.size > 1) {
    return t("converge.restartRankMismatch", { counts: [...ranks].join(", ") });
  }
  return "";
});

const probe = async () => {
  await Promise.all(
    probed.value.map(async (info) => {
      if (meta.value.has(info.path) || pending.value.has(info.path)) return;
      pending.value = new Set(pending.value).add(info.path);
      try {
        const summary = await h5.summary(info.path);
        meta.value = new Map(meta.value).set(info.path, summary);
      } catch {
        // A restart we cannot read still lists by name, size and mtime.
      } finally {
        const next = new Set(pending.value);
        next.delete(info.path);
        pending.value = next;
      }
    })
  );
};

const toggle = () => {
  open.value = !open.value;
  if (open.value) probe();
};

const openRestart = (path: string) => router.push({ path: `/files${path}` });
</script>
