<template>
  <div v-if="variant === 'stack'" class="hidden md:flex flex-col gap-3">
    <template v-for="(action, index) in actions" :key="action.id">
      <hr
        v-if="dividerBefore(index)"
        class="border-gray-200 dark:border-gray-700"
      />

      <button
        :id="`${action.id}-button`"
        type="button"
        class="btn btn-menu btn-soft"
        :class="action.variant ?? 'btn-white'"
        :disabled="!action.enabled"
        @click="action.run"
      >
        <i
          class="fa-solid fa-fw"
          :class="buttonIcon(action.id, action.icon)"
        ></i>
        <span class="truncate">{{ action.label }}</span>
      </button>
    </template>

    <template v-if="jobUrl">
      <hr class="border-gray-200 dark:border-gray-700" />
      <a
        :href="jobUrl"
        target="_blank"
        rel="noopener noreferrer"
        class="btn btn-menu btn-white btn-soft"
      >
        <i class="fa-kit fa-converge-mark fa-fw"></i>
        <span class="truncate">{{ t("sidebar.createJob") }}</span>
      </a>
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
        target="_blank"
        rel="noopener noreferrer"
        class="btn btn-flex btn-white btn-soft whitespace-nowrap"
      >
        <i class="fa-kit fa-converge-mark fa-fw"></i>
        <span>{{ t("sidebar.createJob") }}</span>
      </a>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";

import { useFileActions } from "@/composables/useFileActions";
import { buttonIcon } from "@/utils/buttons";
import { domain, teamId, filesystemId } from "@/utils/constants";

withDefaults(defineProps<{ variant?: "stack" | "rail" }>(), {
  variant: "stack",
});

const { t } = useI18n();
const route = useRoute();
const { actions } = useFileActions();

const GROUP_STARTS = new Set(["download", "delete"]);

const dividerBefore = (index: number) =>
  index > 0 && GROUP_STARTS.has(actions.value[index].id);

const jobUrl = computed(() => {
  if (!domain || !teamId || !filesystemId) return "";

  const folderPath = route.path.replace(/^\/files/, "") || "/";
  return `${domain}/${teamId}/jobs/create?sid=${filesystemId}&stype=filesystem&path=${encodeURIComponent(folderPath)}`;
});
</script>
