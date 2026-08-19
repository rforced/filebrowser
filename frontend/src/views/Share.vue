<template>
  <main class="flex flex-col gap-4 p-4">
    <Card v-if="layoutStore.loading" class="p-10">
      <div
        class="flex flex-col items-center gap-3 text-gray-600 dark:text-gray-300"
      >
        <i class="fa-solid fa-spinner fa-spin text-3xl"></i>
        <span class="text-sm font-medium">{{ t("files.loading") }}</span>
      </div>
    </Card>

    <template v-else-if="error">
      <Card v-if="error.status === 401" class="w-full max-w-sm mx-auto">
        <div class="flex flex-col gap-4 p-6">
          <div class="flex flex-col items-center gap-2 text-center">
            <i
              class="fa-solid fa-lock text-3xl text-gray-500 dark:text-gray-400"
            ></i>
            <h2 class="text-lg font-medium text-gray-900 dark:text-gray-100">
              {{ t("login.password") }}
            </h2>
          </div>

          <div
            v-if="passwordError"
            class="flex gap-2 items-start rounded-md bg-red-50 dark:bg-red-900/40 px-3 py-2 text-sm text-red-700 dark:text-red-200"
            role="alert"
          >
            <i class="fa-solid fa-circle-exclamation mt-0.5"></i>
            <span>{{ passwordError }}</span>
          </div>

          <input
            v-focus
            v-model="password"
            class="form-control"
            type="password"
            autocomplete="current-password"
            :placeholder="t('login.password')"
            @keyup.enter="unlock"
          />

          <button type="button" class="btn btn-blue w-full" @click="unlock">
            {{ t("buttons.submit") }}
          </button>
        </div>
      </Card>

      <errors v-else :errorCode="error.status" :message="errorMessage">
        <router-link
          v-if="shareRoot"
          :to="shareRoot"
          class="btn btn-flex btn-blue btn-soft"
        >
          <i class="fa-solid fa-folder-open"></i>
          <span>{{ t("errors.backToShare") }}</span>
        </router-link>
      </errors>
    </template>

    <template v-else-if="req !== null">
      <breadcrumbs :base="'/share/' + hash">
        <template #actions>
          <IconAction
            v-if="isSingleFile()"
            icon="fa-paste"
            :title="t('buttons.copyDownloadLinkToClipboard')"
            size="lg"
            @action="copyToClipboard(linkSelected())"
          />
          <IconAction
            v-if="req.isDir"
            :icon="fileStore.multiple ? 'fa-circle-check' : 'fa-square-check'"
            :title="t('buttons.selectMultiple')"
            size="lg"
            @action="toggleMultipleSelection"
          />
          <button
            v-if="fileStore.selectedCount"
            type="button"
            class="btn btn-flex btn-blue btn-soft"
            @click="download"
          >
            <i class="fa-solid fa-download"></i>
            <span>
              {{ t("buttons.download") }}
              <template v-if="fileStore.selectedCount > 1"
                >({{ fileStore.selectedCount }})</template
              >
            </span>
          </button>
        </template>
      </breadcrumbs>

      <two-columns>
        <template #main>
          <!-- Directory contents -->
          <template v-if="req.isDir">
            <Card v-if="req.items.length === 0" class="p-10">
              <div
                class="flex flex-col items-center gap-2 text-center text-gray-600 dark:text-gray-300"
              >
                <i class="fa-solid fa-folder-open text-4xl"></i>
                <div class="text-sm font-medium">{{ t("files.lonely") }}</div>
              </div>
            </Card>

            <Card v-else id="listing" class="file-icons overflow-hidden">
              <item
                v-for="item in req.items.slice(0, showLimit)"
                :key="base64(item.name)"
                :index="item.index"
                :name="item.name"
                :isDir="item.isDir"
                :url="item.url"
                :modified="item.modified"
                :type="item.type"
                :size="item.size"
                readOnly
              />

              <button
                v-if="req.items.length > showLimit"
                type="button"
                class="w-full px-4 py-3 text-sm font-medium text-blue-600 dark:text-teal hover:bg-gray-100 dark:hover:bg-gray-700 transition"
                @click="showLimit += 100"
              >
                + {{ req.items.length - showLimit }}
              </button>
            </Card>
          </template>

          <!-- Single-file preview -->
          <Card
            v-else
            class="flex flex-col items-center justify-center gap-4 p-10"
          >
            <i class="fa-solid text-6xl" :class="[icon.icon, icon.color]"></i>
            <div
              class="text-sm font-medium text-gray-700 dark:text-gray-200 break-all text-center"
            >
              {{ req.name }}
            </div>
          </Card>
        </template>

        <template #sidebar>
          <Card class="flex flex-col gap-4 p-6">
            <h3 class="text-lg font-medium text-gray-900 dark:text-gray-100">
              {{
                req.isDir
                  ? t("download.downloadFolder")
                  : t("download.downloadFile")
              }}
            </h3>

            <div class="flex flex-col gap-3">
              <div>
                <div class="text-sm text-gray-600 dark:text-gray-300">
                  {{ t("prompts.displayName") }}
                </div>
                <div class="font-medium text-sm break-all">{{ req.name }}</div>
              </div>

              <div v-if="!req.isDir" :title="modTime">
                <div class="text-sm text-gray-600 dark:text-gray-300">
                  {{ t("prompts.lastModified") }}
                </div>
                <div class="font-medium text-sm">{{ humanTime }}</div>
              </div>

              <div>
                <div class="text-sm text-gray-600 dark:text-gray-300">
                  {{ req.isDir ? t("files.files") : t("prompts.size") }}
                </div>
                <div class="font-medium text-sm">{{ humanSize }}</div>
              </div>
            </div>

            <div class="flex flex-col gap-2">
              <button
                v-if="req.isDir"
                type="button"
                class="btn btn-menu btn-blue btn-soft"
                @click="download"
              >
                <i class="fa-solid fa-download fa-fw"></i>
                <span>{{ t("buttons.download") }}</span>
              </button>

              <a
                v-else
                target="_blank"
                :href="link"
                class="btn btn-menu btn-blue btn-soft"
              >
                <i class="fa-solid fa-download fa-fw"></i>
                <span>{{ t("buttons.download") }}</span>
              </a>

              <a
                v-if="!req.isDir"
                target="_blank"
                :href="inlineLink"
                class="btn btn-menu btn-white btn-soft"
              >
                <i class="fa-solid fa-arrow-up-right-from-square fa-fw"></i>
                <span>{{ t("buttons.openFile") }}</span>
              </a>
            </div>
          </Card>
        </template>
      </two-columns>
    </template>

    <!-- Multiple-selection mode indicator -->
    <Transition
      enter-active-class="transition ease-out duration-200"
      enter-from-class="translate-y-full"
      leave-active-class="transition ease-in duration-200"
      leave-to-class="translate-y-full"
    >
      <div
        v-if="fileStore.multiple"
        id="multiple-selection"
        class="fixed bottom-0 left-0 w-full z-[99999] bg-blue-500 dark:bg-teal-600 text-white dark:text-blue-900 flex items-center justify-between gap-4 px-4 py-3"
      >
        <p class="text-sm font-medium">
          {{ t("files.multipleSelectionEnabled") }}
        </p>
        <button
          type="button"
          class="w-8 h-8 flex items-center justify-center rounded-md hover:bg-black/10 transition"
          :aria-label="t('buttons.clear')"
          @click="fileStore.multiple = false"
        >
          <i class="fa-solid fa-xmark"></i>
        </button>
      </div>
    </Transition>
  </main>
