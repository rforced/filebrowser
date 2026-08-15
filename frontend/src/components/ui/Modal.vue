<template>
  <Teleport to="body">
    <div
      data-component="modal"
      class="fixed inset-0 bg-gray-400/60 dark:bg-gray-900/70 backdrop-blur-xs z-100 flex justify-center items-center overscroll-none p-4 isolate"
      tabindex="-1"
      role="dialog"
      aria-modal="true"
      :aria-label="title"
      @mousedown.self="close"
    >
      <Card
        ref="card"
        class="relative w-full max-h-full overflow-y-auto overscroll-contain flex flex-col"
        :class="WIDTHS[size]"
      >
        <div
          v-if="title"
          class="flex gap-4 items-center justify-between px-6 py-4 border-b border-gray-200 dark:border-gray-700"
        >
          <h3 class="text-lg font-medium text-gray-900 dark:text-gray-100">
            {{ title }}
          </h3>
        </div>

        <slot />

        <button
          v-if="closeButton"
          type="button"
          class="absolute top-3 right-3 w-7 h-7 flex items-center justify-center text-gray-400 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200 hover:bg-gray-200 dark:hover:bg-gray-700 rounded-md transition"
          :aria-label="t('buttons.close')"
          @click="close"
        >
          <i class="fa-solid fa-times text-lg"></i>
        </button>
      </Card>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import Card from "@/components/ui/Card.vue";

type ModalSize = "sm" | "md" | "lg" | "xl";

withDefaults(
  defineProps<{
    title?: string;
    size?: ModalSize;
    closeButton?: boolean;
  }>(),
  { title: "", size: "md", closeButton: true }
);

const emit = defineEmits<{ (e: "closed"): void }>();

const { t } = useI18n();

const card = ref<InstanceType<typeof Card> | null>(null);

const WIDTHS: Record<ModalSize, string> = {
  sm: "sm:w-96",
  md: "sm:w-2/3 lg:w-1/2 xl:w-1/3",
  lg: "sm:w-5/6 lg:w-3/4 xl:w-1/2",
  xl: "sm:w-11/12 lg:w-5/6",
};

const close = () => emit("closed");

const onKeydown = (event: KeyboardEvent) => {
  if (event.key === "Escape") {
    event.stopPropagation();
    close();
  }
};

onMounted(() => {
  window.addEventListener("keydown", onKeydown);

  const preferred = document.querySelector<HTMLElement>("#focus-prompt");
  if (preferred) {
    preferred.focus();
  } else {
    (card.value?.$el as HTMLElement | undefined)?.focus?.();
  }
});

onBeforeUnmount(() => {
  window.removeEventListener("keydown", onKeydown);
});
</script>
