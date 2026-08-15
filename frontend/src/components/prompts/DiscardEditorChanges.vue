<template>
  <div class="flex flex-col">
    <div class="px-6 py-4 flex flex-col gap-3">
      <p>
        {{ $t("prompts.discardEditorChanges") }}
      </p>
    </div>
    <div
      class="flex flex-wrap justify-end items-center gap-2 px-6 py-4 bg-gray-50 dark:bg-gray-900 rounded-b-lg"
    >
      <button
        class="btn btn-white btn-soft"
        @click="closeHovers"
        :aria-label="$t('buttons.cancel')"
        :title="$t('buttons.cancel')"
        tabindex="3"
      >
        {{ $t("buttons.cancel") }}
      </button>
      <button
        class="btn btn-blue btn-soft"
        @click="currentPrompt.saveAction"
        :aria-label="$t('buttons.saveChanges')"
        :title="$t('buttons.saveChanges')"
        tabindex="1"
      >
        {{ $t("buttons.saveChanges") }}
      </button>
      <button
        id="focus-prompt"
        @click="currentPrompt.confirm"
        class="btn btn-red btn-soft"
        :aria-label="$t('buttons.discardChanges')"
        :title="$t('buttons.discardChanges')"
        tabindex="2"
      >
        {{ $t("buttons.discardChanges") }}
      </button>
    </div>
  </div>
</template>

<script>
import { useLayoutStore } from "@/stores/layout";
import { mapActions, mapState } from "pinia";

export default {
  name: "discardEditorChanges",
  computed: {
    ...mapState(useLayoutStore, ["currentPrompt"]),
  },
  methods: {
    ...mapActions(useLayoutStore, ["closeHovers"]),
  },
};
</script>
