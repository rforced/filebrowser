<template>
  <div
    v-show="show"
    ref="contextMenu"
    class="absolute min-w-48 max-w-[80svw] overflow-hidden border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-900 text-gray-900 dark:text-gray-100 py-1 rounded-md shadow-lg z-50"
    :style="{ top: `${props.pos.y}px`, left: `${left}px` }"
  >
    <slot />
  </div>
</template>

<script setup lang="ts">
import { ref, watch, computed, onUnmounted } from "vue";

const emit = defineEmits(["hide"]);
const props = defineProps<{ show: boolean; pos: { x: number; y: number } }>();
const contextMenu = ref<HTMLElement | null>(null);

const left = computed(() => {
  return Math.min(
    props.pos.x,
    window.innerWidth - (contextMenu.value?.clientWidth ?? 0)
  );
});

const hideContextMenu = () => {
  emit("hide");
};

watch(
  () => props.show,
  (val) => {
    if (val) {
      document.addEventListener("click", hideContextMenu);
    } else {
      document.removeEventListener("click", hideContextMenu);
    }
  }
);

onUnmounted(() => {
  document.removeEventListener("click", hideContextMenu);
});
</script>
