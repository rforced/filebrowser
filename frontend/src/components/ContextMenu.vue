<template>
  <div
    v-if="show"
    ref="contextMenu"
    class="absolute min-w-48 max-w-[80svw] overflow-y-auto overflow-x-hidden border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-900 text-gray-900 dark:text-gray-100 py-1 rounded-md shadow-lg z-50"
    :style="style"
  >
    <slot />
  </div>
</template>

<script setup lang="ts">
import { ref, watch, onUnmounted } from "vue";

const MARGIN = 8;

const emit = defineEmits(["hide"]);
const props = defineProps<{ show: boolean; pos: { x: number; y: number } }>();
const contextMenu = ref<HTMLElement | null>(null);
const style = ref<Record<string, string>>({});

const place = () => {
  const el = contextMenu.value;
  if (!el) return;

  const maxHeight = window.innerHeight - MARGIN * 2;
  const borders = el.offsetHeight - el.clientHeight;
  const height = Math.min(el.scrollHeight + borders, maxHeight);
  const width = el.offsetWidth;

  const x = props.pos.x - window.scrollX;
  const y = props.pos.y - window.scrollY;

  const flip = y + height > window.innerHeight - MARGIN;
  const top = Math.min(
    Math.max(flip ? y - height : y, MARGIN),
    window.innerHeight - height - MARGIN
  );
  const left = Math.min(
    Math.max(x, MARGIN),
    window.innerWidth - width - MARGIN
  );

  style.value = {
    top: `${top + window.scrollY}px`,
    left: `${left + window.scrollX}px`,
    maxHeight: `${maxHeight}px`,
  };
};

const hideContextMenu = () => {
  emit("hide");
};

watch(
  () => [props.show, props.pos],
  ([val]) => {
    if (val) {
      place();
      document.addEventListener("click", hideContextMenu);
    } else {
      document.removeEventListener("click", hideContextMenu);
    }
  },
  { flush: "post" }
);

onUnmounted(() => {
  document.removeEventListener("click", hideContextMenu);
});
</script>