</template>

<script setup lang="ts">
import { pub as api } from "@/api";
import { base64url, filesize } from "@/utils";
import dayjs from "dayjs";
import Breadcrumbs from "@/components/Breadcrumbs.vue";
import Card from "@/components/ui/Card.vue";
import IconAction from "@/components/ui/IconAction.vue";
import TwoColumns from "@/components/layout/TwoColumns.vue";
import { fileIcon } from "@/utils/fileIcons";
import Errors from "@/views/Errors.vue";
import Item from "@/components/files/ListingItem.vue";
import { useFileStore } from "@/stores/file";
import { useLayoutStore } from "@/stores/layout";
import { computed, inject, onMounted, onBeforeUnmount, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { useI18n } from "vue-i18n";
import { StatusError } from "@/api/utils";
import { copy } from "@/utils/clipboard";
import {
  executeRecaptcha,
  mountRecaptcha,
  recaptchaEnabled,
  unmountRecaptcha,
} from "@/utils/recaptcha";

const error = ref<StatusError | null>(null);
const showLimit = ref<number>(100);
const password = ref<string>("");
const passwordError = ref<string>("");
const hash = ref<string>("");

const $showError = inject<IToastError>("$showError")!;
const $showSuccess = inject<IToastSuccess>("$showSuccess")!;

const { t } = useI18n({});

const route = useRoute();
const fileStore = useFileStore();
const layoutStore = useLayoutStore();

watch(route, () => {
  showLimit.value = 100;
  fetchData();
});

const req = computed(() => fileStore.req);

const shareRoot = computed(() => {
  const parts = route.params.path;
  return Array.isArray(parts) && parts.length > 1 ? `/share/${parts[0]}/` : "";
});

// A missing share root is the share itself being gone, and the server answers
// a revoked, expired and never-issued link identically — an expired one is
// deleted the moment it is asked for — so the wording covers both without
// claiming to know which. Deeper in, the share is fine and only the path
// within it is not, which the generic wording already says.
const errorMessage = computed(() =>
  error.value?.status === 404 && !shareRoot.value
    ? "errors.shareNotFound"
    : undefined
);

// Define computes

const icon = computed(() =>
  fileIcon({
    isDir: req.value?.isDir,
    type: req.value?.type,
    extension: req.value?.name
      .slice(req.value.name.lastIndexOf("."))
      .toLowerCase(),
    name: req.value?.name,
  })
);

const link = computed(() =>
  req.value ? api.getDownloadURL(req.value, false, password.value) : ""
);
const inlineLink = computed(() =>
  req.value ? api.getDownloadURL(req.value, true, password.value) : ""
);
const humanSize = computed(() => {
  if (req.value) {
    return req.value.isDir
      ? req.value.items.length
      : filesize(req.value.size ?? 0);
  } else {
    return "";
  }
});
const humanTime = computed(() => dayjs(req.value?.modified).fromNow());
const modTime = computed(() =>
  req.value
    ? new Date(Date.parse(req.value.modified)).toLocaleString()
    : new Date().toLocaleString()
);

const base64 = (name: string) => base64url(name);

let captchaMounted = false;

const syncCaptcha = (needed: boolean) => {
  if (!recaptchaEnabled || needed === captchaMounted) return;

  captchaMounted = needed;
  if (needed) {
    mountRecaptcha();
  } else {
    unmountRecaptcha();
  }
};

const captchaToken = async () => {
  if (!recaptchaEnabled) return "";

  syncCaptcha(true);
  try {
    return await executeRecaptcha("share");
  } catch {
    return "";
  }
};

const unlock = async () => {
  layoutStore.loading = true;
  await fetchData(await captchaToken());
};

const passwordMessage = (err: Error) => {
  if (!(err instanceof StatusError) || err.status !== 401) return "";

  if (err.code === "captchaFailed" || err.code === "captchaRequired") {
    return t("login.captchaFailed");
  }

  return password.value === "" ? "" : t("login.wrongCredentials");
};

const fetchData = async (captcha = "") => {
  fileStore.reload = false;
  fileStore.selected = [];
  fileStore.multiple = false;
  layoutStore.closeHovers();
  layoutStore.loading = true;
  error.value = null;
  passwordError.value = "";

  let url = route.path;
  if (url === "") url = "/";
  if (url[0] !== "/") url = "/" + url;

  try {
    const file = await api.fetch(url, password.value, captcha);
    file.hash = hash.value;

    fileStore.updateRequest(file);
    document.title = `${file.name} - ${document.title}`;
  } catch (err) {
    if (
      err instanceof StatusError &&
      err.code === "captchaRequired" &&
      !captcha
    ) {
      const token = await captchaToken();
      if (token !== "") return fetchData(token);
    }

    if (err instanceof Error) {
      error.value = err;
      passwordError.value = passwordMessage(err);
    }
  } finally {
    layoutStore.loading = false;
    syncCaptcha(error.value?.status === 401);
  }
};

const keyEvent = (event: KeyboardEvent) => {
  if (event.key === "Escape") {
    // If we're on a listing, unselect all
    // files and folders.
    if (fileStore.selectedCount > 0) {
      fileStore.selected = [];
    }
  }
};

const toggleMultipleSelection = () => {
  fileStore.toggleMultiple();
};

const isSingleFile = () =>
  fileStore.selectedCount === 1 &&
  !req.value?.items[fileStore.selected[0]].isDir;

const download = () => {
  if (!req.value) return false;

  if (isSingleFile()) {
    api.download(
      null,
      hash.value,
      password.value,
      req.value.items[fileStore.selected[0]].path
    );
    return true;
  }

  const current = req.value;
  const files = fileStore.selected.map((i) => current.items[i].path);

  if (files.length === 0) {
    files.push(current.path);
  }

  layoutStore.showHover({
    prompt: "download",
    confirm: (format: DownloadFormat) => {
      layoutStore.closeHovers();
      api.download(format, hash.value, password.value, ...files);
      return true;
    },
  });

  return true;
};

const linkSelected = () => {
  return isSingleFile() && req.value
    ? api.getDownloadURL(
        {
          ...req.value,
          hash: hash.value,
          path: req.value.items[fileStore.selected[0]].path,
        },
        false,
        password.value
      )
    : "";
};

const copyToClipboard = (text: string) => {
  copy({ text }).then(
    () => {
      // clipboard successfully set
      $showSuccess(t("success.linkCopied"));
    },
    () => {
      // clipboard write failed
      copy({ text }, { permission: true }).then(
        () => {
          // clipboard successfully set
          $showSuccess(t("success.linkCopied"));
        },
        (e) => {
          // clipboard write failed
          $showError(e);
        }
      );
    }
  );
};

onMounted(async () => {
  // Created
  hash.value = route.params.path[0];
  window.addEventListener("keydown", keyEvent);
  await fetchData();
});

onBeforeUnmount(() => {
  window.removeEventListener("keydown", keyEvent);
  syncCaptcha(false);
});
</script>
