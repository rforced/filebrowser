<template>
  <div
    data-component="banner"
    class="flex gap-4 items-center justify-between flex-wrap"
  >
    <div class="flex flex-col gap-1 break-words min-w-0">
      <h2 class="text-xl text-blue-600 dark:text-gray-100 font-semibold">
        {{ title }}
      </h2>

      <div
        v-if="subtitle"
        class="text-sm text-gray-700 dark:text-gray-300 font-medium"
      >
        {{ subtitle }}
      </div>
    </div>

    <div class="whitespace-nowrap">
      <slot />
    </div>
  </div>
</template>

<script setup lang="ts">
defineProps<{ title: string; subtitle?: string }>();
</script>
