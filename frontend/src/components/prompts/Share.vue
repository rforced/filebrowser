<template>
  <div class="card floating" id="share">
    <div class="card-title">
      <h2>{{ t("buttons.share") }}</h2>
    </div>

    <template v-if="listing">
      <div class="card-content">
        <table>
          <tr>
            <th>#</th>
            <th>{{ t("settings.shareDuration") }}</th>
            <th></th>
            <th></th>
          </tr>

          <tr v-for="link in links" :key="link.hash">
            <td>{{ link.hash.substring(0, 8) }}...</td>
            <td>
              <template v-if="link.expire !== 0">{{
                humanTime(link.expire)
              }}</template>
              <template v-else>{{ t("permanent") }}</template>
            </td>
            <td class="small">
              <button
                class="action"
                :aria-label="t('buttons.copyToClipboard')"
                :title="t('buttons.copyToClipboard')"
                @click="copyToClipboard(buildLink(link))"
              >
                <i class="material-icons">content_paste</i>
              </button>
            </td>
            <td class="small">
              <button
                class="action"
                @click="deleteLink($event, link)"
                :aria-label="t('buttons.delete')"
                :title="t('buttons.delete')"
              >
                <i class="material-icons">delete</i>
              </button>
            </td>
          </tr>
        </table>
      </div>

      <div class="card-action">
        <button
          class="button button--flat button--grey"
          @click="layoutStore.closeHovers"
          :aria-label="t('buttons.close')"
          :title="t('buttons.close')"
          tabindex="2"
        >
          {{ t("buttons.close") }}
        </button>
        <button
          id="focus-prompt"
          class="button button--flat button--blue"
          @click="() => switchListing()"
          :aria-label="t('buttons.new')"
          :title="t('buttons.new')"
          tabindex="1"
        >
          {{ t("buttons.new") }}
        </button>
      </div>
    </template>

    <template v-else>
      <div class="card-content">
        <p>{{ t("settings.shareDuration") }}</p>
        <div class="input-group input">
          <vue-number-input
            center
            controls
            size="small"
            :max="2147483647"
            :min="1"
            @keyup.enter="submit"
            v-model="time"
            tabindex="1"
          />
          <select
            class="right"
            v-model="unit"
            :aria-label="t('time.unit')"
            tabindex="2"
          >
            <option value="seconds">{{ t("time.seconds") }}</option>
            <option value="minutes">{{ t("time.minutes") }}</option>
            <option value="hours">{{ t("time.hours") }}</option>
            <option value="days">{{ t("time.days") }}</option>
          </select>
        </div>
        <p>{{ t("settings.password") }}</p>
        <input
          class="input input--block"
          type="password"
          v-model.trim="password"
          required
          tabindex="3"
        />
      </div>

      <div class="card-action">
        <button
          class="button button--flat button--grey"
          @click="() => switchListing()"
          :aria-label="t('buttons.cancel')"
          :title="t('buttons.cancel')"
          tabindex="5"
        >
          {{ t("buttons.cancel") }}
        </button>
        <button
          id="focus-prompt"
          class="button button--flat button--blue"
          @click="submit"
          :aria-label="t('buttons.share')"
          :title="t('buttons.share')"
          tabindex="4"
        >
          {{ t("buttons.share") }}
        </button>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, inject, onBeforeMount } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";
import { useFileStore } from "@/stores/file";
import { useLayoutStore } from "@/stores/layout";
import * as api from "@/api/index";
import dayjs from "dayjs";
import { copy } from "@/utils/clipboard";

const { t } = useI18n();
const route = useRoute();
const fileStore = useFileStore();
const layoutStore = useLayoutStore();

const $showError = inject<IToastError>("$showError")!;
const $showSuccess = inject<IToastSuccess>("$showSuccess")!;

const time = ref(1);
const unit = ref("hours");
const links = ref<Share[]>([]);
const password = ref("");
const listing = ref(true);

const url = computed(() => {
  if (!fileStore.isListing) {
    return route.path;
  }

  if (fileStore.selectedCount === 0 || fileStore.selectedCount > 1) {
    // This shouldn't happen.
    return;
  }

  return fileStore.req!.items[fileStore.selected[0]].url;
});

function sort() {
  links.value = links.value.sort((a, b) => {
    if (a.expire === 0) return -1;
    if (b.expire === 0) return 1;
    return new Date(a.expire).getTime() - new Date(b.expire).getTime();
  });
}

function humanTime(timeVal: number): string {
  return dayjs(timeVal * 1000).fromNow();
}

function buildLink(share: Share): string {
  return api.share.getShareURL(share);
}

function switchListing() {
  if (links.value.length == 0 && !listing.value) {
    layoutStore.closeHovers();
  }

  listing.value = !listing.value;
}

function copyToClipboard(text: string) {
  copy({ text }).then(
    () => {
      $showSuccess(t("success.linkCopied"));
    },
    () => {
      copy({ text }, { permission: true }).then(
        () => {
          $showSuccess(t("success.linkCopied"));
        },
        (e: Error) => {
          $showError(e);
        }
      );
    }
  );
}

async function submit() {
  if (!password.value) {
    $showError(t("prompts.passwordRequired"));
    return;
  }
  if (!time.value || time.value <= 0) {
    $showError(t("prompts.expirationRequired"));
    return;
  }
  try {
    const res = await api.share.create(
      url.value!,
      password.value,
      time.value.toString(),
      unit.value
    );

    links.value.push(res as Share);
    sort();

    time.value = 1;
    unit.value = "hours";
    password.value = "";

    listing.value = true;
  } catch (e) {
    $showError(e as Error);
  }
}

async function deleteLink(event: Event, link: Share) {
  event.preventDefault();
  try {
    await api.share.remove(link.hash);
    links.value = links.value.filter((item) => item.hash !== link.hash);

    if (links.value.length == 0) {
      listing.value = false;
    }
  } catch (e) {
    $showError(e as Error);
  }
}

onBeforeMount(async () => {
  try {
    const fetchedLinks = await api.share.get(url.value!);
    links.value = fetchedLinks as unknown as Share[];
    sort();

    if (links.value.length == 0) {
      listing.value = false;
    }
  } catch (e) {
    $showError(e as Error);
  }
});
</script>
