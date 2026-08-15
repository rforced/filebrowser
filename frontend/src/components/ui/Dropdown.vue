<template>
  <div ref="root" class="relative">
    <slot name="trigger" :open="open" :toggle="toggle" />

    <Transition
      enter-active-class="transition ease-out duration-150"
      enter-from-class="opacity-0 scale-95"
      enter-to-class="opacity-100 scale-100"
      leave-active-class="transition ease-in duration-100"
      leave-from-class="opacity-100 scale-100"
      leave-to-class="opacity-0 scale-95"
    >
      <div
        v-if="open"
        class="absolute mt-1 min-w-full w-max max-w-[80svw] overflow-hidden border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-900 text-gray-900 dark:text-gray-100 py-1 rounded-md shadow-lg z-50"
        :class="align === 'right' ? 'right-0' : 'left-0'"
        @click="close"
      >
        <slot :close="close" />
      </div>
    </Transition>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from "vue";

withDefaults(defineProps<{ align?: "left" | "right" }>(), { align: "left" });

const open = ref(false);
const root = ref<HTMLElement | null>(null);

const close = () => (open.value = false);
const toggle = () => (open.value = !open.value);

const onDocumentPointerDown = (event: PointerEvent) => {
  if (open.value && !root.value?.contains(event.target as Node)) {
    close();
  }
};

const onKeydown = (event: KeyboardEvent) => {
  if (event.key === "Escape" && open.value) {
    event.stopPropagation();
    close();
  }
};

onMounted(() => {
  document.addEventListener("pointerdown", onDocumentPointerDown);
  document.addEventListener("keydown", onKeydown);
});

onBeforeUnmount(() => {
  document.removeEventListener("pointerdown", onDocumentPointerDown);
  document.removeEventListener("keydown", onKeydown);
});

defineExpose({ open, close, toggle });
</script>
