<template>
  <span
    data-component="status-badge"
    class="px-2 py-0.5 rounded-full text-sm font-semibold shadow-xs text-slate-800 dark:text-slate-100 transition"
    :class="VARIANTS[status]"
  >
    <slot>{{ display }}</slot>
  </span>
</template>

<script setup lang="ts">
export type BadgeStatus =
  "default" | "primary" | "success" | "warning" | "danger" | "interactive";

withDefaults(defineProps<{ status?: BadgeStatus; display?: string }>(), {
  status: "default",
  display: "",
});

const VARIANTS: Record<BadgeStatus, string> = {
  default: "bg-slate-200 dark:bg-slate-700",
  primary: "bg-sky-300 dark:bg-sky-700",
  success: "bg-emerald-300 dark:bg-emerald-700",
  warning: "bg-amber-300 dark:bg-amber-700",
  danger: "bg-rose-300 dark:bg-rose-700",
  interactive: "bg-purple-300 dark:bg-purple-700",
};
</script>
