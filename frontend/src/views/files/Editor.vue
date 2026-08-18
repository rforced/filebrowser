<template>
  <div
    id="editor-container"
    class="fixed inset-0 z-9998 flex flex-col bg-gray-50 dark:bg-gray-900"
  >
    <header
      class="flex gap-3 items-center justify-between bg-gray-200 dark:bg-gray-900 border-b border-gray-300 dark:border-gray-700 p-3 md:px-6 shrink-0"
    >
      <div class="flex gap-2 items-center min-w-0">
        <button
          v-tooltip="t('buttons.close')"
          type="button"
          class="action shrink-0"
          :aria-label="t('buttons.close')"
          @click="close()"
        >
          <i class="fa-solid fa-xmark text-lg"></i>
        </button>

        <span class="font-medium text-gray-900 dark:text-gray-100 truncate">
          {{ fileStore.req?.name ?? "" }}
        </span>
      </div>

      <div class="flex gap-2 items-center shrink-0">
        <!-- Font size stepper -->
        <div class="hidden sm:flex items-center btn-group">
          <button
            v-tooltip="t('buttons.decreaseFontSize')"
            type="button"
            class="btn btn-gray btn-sm"
            :aria-label="t('buttons.decreaseFontSize')"
            @click="decreaseFontSize"
          >
            <i class="fa-solid fa-minus"></i>
          </button>
          <span
            class="px-2 py-1 text-sm font-medium tabular-nums bg-gray-700 dark:bg-gray-600 text-gray-100 border-y border-gray-600 dark:border-gray-500"
            >{{ fontSize }}px</span
          >
          <button
            v-tooltip="t('buttons.increaseFontSize')"
            type="button"
            class="btn btn-gray btn-sm"
            :aria-label="t('buttons.increaseFontSize')"
            @click="increaseFontSize"
          >
            <i class="fa-solid fa-plus"></i>
          </button>
        </div>

        <button
          v-show="isMarkdownFile"
          v-tooltip="t('buttons.preview')"
          type="button"
          class="btn btn-flex btn-white btn-soft"
          :aria-label="t('buttons.preview')"
          @click="preview()"
        >
          <i class="fa-solid" :class="isPreview ? 'fa-pen' : 'fa-eye'"></i>
          <span class="hidden md:inline">{{ t("buttons.preview") }}</span>
        </button>

        <button
          v-if="isOutFile"
          type="button"
          class="btn btn-flex btn-white btn-soft"
          :aria-label="t('buttons.viewAsGraph')"
          @click="viewAsGraph"
        >
          <i class="fa-solid fa-chart-line"></i>
          <span class="hidden md:inline">{{ t("buttons.viewAsGraph") }}</span>
        </button>

        <button
          v-if="isSurfaceFile"
          type="button"
          class="btn btn-flex btn-white btn-soft"
          :aria-label="t('buttons.view3d')"
          @click="view3d"
        >
          <i class="fa-solid fa-cube"></i>
          <span class="hidden md:inline">{{ t("buttons.view3d") }}</span>
        </button>

        <button
          v-if="isLogFile"
          type="button"
          class="btn btn-flex btn-white btn-soft"
          :aria-label="t('buttons.follow')"
          @click="followLog"
        >
          <i class="fa-solid fa-play"></i>
          <span class="hidden md:inline">{{ t("buttons.follow") }}</span>
        </button>

        <button
          v-if="authStore.user?.perm.modify && !isReadOnly"
          id="save-button"
          type="button"
          class="btn btn-flex btn-blue btn-soft"
          :aria-label="t('buttons.save')"
          @click="save()"
        >
          <i class="fa-solid" :class="buttonIcon('save', 'fa-floppy-disk')"></i>
          <span class="hidden md:inline">{{ t("buttons.save") }}</span>
        </button>
      </div>
    </header>

    <div
      v-if="layoutStore.loading"
      class="flex-1 flex items-center justify-center"
    >
      <i
        class="fa-solid fa-spinner fa-spin text-3xl text-gray-500 dark:text-gray-400"
      ></i>
    </div>

    <template v-else>
      <div
        class="flex gap-3 items-center justify-between px-3 md:px-6 py-2 bg-white dark:bg-gray-800 border-b border-gray-200 dark:border-gray-700 shrink-0"
      >
        <Breadcrumbs base="/files" noLink />

        <div class="flex gap-1 items-center shrink-0">
          <button
            v-tooltip="t('buttons.copy')"
            type="button"
            class="action"
            :disabled="isSelectionEmpty"
            :aria-label="t('buttons.copy')"
            @click="executeEditorCommand('copy')"
          >
            <i class="fa-solid fa-copy"></i>
          </button>
          <button
            v-tooltip="t('buttons.cut')"
            type="button"
            class="action"
            :disabled="isSelectionEmpty"
            :aria-label="t('buttons.cut')"
            @click="executeEditorCommand('cut')"
          >
            <i class="fa-solid fa-scissors"></i>
          </button>
          <button
            v-tooltip="t('buttons.paste')"
            type="button"
            class="action"
            :aria-label="t('buttons.paste')"
            @click="executeEditorCommand('paste')"
          >
            <i class="fa-solid fa-paste"></i>
          </button>
          <button
            v-tooltip="t('buttons.more')"
            type="button"
            class="action"
            :aria-label="t('buttons.more')"
            @click="executeEditorCommand('openCommandPalette')"
          >
            <i class="fa-solid fa-ellipsis-vertical"></i>
          </button>
        </div>
      </div>

      <div
        v-show="isPreview && isMarkdownFile"
        id="preview-container"
        class="md_preview m-4"
        v-html="previewContent"
      ></div>
      <form
        v-show="!isPreview || !isMarkdownFile"
        id="editor"
        class="flex-1 min-h-0"
      ></form>
    </template>
  </div>
