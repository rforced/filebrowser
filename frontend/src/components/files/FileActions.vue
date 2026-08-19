<template>
  <div v-if="variant === 'stack'" class="hidden md:flex flex-col gap-2">
    <template v-for="(group, groupIndex) in stackGroups" :key="groupIndex">
      <hr v-if="groupIndex > 0" class="border-gray-200 dark:border-gray-700" />

      <div class="grid grid-cols-2 gap-2">
        <template v-for="entry in group" :key="entry.id">
          <a
            v-if="entry.id === 'create-job'"
            :href="jobUrl"
            :target="linkTarget"
            rel="noopener noreferrer"
            class="btn btn-menu btn-white btn-soft min-w-0"
            :class="entry.span === 2 ? 'col-span-2' : ''"
          >
            <i class="fa-kit fa-converge-mark fa-fw"></i>
            <span class="truncate">{{ t("sidebar.createJob") }}</span>
          </a>

          <router-link
            v-else-if="entry.id === 'settings'"
            to="/settings/profile"
            class="btn btn-menu btn-white btn-soft min-w-0"
            :class="entry.span === 2 ? 'col-span-2' : ''"
          >
            <i class="fa-solid fa-gear fa-fw"></i>
            <span class="truncate">{{ t("sidebar.settings") }}</span>
          </router-link>

          <button
            v-else-if="entry.action"
            :id="`${entry.action.id}-button`"
            type="button"
            class="btn btn-menu btn-soft min-w-0"
            :class="[
              entry.action.variant ?? 'btn-white',
              entry.span === 2 ? 'col-span-2' : '',
            ]"
            :disabled="!entry.action.enabled"
            @click="entry.action.run"
          >
            <i
              class="fa-solid fa-fw"
              :class="buttonIcon(entry.action.id, entry.action.icon)"
            ></i>
            <span class="truncate">{{ entry.action.label }}</span>
          </button>
        </template>
      </div>
    </template>
  </div>

  <div v-else class="md:hidden -mx-4 px-4 overflow-x-auto">
    <div class="flex gap-2 w-max pb-1">
      <button
        v-for="action in actions"
        :id="`${action.id}-button-mobile`"
        :key="action.id"
        type="button"
        class="btn btn-flex btn-soft whitespace-nowrap"
        :class="action.variant ?? 'btn-white'"
        :disabled="!action.enabled"
        @click="action.run"
      >
        <i
          class="fa-solid fa-fw"
          :class="buttonIcon(action.id, action.icon)"
        ></i>
        <span>{{ action.label }}</span>
      </button>

      <a
        v-if="jobUrl"
        :href="jobUrl"
        :target="linkTarget"
        rel="noopener noreferrer"
        class="btn btn-flex btn-white btn-soft whitespace-nowrap"
      >
        <i class="fa-kit fa-converge-mark fa-fw"></i>
        <span>{{ t("sidebar.createJob") }}</span>
      </a>

      <router-link
        v-if="embedded"
        to="/settings/profile"
        class="btn btn-flex btn-white btn-soft whitespace-nowrap"
      >
        <i class="fa-solid fa-gear fa-fw"></i>
        <span>{{ t("sidebar.settings") }}</span>
      </router-link>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";

import { useFileActions, type FileAction } from "@/composables/useFileActions";
import { buttonIcon } from "@/utils/buttons";
import { domain, teamId, filesystemId } from "@/utils/constants";
import { embedded } from "@/utils/embedded";

withDefaults(defineProps<{ variant?: "stack" | "rail" }>(), {
  variant: "stack",
});

const { t } = useI18n();
const route = useRoute();
const { actions } = useFileActions();

// Inside the platform's iframe the deep link should carry the whole page over
// to the platform rather than spawn a tab from within the frame.
const linkTarget = embedded ? "_top" : "_blank";

const STACK_LAYOUT: string[][][] = [
  [
    ["new-folder", "new-file"],
    ["upload", "download"],
  ],
  [["create-job"], ["converge-clean"]],
  [["info", "rename"], ["copy", "move"], ["share"], ["extract"], ["delete"]],
  [["settings"]],
];

interface StackEntry {
  id: string;
  action?: FileAction;
  span: 1 | 2;
}

const jobUrl = computed(() => {
  if (!domain || !teamId || !filesystemId) return "";

  const folderPath = route.path.replace(/^\/files/, "") || "/";
  return `${domain}/${teamId}/jobs/create?sid=${filesystemId}&stype=filesystem&path=${encodeURIComponent(folderPath)}`;
});

const stackGroups = computed<StackEntry[][]>(() => {
  const byId = new Map(actions.value.map((action) => [action.id, action]));

  return STACK_LAYOUT.map((rows) => {
    const group: StackEntry[] = [];

    for (const row of rows) {
      const entries: StackEntry[] = [];

      for (const id of row) {
        if (id === "create-job") {
          if (jobUrl.value) entries.push({ id, span: 2 });
        } else if (id === "settings") {
          // Only embedded: standalone keeps its header button.
          if (embedded) entries.push({ id, span: 2 });
        } else {
          const action = byId.get(id);
          if (action) entries.push({ id, action, span: 2 });
        }
      }

      if (entries.length === 2) {
        entries[0].span = 1;
        entries[1].span = 1;
      }

      group.push(...entries);
    }

    return group;
  }).filter((group) => group.length > 0);
});
</script>
