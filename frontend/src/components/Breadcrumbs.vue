<template>
  <nav
    class="flex items-center gap-0.5 text-base text-gray-600 dark:text-gray-300 flex-wrap"
    :aria-label="t('files.home')"
  >
    <component
      :is="element"
      v-tooltip="t('files.home')"
      :to="base || ''"
      :aria-label="t('files.home')"
      class="flex items-center gap-1.5 px-2.5 py-1.5 rounded-md transition"
      :class="interactive"
    >
      <i class="fa-solid fa-hard-drive text-lg"></i>
      <span class="sr-only">{{ t("files.home") }}</span>
    </component>

    <template v-for="(link, index) in items" :key="index">
      <i
        class="fa-solid fa-chevron-right text-sm text-gray-400 dark:text-gray-500 mx-0.5"
      ></i>

      <!-- The final crumb is the current location: emphasised, never a link. -->
      <span
        v-if="index === items.length - 1"
        class="px-2.5 py-1.5 font-semibold text-gray-900 dark:text-gray-100 break-all"
        aria-current="page"
        >{{ link.name }}</span
      >

      <component
        :is="element"
        v-else
        :to="link.url"
        class="px-2.5 py-1.5 rounded-md transition break-all"
        :class="interactive"
        >{{ link.name }}</component
      >
    </template>
  </nav>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";

const { t } = useI18n();

const route = useRoute();

const props = defineProps<{
  base: string;
  noLink?: boolean;
}>();

const items = computed(() => {
  const relativePath = route.path.replace(props.base, "");
  const parts = relativePath.split("/");

  if (parts[0] === "") {
    parts.shift();
  }

  if (parts[parts.length - 1] === "") {
    parts.pop();
  }

  const breadcrumbs: BreadCrumb[] = [];

  for (let i = 0; i < parts.length; i++) {
    if (i === 0) {
      breadcrumbs.push({
        name: decodeURIComponent(parts[i]),
        url: props.base + "/" + parts[i] + "/",
      });
    } else {
      breadcrumbs.push({
        name: decodeURIComponent(parts[i]),
        url: breadcrumbs[i - 1].url + parts[i] + "/",
      });
    }
  }

  if (breadcrumbs.length > 3) {
    while (breadcrumbs.length !== 4) {
      breadcrumbs.shift();
    }

    breadcrumbs[0].name = "...";
  }

  return breadcrumbs;
});

const element = computed(() => (props.noLink ? "span" : "router-link"));

const interactive = computed(() =>
  props.noLink ? "" : "hover:bg-gray-200 dark:hover:bg-gray-700"
);
</script>