</template>

<script setup lang="ts">
import { files as api } from "@/api";
import buttons, { buttonIcon } from "@/utils/buttons";
import url from "@/utils/url";
import ace, { Ace } from "ace-builds";
import "ace-builds/src-noconflict/ext-language_tools";
import modelist from "ace-builds/src-noconflict/ext-modelist";
import { ACE_ASSET_DIR, clampMode } from "@/utils/aceAssets";
import DOMPurify from "dompurify";

import Breadcrumbs from "@/components/Breadcrumbs.vue";
import { useAuthStore } from "@/stores/auth";
import { useFileStore } from "@/stores/file";
import { useLayoutStore } from "@/stores/layout";
import { isOutFileName } from "@/utils/convergeOut";
import { isSurfaceDatFile } from "@/utils/convergeSurface";
import { isLogFileName } from "@/utils/logTail";
import { stripAnsi } from "@/utils/ansi";
import { getEditorTheme } from "@/utils/theme";
import { marked } from "marked";
import { inject, onBeforeUnmount, onMounted, ref, watchEffect } from "vue";
import { useI18n } from "vue-i18n";
import { onBeforeRouteUpdate, useRoute, useRouter } from "vue-router";
import { read, copy } from "@/utils/clipboard";

const $showError = inject<IToastError>("$showError")!;

const fileStore = useFileStore();
const authStore = useAuthStore();
const layoutStore = useLayoutStore();

const { t } = useI18n();

const route = useRoute();
const router = useRouter();

const editor = ref<Ace.Editor | null>(null);
const fontSize = ref(parseInt(localStorage.getItem("editorFontSize") || "14"));

const isPreview = ref(false);
const previewContent = ref("");
const isMarkdownFile =
  fileStore.req?.name.endsWith(".md") ||
  fileStore.req?.name.endsWith(".markdown");
const isOutFile = isOutFileName(fileStore.req?.name ?? "");
const isLogFile = isLogFileName(fileStore.req?.name ?? "");
const isSurfaceFile = isSurfaceDatFile(
  fileStore.req?.name ?? "",
  fileStore.req?.type ?? "",
  fileStore.req?.content
);
const isSelectionEmpty = ref(true);
// Escape codes are stripped out of a log so it reads as text, which makes the
// buffer a lossy copy — saving it back would rewrite a file the solver may
// still be appending to.
const isReadOnly = fileStore.req?.type === "textImmutable" || isLogFile;

const viewAsGraph = () => {
  router.replace({ query: { ...route.query, view: "plot" } });
};

const view3d = () => {
  router.replace({ query: { ...route.query, view: "3d" } });
};

const followLog = () => {
  router.replace({ query: { ...route.query, view: undefined } });
};

const executeEditorCommand = (name: string) => {
  if (name == "paste") {
    read()
      .then((data) => {
        editor.value?.execCommand("paste", {
          text: data,
        });
      })
      .catch((e) => {
        if (
          document.queryCommandSupported &&
          document.queryCommandSupported("paste")
        ) {
          document.execCommand("paste");
        } else {
          console.warn("the clipboard api is not supported", e);
        }
      });
    return;
  }
  if (name == "copy" || name == "cut") {
    const selectedText = editor.value?.getCopyText();
    copy({ text: selectedText });
  }
  editor.value?.execCommand(name);
};

