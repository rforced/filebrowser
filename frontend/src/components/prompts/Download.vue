<template>
  <div class="flex flex-col" id="download">
    <div class="card-title">
      <h2>{{ t("prompts.download") }}</h2>
    </div>

    <div class="px-6 py-4 flex flex-col gap-3">
      <prompt-targets
        v-if="targets.length > 0"
        :items="targets"
        :label="t('prompts.downloading')"
      />

      <p>{{ t("prompts.downloadMessage") }}</p>

      <button
        id="focus-prompt"
        v-for="(ext, format) in formats"
        :key="format"
        class="btn btn-blue btn-soft w-full"
        @click="layoutStore.currentPrompt?.confirm(format)"
      >
        {{ ext }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { useLayoutStore } from "@/stores/layout";
import { usePromptTargets } from "@/composables/usePromptTargets";
import PromptTargets from "@/components/prompts/PromptTargets.vue";

const layoutStore = useLayoutStore();

const { t } = useI18n();

const { selectedTargets, currentTargets } = usePromptTargets();

const targets = computed(() =>
  selectedTargets.value.length > 0
    ? selectedTargets.value
    : currentTargets.value
);

// Kept in step with parseQueryAlgorithm in http/raw.go. Every extra format is
// another encoder in the binary and another decoder the downloader has to run.
const formats = {
  zip: "zip",
  tar: "tar",
  targz: "tar.gz",
  tarlz4: "tar.lz4",
  tarzst: "tar.zst",
};
</script>
