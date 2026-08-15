<template>
  <div
    ref="container"
    class="flex items-center gap-1 overflow-x-auto max-w-full text-sm text-gray-600 dark:text-gray-400 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
  >
    <template v-for="(item, index) in path" :key="index">
      <span class="text-gray-400 dark:text-gray-600">/</span>
      <span class="flex items-center gap-1 whitespace-nowrap">
        <i
          class="fa-solid fa-fw text-xs"
          :class="
            isDir === true || index < path.length - 1 ? 'fa-folder' : 'fa-file'
          "
        ></i>
        {{ item }}
      </span>
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick } from "vue";
import { useRoute } from "vue-router";
import { useFileStore } from "@/stores/file";
import url from "@/utils/url";

const fileStore = useFileStore();
const route = useRoute();

const props = defineProps({
  name: {
    type: String,
    required: true,
  },
  isDir: {
    type: Boolean,
    default: false,
  },
  path: {
    type: String,
    default: null,
  },
});

const container = ref<HTMLElement | null>(null);

const path = computed(() => {
  const routePath = props.path || route.path;
  let basePath = fileStore.isFiles ? routePath : url.removeLastDir(routePath);
  if (!basePath.endsWith("/")) {
    basePath += "/";
  }
  basePath += props.name;
  return basePath.split("/").filter(Boolean).splice(1);
});

watch(path, () => {
  nextTick(() => {
    const lastItem = container.value?.lastElementChild;
    lastItem?.scrollIntoView({ behavior: "auto", inline: "end" });
  });
});
</script>
