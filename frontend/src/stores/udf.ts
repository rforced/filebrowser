import { defineStore } from "pinia";
import { reactive, ref } from "vue";

import { files as api } from "@/api";
import type { UdfPhase } from "@/api/files";
import { StatusError } from "@/api/utils";
import buttons from "@/utils/buttons";
import { useFileStore } from "./file";
import { useToastStore } from "./toast";
import i18n from "@/i18n";

export interface UdfBuildJob {
  id: number;
  name: string;
  version: string;
  phase: UdfPhase;
  percent: number;
  line: string;
}

let nextId = 0;

const controllers = new Map<number, AbortController>();

const beforeUnload = (event: BeforeUnloadEvent) => {
  event.preventDefault();
};

export const useUdfStore = defineStore("udf", () => {
  const jobs = ref<UdfBuildJob[]>([]);

  const remove = (id: number) => {
    controllers.delete(id);
    const index = jobs.value.findIndex((job) => job.id === id);
    if (index !== -1) jobs.value.splice(index, 1);
    if (jobs.value.length === 0) {
      window.removeEventListener("beforeunload", beforeUnload);
    }
  };

  const start = (url: string, name: string, version: string): Promise<void> => {
    const { t } = i18n.global;
    const toastStore = useToastStore();
    const fileStore = useFileStore();

    const id = nextId++;
    const controller = new AbortController();
    controllers.set(id, controller);

    const job = reactive<UdfBuildJob>({
      id,
      name,
      version,
      phase: "configure",
      percent: 0,
      line: "",
    });

    // A build the server refused never becomes a card: the prompt that asked
    // for it is still open and shows the error itself.
    let accepted = false;
    buttons.loading("converge-udf");

    return new Promise<void>((resolve, reject) => {
      api
        .udfBuild(
          url,
          version,
          (progress) => {
            job.phase = progress.phase;
            if (progress.percent > 0) job.percent = progress.percent;
            if (progress.line) job.line = progress.line;
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
        .then((final) => {
          // The stream carries its own outcome: a compile that fails still ends
          // in a completed HTTP response, because the headers went out before
          // the first line of the compiler's output did.
          if (final?.error) {
            buttons.done("converge-udf");
            toastStore.show(
              t("prompts.udfFailed", { name, error: final.error }),
              "error",
              0
            );
          } else {
            buttons.success("converge-udf");
            toastStore.show(t("prompts.udfSuccess", { name }), "success");
          }
          fileStore.reload = true;
        })
        .catch((e: unknown) => {
          buttons.done("converge-udf");
          if (!accepted) {
            reject(e);
            return;
          }
          if (controller.signal.aborted) {
            // A cancelled build still left objects in build/; show them.
            fileStore.reload = true;
            return;
          }
          const error = e instanceof Error ? e.message : String(e);
          toastStore.show(t("prompts.udfFailed", { name, error }), "error", 0);
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

/*
 * Turns a refused build into what the prompt should say. The server names the
 * cause, so "another build is already running here" reads as itself rather than
 * as a bare 409.
 */
export const udfStartFailure = (error: unknown): string => {
  const { t } = i18n.global;
  const code = error instanceof StatusError ? error.code : undefined;

  switch (code) {
    case "udfBuilding":
      return t("prompts.udfAlreadyBuilding");
    case "udfBusy":
      return t("prompts.udfBusy");
    case "udfUnknownVersion":
      return t("prompts.udfUnknownVersion");
    default:
      return error instanceof Error ? error.message : String(error);
  }
};
