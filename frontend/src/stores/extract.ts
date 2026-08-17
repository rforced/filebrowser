import { defineStore } from "pinia";
import { reactive, ref } from "vue";

import { files as api } from "@/api";
import type { ExtractOptions } from "@/api/files";
import { useFileStore } from "./file";
import { useToastStore } from "./toast";
import i18n from "@/i18n";

export interface ExtractionJob {
  id: number;
  name: string;
  current: number;
  total: number;
  currentFile: string;
}

let nextId = 0;

const controllers = new Map<number, AbortController>();

const beforeUnload = (event: BeforeUnloadEvent) => {
  event.preventDefault();
};

export const useExtractStore = defineStore("extract", () => {
  const jobs = ref<ExtractionJob[]>([]);

  const remove = (id: number) => {
    controllers.delete(id);
    const index = jobs.value.findIndex((job) => job.id === id);
    if (index !== -1) jobs.value.splice(index, 1);
    if (jobs.value.length === 0) {
      window.removeEventListener("beforeunload", beforeUnload);
    }
  };

  const start = (
    url: string,
    name: string,
    options: ExtractOptions
  ): Promise<void> => {
    const { t } = i18n.global;
    const toastStore = useToastStore();
    const fileStore = useFileStore();

    const id = nextId++;
    const controller = new AbortController();
    controllers.set(id, controller);

    const job = reactive<ExtractionJob>({
      id,
      name,
      current: 0,
      total: 0,
      currentFile: "",
    });

    let accepted = false;

    return new Promise<void>((resolve, reject) => {
      api
        .extract(
          url,
          options,
          (progress) => {
            // The final done event carries zeroed fields; don't blank the
            // card with it.
            if (progress.current > 0) job.current = progress.current;
            if (progress.total > 0) job.total = progress.total;
            if (progress.currentFile) job.currentFile = progress.currentFile;
          },
          () => {
            accepted = true;
            if (jobs.value.length === 0) {
              window.addEventListener("beforeunload", beforeUnload);
            }
            jobs.value.push(job);
            resolve();
          },
          controller.signal
        )
        .then(() => {
          toastStore.show(t("prompts.extractSuccess", { name }), "success");
          fileStore.reload = true;
        })
        .catch((e: unknown) => {
          if (!accepted) {
            reject(e);
            return;
          }
          if (controller.signal.aborted) {
            // A cancelled extraction still left files behind; show them.
            fileStore.reload = true;
            return;
          }
          const error = e instanceof Error ? e.message : String(e);
          toastStore.show(
            t("prompts.extractFailed", { name, error }),
            "error",
            0
          );
          fileStore.reload = true;
        })
        .finally(() => remove(id));
    });
  };

  const cancel = (id: number) => {
    controllers.get(id)?.abort();
  };

  return { jobs, start, cancel };
});
