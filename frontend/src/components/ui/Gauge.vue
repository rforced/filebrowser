<template>
  <div data-component="gauge" :class="sizeClass">
    <svg
      viewBox="0 0 128 128"
      class="w-full h-full"
      role="img"
      :aria-label="`${display}%`"
    >
      <circle
        cx="64"
        cy="64"
        r="58"
        class="stroke-gray-200 dark:stroke-gray-700"
        stroke-width="12"
        fill="none"
      />
      <circle
        cx="64"
        cy="64"
        r="58"
        class="transition-all duration-500"
        :class="strokeClass"
        stroke-width="12"
        stroke-linecap="round"
        fill="none"
        :stroke-dasharray="CIRCUMFERENCE"
        :stroke-dashoffset="offset"
        transform="rotate(-90 64 64)"
      />
      <text
        x="64"
        y="64"
        text-anchor="middle"
        dominant-baseline="middle"
        font-size="22"
        font-weight="bold"
        class="fill-gray-900 dark:fill-gray-100"
      >
        {{ display }}%
      </text>
    </svg>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";

const CIRCUMFERENCE = 2 * Math.PI * 58;
const CRITICAL_THRESHOLD = 85;
const WARNING_THRESHOLD = 70;

const props = withDefaults(
  defineProps<{ percentage: number; size?: "sm" | "md" }>(),
  { size: "md" }
);

const clamped = computed(() =>
  Math.min(100, Math.max(0, props.percentage || 0))
);

const display = computed(() => Math.round(clamped.value));

const offset = computed(() => CIRCUMFERENCE * (1 - clamped.value / 100));

const sizeClass = computed(() =>
  props.size === "sm" ? "w-20 h-20" : "w-28 h-28"
);

const strokeClass = computed(() => {
  if (clamped.value >= CRITICAL_THRESHOLD) {
    return "stroke-red-500 dark:stroke-red-400";
  }
  if (clamped.value >= WARNING_THRESHOLD) {
    return "stroke-amber-600 dark:stroke-amber-400";
  }
  return "stroke-blue-500 dark:stroke-teal-600";
});
</script>
