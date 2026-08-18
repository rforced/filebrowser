<template>
  <div v-if="points.length > 1" class="flex flex-col gap-1">
    <div class="flex flex-wrap items-baseline gap-x-3 gap-y-0.5">
      <span class="text-xs font-medium text-gray-600 dark:text-gray-300">
        {{ t("h5View.runProfile") }}
      </span>
      <span class="text-xs text-gray-500 dark:text-gray-400 tabular-nums">
        {{
          projection
            ? t("h5View.writtenSoFar", {
                size: writtenLabel,
                percent: percent,
              })
            : t("h5View.written", { size: writtenLabel })
        }}
      </span>
      <span
        v-if="projection"
        class="text-xs tabular-nums"
        :class="
          projection.projected > projection.written * 1.5
            ? 'text-amber-700 dark:text-amber-400'
            : 'text-gray-500 dark:text-gray-400'
        "
      >
        {{ t("h5View.projectedTotal", { size: projectedLabel }) }}
      </span>
    </div>

    <!--
      Bars, not a line chart: this is a mesh-cost profile at a glance, and each
      bar is a real file the user can open.
    -->
    <div
      class="flex items-end gap-px h-10"
      role="img"
      :aria-label="t('h5View.runProfile')"
    >
      <button
        v-for="p in points"
        :key="p.name"
        type="button"
        class="flex-1 min-w-0 rounded-t-sm bg-purple-400/70 dark:bg-purple-500/60 hover:bg-purple-500 dark:hover:bg-purple-400 transition"
        :style="{ height: `${Math.max(6, (p.size / peak) * 100)}%` }"
        :title="`${p.name} · ${filesize(p.size)}`"
        @click="$emit('open', p.name)"
      ></button>
    </div>

    <div
      class="flex justify-between text-[0.65rem] text-gray-400 dark:text-gray-500 tabular-nums"
    >
      <span>{{ label(points[0].time) }}</span>
      <span>{{ label(points[points.length - 1].time) }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import { filesize } from "@/utils";
import {
  formatSimTime,
  outputProfile,
  projectTotal,
  type OutputPoint,
} from "@/utils/convergeH5";

const props = defineProps<{
  items: { name: string; size: number }[];
  // Deck bounds, used only to project a total. Absent or unusable bounds
  // simply drop the projection rather than guessing.
  start?: number;
  end?: number;
  unit?: "s" | "deg";
}>();

defineEmits<{ open: [name: string] }>();

const { t } = useI18n({});

const points = computed<OutputPoint[]>(() => outputProfile(props.items));

const peak = computed(() => Math.max(...points.value.map((p) => p.size), 1));

const projection = computed(() =>
  props.start !== undefined && props.end !== undefined
    ? projectTotal(points.value, props.start, props.end)
    : null
);

const writtenLabel = computed(() =>
  filesize(points.value.reduce((sum, p) => sum + p.size, 0))
);

const projectedLabel = computed(() =>
  projection.value ? filesize(projection.value.projected) : ""
);

const percent = computed(() =>
  projection.value ? Math.round(projection.value.fraction * 100) : 0
);

// No unit prop means the deck never said which one this case runs in, and
// formatSimTime renders the number bare rather than guessing at it.
const label = (time: number | null) =>
  time === null ? "" : formatSimTime(time, props.unit);
</script>
