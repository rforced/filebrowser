<template>
  <div
    v-if="toastStore.items.length"
    data-component="toasts"
    class="fixed right-4 bottom-4 z-110 flex flex-col gap-3"
    style="width: min(400px, 100% - 2rem)"
    role="status"
    aria-live="polite"
  >
    <TransitionGroup
      enter-active-class="transition ease-out duration-300"
      enter-from-class="opacity-0 translate-y-2"
      enter-to-class="opacity-100 translate-y-0"
      leave-active-class="transition ease-in duration-200 absolute"
      leave-from-class="opacity-100"
      leave-to-class="opacity-0"
    >
      <Card
        v-for="toast in toastStore.items"
        :key="toast.id"
        class="flex gap-3 items-start px-4 py-3 text-gray-800 dark:text-gray-100 wrap-break-word ring-3"
        :class="RING[toast.severity]"
      >
        <i class="fa-solid mt-0.5 shrink-0" :class="ICON[toast.severity]"></i>

        <div class="flex-1 min-w-0 text-sm break-words">
          {{ toast.message }}
        </div>

        <button
          type="button"
          class="shrink-0 w-6 h-6 flex items-center justify-center rounded-md text-gray-500 dark:text-gray-400 hover:text-gray-800 dark:hover:text-gray-100 hover:bg-gray-200 dark:hover:bg-gray-700 transition"
          :aria-label="t('buttons.close')"
          @click="toastStore.dismiss(toast.id)"
        >
          <i class="fa-solid fa-times"></i>
        </button>
      </Card>
    </TransitionGroup>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from "vue-i18n";
import { useToastStore, type ToastSeverity } from "@/stores/toast";
import Card from "@/components/ui/Card.vue";

const { t } = useI18n();
const toastStore = useToastStore();

const RING: Record<ToastSeverity, string> = {
  success: "ring-green-500/70",
  error: "ring-red-500/70",
  info: "ring-blue-500/70",
};

const ICON: Record<ToastSeverity, string> = {
  success: "fa-circle-check text-green-600 dark:text-green-400",
  error: "fa-circle-exclamation text-red-600 dark:text-red-300",
  info: "fa-circle-info text-blue-500 dark:text-teal",
};
</script>
