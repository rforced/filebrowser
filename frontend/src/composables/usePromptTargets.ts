import { computed } from "vue";

import { useFileStore } from "@/stores/file";

export function usePromptTargets() {
  const fileStore = useFileStore();

  const selectedTargets = computed(() => {
    const items = fileStore.req?.items;
    if (!items) return [];

    return fileStore.selected
      .map((index) => items[index])
      .filter((item) => item !== undefined)
      .map((item) => ({ name: item.name, isDir: item.isDir }));
  });

  const currentTargets = computed(() =>
    fileStore.req
      ? [{ name: fileStore.req.name, isDir: fileStore.req.isDir }]
      : []
  );

  return { selectedTargets, currentTargets };
}
