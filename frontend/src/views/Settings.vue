<template>
  <main class="flex flex-col gap-4 p-4">
    <Banner :title="t('sidebar.settings')" :subtitle="activeLabel">
      <router-link to="/files/" class="btn btn-flex btn-white btn-soft">
        <i class="fa-solid fa-folder-open"></i>
        <span>{{ t("sidebar.myFiles") }}</span>
      </router-link>
    </Banner>

    <hr class="border-gray-200 dark:border-gray-700" />

    <!-- Tabs, after Horizon's components/tabs/*. -->
    <nav
      class="flex gap-2 border-b border-gray-200 dark:border-gray-700 pl-1 overflow-x-auto"
      :aria-label="t('sidebar.settings')"
    >
      <router-link
        v-for="tab in tabs"
        :key="tab.to"
        :to="tab.to"
        :data-tab="tab.active ? 'active' : 'inactive'"
        class="relative whitespace-nowrap py-3 px-6 text-sm font-semibold rounded-t-lg border border-transparent transition-all duration-200 ease-in-out data-[tab=active]:text-blue-600 dark:data-[tab=active]:text-gray-100 data-[tab=active]:bg-white dark:data-[tab=active]:bg-gray-800 data-[tab=active]:border-blue-200 dark:data-[tab=active]:border-blue-700 data-[tab=active]:shadow-xs data-[tab=inactive]:text-slate-600 dark:data-[tab=inactive]:text-slate-400 data-[tab=inactive]:bg-slate-50 dark:data-[tab=inactive]:bg-gray-700 data-[tab=inactive]:hover:text-slate-800 dark:data-[tab=inactive]:hover:text-slate-200 data-[tab=inactive]:hover:bg-white dark:data-[tab=inactive]:hover:bg-gray-800 data-[tab=inactive]:hover:shadow-xs before:absolute before:bottom-0 before:left-1/2 before:h-0.5 before:w-0 before:bg-blue-500 before:transition-all before:duration-200 before:-translate-x-1/2 data-[tab=active]:before:w-full"
      >
        {{ tab.label }}
      </router-link>
    </nav>

    <Card v-if="loading" class="p-10">
      <div
        class="flex flex-col items-center gap-3 text-gray-600 dark:text-gray-300"
      >
        <i class="fa-solid fa-spinner fa-spin text-3xl"></i>
        <span class="text-sm font-medium">{{ t("files.loading") }}</span>
      </div>
    </Card>

    <div v-show="!loading" class="contents">
      <router-view />
    </div>
  </main>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";

import { useAuthStore } from "@/stores/auth";
import { useLayoutStore } from "@/stores/layout";
import Banner from "@/components/ui/Banner.vue";
import Card from "@/components/ui/Card.vue";

const { t } = useI18n();
const route = useRoute();

const authStore = useAuthStore();
const layoutStore = useLayoutStore();

const user = computed(() => authStore.user);
const loading = computed(() => layoutStore.loading);

const tabs = computed(() =>
  [
    {
      to: "/settings/profile",
      label: t("settings.profileSettings"),
      shown: true,
      // The user editor lives under /settings/users/:id but belongs to the
      // user-management tab, so it is matched by route name rather than path.
      active: route.path === "/settings/profile",
    },
    {
      to: "/settings/shares",
      label: t("settings.shareManagement"),
      shown: !!user.value?.perm.share,
      active: route.path === "/settings/shares",
    },
    {
      to: "/settings/global",
      label: t("settings.globalSettings"),
      shown: !!user.value?.perm.admin,
      active: route.path === "/settings/global",
    },
    {
      to: "/settings/users",
      label: t("settings.userManagement"),
      shown: !!user.value?.perm.admin,
      active: route.path === "/settings/users" || route.name === "User",
    },
  ].filter((tab) => tab.shown)
);

const activeLabel = computed(() => tabs.value.find((tab) => tab.active)?.label);
</script>
