<template>
  <div
    data-component="card"
    class="@container/card bg-white dark:bg-gray-800 rounded-lg shadow-xs dark:ring-1 dark:ring-gray-700"
    :class="hover ? 'transition hover:shadow-md' : ''"
  >
    <slot />
  </div>
</template>

<script setup lang="ts">
withDefaults(defineProps<{ hover?: boolean }>(), { hover: false });
</script>
