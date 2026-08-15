<template>
  <tr
    class="text-gray-900 dark:text-gray-100 transition group"
    :class="[
      striped ? 'even:bg-gray-50 dark:even:bg-gray-800/50' : '',
      hover ? 'hover:bg-gray-100 dark:hover:bg-gray-700' : '',
    ]"
  >
    <slot />
  </tr>
</template>

<script setup lang="ts">
withDefaults(defineProps<{ striped?: boolean; hover?: boolean }>(), {
  striped: false,
  hover: true,
});
</script>
