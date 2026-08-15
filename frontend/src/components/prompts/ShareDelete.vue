<template>
  <div class="flex flex-col">
    <div class="px-6 py-4 flex flex-col gap-3">
      <p>{{ $t("prompts.deleteMessageShare", { path: "" }) }}</p>
    </div>
    <div
      class="flex flex-wrap justify-end items-center gap-2 px-6 py-4 bg-gray-50 dark:bg-gray-900 rounded-b-lg"
    >
      <button
        @click="closeHovers"
        class="btn btn-white btn-soft"
        :aria-label="$t('buttons.cancel')"
        :title="$t('buttons.cancel')"
        tabindex="2"
      >
        {{ $t("buttons.cancel") }}
      </button>
      <button
        id="focus-prompt"
        @click="submit"
        class="btn btn-red btn-soft"
        :aria-label="$t('buttons.delete')"
        :title="$t('buttons.delete')"
        tabindex="1"
      >
        {{ $t("buttons.delete") }}
      </button>
    </div>
  </div>
</template>

<script>
import { mapActions, mapState } from "pinia";
import { useLayoutStore } from "@/stores/layout";

export default {
  name: "share-delete",
  computed: {
    ...mapState(useLayoutStore, ["currentPrompt"]),
  },
  methods: {
    ...mapActions(useLayoutStore, ["closeHovers"]),
    submit: function () {
      this.currentPrompt?.confirm();
    },
  },
};
</script>