onMounted(() => {
  window.addEventListener("keydown", keyEvent);
  window.addEventListener("beforeunload", handlePageChange);

  const raw = fileStore.req?.content || "";
  const fileContent = isLogFile ? stripAnsi(raw) : raw;

  watchEffect(async () => {
    if (isMarkdownFile && isPreview.value) {
      const new_value = editor.value?.getValue() || "";
      try {
        previewContent.value = DOMPurify.sanitize(await marked(new_value));
      } catch (error) {
        console.error("Failed to convert content to HTML:", error);
        previewContent.value = "";
      }
    }
  });

  ace.config.set("basePath", window.__prependStaticUrl(`${ACE_ASSET_DIR}/`));

  if (!layoutStore.loading) {
    initEditor(fileContent);
  } else {
    const unwatch = watchEffect(() => {
      // Initialize editor when layout is loaded
      if (!layoutStore.loading) {
        setTimeout(() => {
          initEditor(fileContent);
          unwatch();
        }, 50);
      }
    });
  }
});

onBeforeUnmount(() => {
  window.removeEventListener("keydown", keyEvent);
  window.removeEventListener("beforeunload", handlePageChange);
  editor.value?.destroy();
});

onBeforeRouteUpdate((to, from, next) => {
  if (editor.value?.session.getUndoManager().isClean()) {
    next();

    return;
  }

  layoutStore.showHover({
    prompt: "discardEditorChanges",
    confirm: (event: Event) => {
      event.preventDefault();
      next();
    },
    saveAction: async () => {
      await save();
      next();
    },
  });
});

const initEditor = (fileContent: string) => {
  editor.value = ace.edit("editor", {
    value: fileContent,
    showPrintMargin: false,
    readOnly: isReadOnly,
    theme: getEditorTheme(),
    mode: clampMode(modelist.getModeForPath(fileStore.req!.name).mode),
    useWorker: false,
    wrap: true,
    enableBasicAutocompletion: true,
    enableLiveAutocompletion: true,
    enableSnippets: true,
  });

  editor.value.setFontSize(fontSize.value);
  editor.value.focus();

  const selection = editor.value?.getSelection();
  selection.on("changeSelection", function () {
    isSelectionEmpty.value = selection.isEmpty();
  });
};

const keyEvent = (event: KeyboardEvent) => {
  if (event.code === "Escape") {
    close();
  }

  if (!event.ctrlKey && !event.metaKey) {
    return;
  }

  if (event.key !== "s") {
    return;
  }

  event.preventDefault();
  save();
};

const handlePageChange = (event: BeforeUnloadEvent) => {
  if (!editor.value?.session.getUndoManager().isClean()) {
    event.preventDefault();
    // returnValue is now depecrated, though keeping in for legacy browser support
    // https://developer.mozilla.org/en-US/docs/Web/API/BeforeUnloadEvent/returnValue
    event.returnValue = true;
  }
};

const save = async (throwError?: boolean) => {
  if (isReadOnly) return;

  const button = "save";
  buttons.loading("save");

  try {
    await api.put(route.path, editor.value?.getValue());
    editor.value?.session.getUndoManager().markClean();
    buttons.success(button);
  } catch (e: any) {
    buttons.done(button);
    $showError(e);
    if (throwError) throw e;
  }
};

const increaseFontSize = () => {
  fontSize.value += 1;
  editor.value?.setFontSize(fontSize.value);
  localStorage.setItem("editorFontSize", fontSize.value.toString());
};

const decreaseFontSize = () => {
  if (fontSize.value > 1) {
    fontSize.value -= 1;
    editor.value?.setFontSize(fontSize.value);
    localStorage.setItem("editorFontSize", fontSize.value.toString());
  }
};

const close = () => {
  if (!editor.value?.session.getUndoManager().isClean()) {
    layoutStore.showHover({
      prompt: "discardEditorChanges",
      confirm: (event: Event) => {
        event.preventDefault();
        editor.value?.session.getUndoManager().reset();
        finishClose();
      },
      saveAction: async () => {
        try {
          await save(true);
          finishClose();
        } catch {}
      },
    });
    return;
  }
  finishClose();
};

const finishClose = () => {
  const uri = url.removeLastDir(route.path) + "/";
  router.push({ path: uri });
};

const preview = () => {
  isPreview.value = !isPreview.value;
};
</script>

<style scoped>
.editor-font-size {
  margin: 0 0.5em;
  color: var(--fg);
}

.editor-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.editor-header > div > button {
  background: transparent;
  color: var(--action);
  border: none;
  outline: none;
  opacity: 0.8;
  cursor: pointer;
}

.editor-header > div > button:hover:not(:disabled) {
  opacity: 1;
}

.editor-header > div > button:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.editor-header > div > button > span > i {
  font-size: 1.2rem;
}
</style>
