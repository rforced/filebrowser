<template>
  <Card class="p-10">
    <div
      class="flex flex-col items-center gap-3 text-center text-gray-600 dark:text-gray-300"
    >
      <i class="fa-solid text-4xl" :class="[info.icon, info.color]"></i>
      <div class="text-sm font-medium">{{ t(info.message) }}</div>
      <slot />
    </div>
  </Card>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import Card from "@/components/ui/Card.vue";

const { t } = useI18n({});

const errors: {
  [key: number]: { icon: string; message: string; color: string };
} = {
  0: {
    icon: "fa-cloud-arrow-down",
    message: "errors.connection",
    color: "text-gray-500 dark:text-gray-400",
  },
  403: {
    icon: "fa-lock",
    message: "errors.forbidden",
    color: "text-amber-600 dark:text-amber-400",
  },
  404: {
    icon: "fa-location-crosshairs",
    message: "errors.notFound",
    color: "text-gray-500 dark:text-gray-400",
  },
  500: {
    icon: "fa-triangle-exclamation",
    message: "errors.internal",
    color: "text-red-600 dark:text-red-300",
  },
};

const props = withDefaults(defineProps<{ errorCode?: number }>(), {
  errorCode: 500,
});

const info = computed(() => {
  return errors[props.errorCode] ? errors[props.errorCode] : errors[500];
});
</script>
