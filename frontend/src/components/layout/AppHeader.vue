<template>
  <header
    data-component="header"
    class="sticky top-0 flex gap-3 md:gap-4 items-center justify-between bg-gray-200 dark:bg-gray-900 border-b border-transparent dark:border-gray-700 p-3 md:px-6 z-50"
  >
    <router-link
      to="/files"
      class="outline-hidden focus:ring-3 ring-offset-4 ring-offset-gray-200 dark:ring-offset-gray-900 rounded-xs shrink-0"
      :aria-label="name"
    >
      <span
        class="font-semibold text-base md:text-lg whitespace-nowrap text-blue-600 dark:text-gray-100"
        >{{ name }}</span
      >
    </router-link>

    <div v-if="isLoggedIn && showSearch" class="flex-1 min-w-0 max-w-lg">
      <search />
    </div>
    <div v-else class="flex-1"></div>

    <div class="flex gap-2 shrink-0 items-center">
      <slot name="actions" />

      <template v-if="isLoggedIn">
        <button
          v-tooltip="t('sidebar.settings')"
          type="button"
          class="btn btn-flex btn-gray h-10"
          :aria-label="t('sidebar.settings')"
          @click="toSettings"
        >
          <i class="fa-solid fa-user"></i>
          <span class="hidden md:inline font-medium">{{ displayName }}</span>
        </button>

        <button
          v-tooltip="t('sidebar.logout')"
          type="button"
          class="btn btn-flex btn-gray h-10"
          :aria-label="t('sidebar.logout')"
          @click="logout()"
        >
          <i class="fa-solid fa-right-from-bracket"></i>
        </button>
      </template>

      <router-link
        v-else-if="!hideLoginButton"
        v-tooltip="t('sidebar.login')"
        to="/login"
        class="btn btn-flex btn-gray h-10"
        :aria-label="t('sidebar.login')"
      >
        <i class="fa-solid fa-right-to-bracket"></i>
        <span class="hidden md:inline font-medium">{{
          t("sidebar.login")
        }}</span>
      </router-link>
    </div>
  </header>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { useRouter } from "vue-router";
import { storeToRefs } from "pinia";

import { useAuthStore } from "@/stores/auth";
import * as auth from "@/utils/auth";
import { hideLoginButton, name } from "@/utils/constants";
import Search from "@/components/Search.vue";

withDefaults(defineProps<{ showSearch?: boolean }>(), { showSearch: true });

const { t } = useI18n();
const router = useRouter();
const authStore = useAuthStore();
const { user, isLoggedIn } = storeToRefs(authStore);
const displayName = computed(() => user.value?.username?.split("@")[0] ?? "");
const toSettings = () => router.push({ path: "/settings/profile" });
const logout = auth.logout;
</script>
