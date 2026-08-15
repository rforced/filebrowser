<template>
  <div
    data-component="help-box"
    class="flex gap-3 bg-blue-50 dark:bg-gray-800 rounded-lg p-4"
  >
    <i class="fa-solid fa-info-circle text-blue-400 dark:text-teal mt-0.5"></i>

    <div class="flex flex-col gap-2 text-sm min-w-0">
      <h3 class="font-medium text-blue-800 dark:text-gray-100">
        {{ header }}
      </h3>

      <div class="text-blue-700 dark:text-gray-300">
        <slot />
      </div>

      <button
        v-if="action"
        type="button"
        class="inline-block text-left font-medium text-blue-700 dark:text-teal hover:underline"
        @click="emit('action')"
      >
        <span>{{ action }}</span>
        <i class="fa-solid fa-arrow-right ml-1"></i>
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
defineProps<{ header: string; action?: string }>();

const emit = defineEmits<{ (e: "action"): void }>();
</script>
