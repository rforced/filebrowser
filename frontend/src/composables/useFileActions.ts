import { computed, inject } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";

import { useAuthStore } from "@/stores/auth";
import { useFileStore } from "@/stores/file";
import { useLayoutStore } from "@/stores/layout";
import { useToastStore } from "@/stores/toast";
import { users, files as api } from "@/api";
import { StatusError } from "@/api/utils";
import buttons, { isButtonBusy } from "@/utils/buttons";

export interface FileAction {
  id: string;
  icon: string;
  label: string;
  enabled: boolean;
  visible: boolean;
  variant?: string;
  run: () => void;
}

const ARCHIVE_EXTENSIONS = [
  ".zip",
  ".tar",
  ".tar.gz",
  ".tgz",
  ".tar.zst",
  ".tzst",
  ".tar.lz4",
  ".tlz4",
  ".zst",
  ".lz4",
];

const COMPOUND_EXTENSIONS = [".tar.gz", ".tar.zst", ".tar.lz4"];

const COMBINED_OUTPUT_DIR = "outputs_combined";

export const isArchiveFile = (name: string): boolean => {
  const lower = name.toLowerCase();
  return ARCHIVE_EXTENSIONS.some((ext) => lower.endsWith(ext));
};

export const archiveBaseName = (name: string): string => {
  const lower = name.toLowerCase();
  for (const ext of COMPOUND_EXTENSIONS) {
    if (lower.endsWith(ext)) return name.slice(0, -ext.length);
  }
  for (const ext of [
    ".tgz",
    ".tzst",
    ".tlz4",
    ".zip",
    ".tar",
    ".zst",
    ".lz4",
  ]) {
    if (lower.endsWith(ext)) return name.slice(0, -ext.length);
  }
  return name;
};

