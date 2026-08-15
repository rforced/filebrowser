<template>
  <div class="row">
    <div class="column">
      <form class="card" @submit="updateSettings">
        <div class="text-lg font-medium text-gray-900 dark:text-gray-100">
          <h2>{{ t("settings.profileSettings") }}</h2>
        </div>

        <div class="px-6 py-4 flex flex-col gap-3">
          <p>
            <input type="checkbox" name="hideDotfiles" v-model="hideDotfiles" />
            {{ t("settings.hideDotfiles") }}
          </p>
          <p>
            <input type="checkbox" name="singleClick" v-model="singleClick" />
            {{ t("settings.singleClick") }}
          </p>
          <p>
            <input
              type="checkbox"
              name="redirectAfterCopyMove"
              v-model="redirectAfterCopyMove"
            />
            {{ t("settings.redirectAfterCopyMove") }}
          </p>
          <p>
            <input type="checkbox" name="dateFormat" v-model="dateFormat" />
            {{ t("settings.setDateFormat") }}
          </p>
          <h3>{{ t("settings.language") }}</h3>
          <languages class="form-control" v-model:locale="locale"></languages>

          <h3>{{ t("settings.aceEditorTheme") }}</h3>
          <AceEditorTheme
            class="form-control"
            v-model:aceEditorTheme="aceEditorTheme"
            id="aceTheme"
          ></AceEditorTheme>
        </div>

        <div
          class="flex flex-wrap justify-end items-center gap-2 px-6 py-4 bg-gray-50 dark:bg-gray-900 rounded-b-lg"
        >
          <input
            class="btn btn-blue btn-soft"
            type="submit"
            name="submitProfile"
            :value="t('buttons.update')"
          />
        </div>
      </form>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useAuthStore } from "@/stores/auth";
import { useLayoutStore } from "@/stores/layout";
import { users as api } from "@/api";
import AceEditorTheme from "@/components/settings/AceEditorTheme.vue";
import Languages from "@/components/settings/Languages.vue";
import { inject, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";

const layoutStore = useLayoutStore();
const authStore = useAuthStore();
const { t } = useI18n();

const $showSuccess = inject<IToastSuccess>("$showSuccess")!;
const $showError = inject<IToastError>("$showError")!;

const hideDotfiles = ref<boolean>(false);
const singleClick = ref<boolean>(false);
const redirectAfterCopyMove = ref<boolean>(false);
const dateFormat = ref<boolean>(false);
const locale = ref<string>("");
const aceEditorTheme = ref<string>("");

onMounted(async () => {
  layoutStore.loading = true;
  if (authStore.user === null) return false;
  locale.value = authStore.user.locale;
  hideDotfiles.value = authStore.user.hideDotfiles;
  singleClick.value = authStore.user.singleClick;
  redirectAfterCopyMove.value = authStore.user.redirectAfterCopyMove;
  dateFormat.value = authStore.user.dateFormat;
  aceEditorTheme.value = authStore.user.aceEditorTheme;
  layoutStore.loading = false;

  return true;
});

const updateSettings = async (event: Event) => {
  event.preventDefault();

  try {
    if (authStore.user === null) throw new Error("User is not set!");

    const data = {
      ...authStore.user,
      id: authStore.user.id,
      locale: locale.value,
      hideDotfiles: hideDotfiles.value,
      singleClick: singleClick.value,
      redirectAfterCopyMove: redirectAfterCopyMove.value,
      dateFormat: dateFormat.value,
      aceEditorTheme: aceEditorTheme.value,
    };

    await api.update(data, [
      "locale",
      "hideDotfiles",
      "singleClick",
      "redirectAfterCopyMove",
      "dateFormat",
      "aceEditorTheme",
    ]);
    authStore.updateUser(data);
    $showSuccess(t("settings.settingsUpdated"));
  } catch (err) {
    if (err instanceof Error) {
      $showError(err);
    }
  }
};
</script>
