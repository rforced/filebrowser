<template>
  <div
    class="shadow-xs ring-1 ring-black/5 dark:ring-white/10 overflow-x-auto bg-white dark:bg-gray-800"
    :class="rounded ? 'rounded-lg' : ''"
  >
    <table class="min-w-full divide-y divide-gray-200 dark:divide-gray-700">
      <slot />
    </table>
  </div>
</template>

<script setup lang="ts">
withDefaults(defineProps<{ rounded?: boolean }>(), { rounded: true });
</script>