export const useFileActions = () => {
  const { t } = useI18n();
  const route = useRoute();
  const authStore = useAuthStore();
  const fileStore = useFileStore();
  const layoutStore = useLayoutStore();
  const $showError = inject<IToastError>("$showError")!;

  const selectedCount = computed(() => fileStore.selectedCount);

  const selectedItem = computed(() =>
    selectedCount.value === 1 && fileStore.req
      ? fileStore.req.items[fileStore.selected[0]]
      : null
  );

  const isConvergeCase = computed(
    () =>
      fileStore.req?.isDir === true &&
      fileStore.req.items.some(
        (item) => !item.isDir && item.name === "inputs.in"
      )
  );

  // A UDF package is a CMakeLists.txt plus sources, and need not be a CONVERGE
  // case at all — the reference package carries no inputs.in. The listing is
  // only the cheap half of the test: the server confirms the file really is the
  // CONVERGE one by its content before it offers a build.
  const isUdfPackage = computed(
    () =>
      fileStore.req?.isDir === true &&
      fileStore.req.items.some(
        (item) => !item.isDir && item.name === "CMakeLists.txt"
      )
  );

  // The combined tree is derived from the legs rather than being one, so it
  // does not count toward having something to combine. The server agrees.
  const outputRunCount = computed(
    () =>
      fileStore.req?.items.filter((item) => {
        const name = item.name.toLowerCase();
        return (
          item.isDir &&
          name.startsWith("outputs_") &&
          name !== COMBINED_OUTPUT_DIR
        );
      }).length ?? 0
  );

  const selectedIsArchive = computed(() => {
    const item = selectedItem.value;
    return item != null && !item.isDir && isArchiveFile(item.name);
  });

  const perm = computed(() => authStore.user?.perm);

  const showPrompt = (prompt: string) => layoutStore.showHover(prompt);

  const download = () => {
    if (fileStore.req === null) return;

    if (
      selectedCount.value === 1 &&
      !fileStore.req.items[fileStore.selected[0]].isDir
    ) {
      api.download(null, fileStore.req.items[fileStore.selected[0]].url);
      return;
    }

    layoutStore.showHover({
      prompt: "download",
      confirm: (format: any) => {
        layoutStore.closeHovers();

        const files = [];
        if (selectedCount.value > 0 && fileStore.req !== null) {
          for (const i of fileStore.selected) {
            files.push(fileStore.req.items[i].url);
          }
        } else {
          files.push(route.path);
        }

        api.download(format, ...files);
      },
    });
  };

  const upload = () => {
    if (
      typeof window.DataTransferItem !== "undefined" &&
      typeof DataTransferItem.prototype.webkitGetAsEntry !== "undefined"
    ) {
      layoutStore.showHover("upload");
    } else {
      document.getElementById("upload-input")?.click();
    }
  };

  const extract = () => {
    const item = selectedItem.value;
    if (!item || item.isDir) return;

    layoutStore.showHover({
      prompt: "extract",
      props: { destination: archiveBaseName(item.name) },
    });
  };

  // Nothing is queued server-side, so refreshing or closing the page cancels
  // the request and takes the half-written tree with it.
  const combineOutput = async () => {
    const toastStore = useToastStore();
    const running = toastStore.show(t("converge.combining"), "info", 0);

    buttons.loading("converge-combine");
    try {
      const result = await api.convergeCombine(route.path);
      buttons.success("converge-combine");
      toastStore.dismiss(running);
      toastStore.show(
        t("converge.combined", {
          files: result.files,
          name: COMBINED_OUTPUT_DIR,
        }),
        "success"
      );
      fileStore.reload = true;
    } catch (error) {
      buttons.done("converge-combine");
      toastStore.dismiss(running);
      $showError(combineFailure(error));
    }
  };

  const combineFailure = (error: unknown): string => {
    const code = error instanceof StatusError ? error.code : undefined;
    switch (code) {
      case "combinedExists":
        return t("converge.combineExists", { name: COMBINED_OUTPUT_DIR });
      case "combineNeedsRuns":
        return t("converge.combineNeedsRuns");
      default:
        return error instanceof Error ? error.message : String(error);
    }
  };

  const switchView = async () => {
    layoutStore.closeHovers();

    const modes: Record<string, ViewModeType> = {
      list: "mosaic",
      mosaic: "mosaic gallery",
      "mosaic gallery": "list",
    };

    const data = {
      id: authStore.user?.id,
      viewMode: modes[authStore.user?.viewMode ?? "list"] || "list",
    };

    users.update(data, ["viewMode"]).catch($showError);
    authStore.updateUser(data);
  };

  const viewIcon = computed(() => {
    const icons: Record<string, string> = {
      list: "fa-table-cells-large",
      mosaic: "fa-images",
      "mosaic gallery": "fa-list",
    };
    return icons[authStore.user?.viewMode ?? "list"] ?? "fa-table-cells-large";
  });

  const actions = computed<FileAction[]>(() => {
    const hasSelection = selectedCount.value > 0;
    const single = selectedCount.value === 1;

    return [
      {
        id: "new-folder",
        icon: "fa-folder-plus",
        label: t("sidebar.newFolder"),
        visible: !!perm.value?.create,
        enabled: true,
        run: () => showPrompt("newDir"),
      },
      {
        id: "new-file",
        icon: "fa-file-circle-plus",
        label: t("sidebar.newFile"),
        visible: !!perm.value?.create,
        enabled: true,
        run: () => showPrompt("newFile"),
      },
      {
        id: "upload",
        icon: "fa-upload",
        label: t("buttons.upload"),
        visible: !!perm.value?.create,
        enabled: true,
        run: upload,
      },
      {
        id: "download",
        icon: "fa-download",
        label:
          selectedCount.value > 1
            ? `${t("buttons.download")} (${selectedCount.value})`
            : t("buttons.download"),
        visible: !!perm.value?.download,
        enabled: true,
        run: download,
      },
      {
        id: "rename",
        icon: "fa-pen-to-square",
        label: t("buttons.rename"),
        visible: !!perm.value?.rename,
        enabled: single,
        run: () => showPrompt("rename"),
      },
      {
        id: "copy",
        icon: "fa-copy",
        label: t("buttons.copyFile"),
        visible: !!perm.value?.create,
        enabled: hasSelection,
        run: () => showPrompt("copy"),
      },
      {
        id: "move",
        icon: "fa-file-export",
        label: t("buttons.moveFile"),
        visible: !!perm.value?.rename,
        enabled: hasSelection,
        run: () => showPrompt("move"),
      },
      {
        id: "share",
        icon: "fa-share-nodes",
        label: t("buttons.share"),
        visible: !!perm.value?.share && !!perm.value?.download,
        enabled: single,
        run: () => showPrompt("share"),
      },
      {
        id: "extract",
        icon: "fa-box-open",
        label: t("buttons.extract"),
        visible: !!perm.value?.create,
        enabled: selectedIsArchive.value,
        run: extract,
      },
      {
        id: "info",
        icon: "fa-circle-info",
        label: t("buttons.info"),
        visible: true,
        enabled: true,
        run: () => showPrompt("info"),
      },
      {
        id: "delete",
        icon: "fa-trash",
        label:
          selectedCount.value > 1
            ? `${t("buttons.delete")} (${selectedCount.value})`
            : t("buttons.delete"),
        visible: !!perm.value?.delete,
        enabled: hasSelection,
        variant: "btn-red-outline",
        run: () => showPrompt("delete"),
      },
      {
        id: "converge-clean",
        icon: "fa-broom",
        label: t("buttons.cleanConvergeOutput"),
        visible: !!perm.value?.delete,
        enabled: isConvergeCase.value,
        run: () => showPrompt("converge-clean"),
      },
      {
        id: "converge-combine",
        icon: "fa-code-merge",
        label: t("buttons.combineConvergeOutput"),
        visible: !!perm.value?.create,
        enabled:
          isConvergeCase.value &&
          outputRunCount.value > 1 &&
          !isButtonBusy("converge-combine"),
        run: () => showPrompt("converge-combine"),
      },
      {
        id: "converge-udf",
        icon: "fa-hammer",
        label: t("buttons.compileUdf"),
        visible: !!perm.value?.create,
        enabled: isUdfPackage.value && !isButtonBusy("converge-udf"),
        run: () => showPrompt("converge-udf"),
      },
    ].filter((action) => action.visible);
  });

  return {
    actions,
    selectedCount,
    selectedItem,
    isConvergeCase,
    isUdfPackage,
    viewIcon,
    switchView,
    download,
    upload,
    extract,
    combineOutput,
  };
};
