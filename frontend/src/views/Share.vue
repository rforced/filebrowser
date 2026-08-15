<template>
  <main class="flex flex-col gap-4 p-4">
    <Banner
      :title="req?.name || t('buttons.share')"
      :subtitle="
        req
          ? req.isDir
            ? t('download.downloadFolder')
            : t('download.downloadFile')
          : undefined
      "
    >
      <div v-if="req" class="flex gap-2 items-center">
        <IconAction
          v-if="isSingleFile()"
          icon="fa-paste"
          :title="t('buttons.copyDownloadLinkToClipboard')"
          @action="copyToClipboard(linkSelected())"
        />
        <IconAction
          v-if="req.isDir"
          :icon="fileStore.multiple ? 'fa-circle-check' : 'fa-square-check'"
          :title="t('buttons.selectMultiple')"
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
      </div>
    </Banner>

    <hr class="border-gray-200 dark:border-gray-700" />

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
            v-if="attemptedPasswordLogin"
            class="flex gap-2 items-start rounded-md bg-red-50 dark:bg-red-900/40 px-3 py-2 text-sm text-red-700 dark:text-red-200"
            role="alert"
          >
            <i class="fa-solid fa-circle-exclamation mt-0.5"></i>
            <span>{{ t("login.wrongCredentials") }}</span>
          </div>

          <input
            v-focus
            v-model="password"
            class="form-control"
            type="password"
            autocomplete="current-password"
            :placeholder="t('login.password')"
            @keyup.enter="fetchData"
          />

          <button type="button" class="btn btn-blue w-full" @click="fetchData">
            {{ t("buttons.submit") }}
          </button>
        </div>
      </Card>

      <errors v-else :errorCode="error.status" />
    </template>

    <template v-else-if="req !== null">
      <breadcrumbs :base="'/share/' + hash" />

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
              <a
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

          <!-- Scan-to-download -->
          <Card class="flex flex-col items-center gap-3 p-6">
            <qrcode-vue
              :value="link"
              :size="180"
              level="M"
              class="rounded-xs"
            />
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
import Banner from "@/components/ui/Banner.vue";
import Card from "@/components/ui/Card.vue";
import IconAction from "@/components/ui/IconAction.vue";
import TwoColumns from "@/components/layout/TwoColumns.vue";
import { fileIcon } from "@/utils/fileIcons";
import Errors from "@/views/Errors.vue";
import QrcodeVue from "qrcode.vue";
import Item from "@/components/files/ListingItem.vue";
import { useFileStore } from "@/stores/file";
import { useLayoutStore } from "@/stores/layout";
import { computed, inject, onMounted, onBeforeUnmount, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { useI18n } from "vue-i18n";
import { StatusError } from "@/api/utils";
import { copy } from "@/utils/clipboard";

const error = ref<StatusError | null>(null);
const showLimit = ref<number>(100);
const password = ref<string>("");
const attemptedPasswordLogin = ref<boolean>(false);
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

// Functions
const base64 = (name: string) => base64url(name);
const fetchData = async () => {
  fileStore.reload = false;
  fileStore.selected = [];
  fileStore.multiple = false;
  layoutStore.closeHovers();

  // Set loading to true and reset the error.
  layoutStore.loading = true;
  error.value = null;
  if (password.value !== "") {
    attemptedPasswordLogin.value = true;
  }

  let url = route.path;
  if (url === "") url = "/";
  if (url[0] !== "/") url = "/" + url;

  try {
    const file = await api.fetch(url, password.value);
    file.hash = hash.value;

    fileStore.updateRequest(file);
    document.title = `${file.name} - ${document.title}`;
  } catch (err) {
    if (err instanceof Error) {
      error.value = err;
    }
  } finally {
    layoutStore.loading = false;
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

  layoutStore.showHover({
    prompt: "download",
    confirm: (format: DownloadFormat) => {
      if (req.value === null) return false;
      layoutStore.closeHovers();

      const files: string[] = [];

      for (const i of fileStore.selected) {
        files.push(req.value.items[i].path);
      }

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
  // Destroyed
  window.removeEventListener("keydown", keyEvent);
});
</script>
