<template>
  <th
    :scope="scope"
    class="px-3 py-3 first:pl-4 last:pr-4 text-xs font-medium text-gray-700 dark:text-gray-200 uppercase tracking-wider whitespace-nowrap"
    :class="align === 'right' ? 'text-right' : 'text-left'"
  >
    <slot />
  </th>
</template>

<script setup lang="ts">
withDefaults(defineProps<{ scope?: string; align?: "left" | "right" }>(), {
  scope: "col",
  align: "left",
});
</script>
