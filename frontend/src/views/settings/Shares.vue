<template>
  <errors v-if="error" :errorCode="error.status" />
  <div class="row" v-else-if="!layoutStore.loading">
    <div class="column">
      <div class="card">
        <div class="card-title">
          <h2>{{ t("settings.shareManagement") }}</h2>
        </div>

        <div
          class="px-6 py-4 flex flex-col gap-3 flex-1 min-h-0 overflow-y-auto"
          v-if="links.length > 0"
        >
          <table>
            <thead>
              <tr>
                <th>{{ t("settings.path") }}</th>
                <th>{{ t("settings.shareDuration") }}</th>
                <th>{{ t("settings.owner") }}</th>
                <th></th>
                <th></th>
              </tr>
            </thead>

            <tbody>
              <tr v-for="link in links" :key="link.hash">
                <td>
                  <a :href="buildLink(link)" target="_blank">{{ link.path }}</a>
                </td>
                <td>
                  <template v-if="link.expire !== 0">{{
                    humanTime(link.expire)
                  }}</template>
                  <template v-else>{{ t("permanent") }}</template>
                </td>
                <td>{{ link.username }}</td>
                <td class="small">
                  <button
                    class="action"
                    @click="deleteLink($event, link)"
                    :aria-label="t('buttons.delete')"
                    :title="t('buttons.delete')"
                  >
                    <i class="fa-solid fa-trash"></i>
                  </button>
                </td>
                <td class="small">
                  <button
                    class="action copy-clipboard"
                    :aria-label="t('buttons.copyToClipboard')"
                    :title="t('buttons.copyToClipboard')"
                    @click="copyToClipboard(buildLink(link))"
                  >
                    <i class="fa-solid fa-paste"></i>
                  </button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <h2 class="message" v-else>
          <span>{{ t("files.lonely") }}</span>
        </h2>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useLayoutStore } from "@/stores/layout";
import { share as api } from "@/api";
import dayjs from "dayjs";
import Errors from "@/views/Errors.vue";
import { inject, ref, onMounted } from "vue";
import { useI18n } from "vue-i18n";
import { StatusError } from "@/api/utils";
import { copy } from "@/utils/clipboard";

const $showError = inject<IToastError>("$showError")!;
const $showSuccess = inject<IToastSuccess>("$showSuccess")!;
const { t } = useI18n();

const layoutStore = useLayoutStore();

const error = ref<StatusError | null>(null);
const links = ref<Share[]>([]);

onMounted(async () => {
  layoutStore.loading = true;

  try {
    links.value = await api.list();
  } catch (err) {
    if (err instanceof Error) {
      error.value = err;
    }
  } finally {
    layoutStore.loading = false;
  }
});

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

const deleteLink = async (event: Event, link: any) => {
  event.preventDefault();

  layoutStore.showHover({
    prompt: "share-delete",
    confirm: () => {
      layoutStore.closeHovers();

      try {
        api.remove(link.hash);
        links.value = links.value.filter((item) => item.hash !== link.hash);
        $showSuccess(t("settings.shareDeleted"));
      } catch (err) {
        if (err instanceof Error) {
          $showError(err);
        }
      }
    },
  });
};
const humanTime = (time: number) => {
  return dayjs(time * 1000).fromNow();
};

const buildLink = (share: Share) => {
  return api.getShareURL(share);
};
</script>
