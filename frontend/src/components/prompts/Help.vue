<template>
  <div class="flex flex-col">
    <div class="text-lg font-medium text-gray-900 dark:text-gray-100">
      <h2>{{ $t("help.help") }}</h2>
    </div>

    <div class="px-6 py-4 flex flex-col gap-3">
      <ul>
        <li><strong>F1</strong> - {{ $t("help.f1") }}</li>
        <li><strong>F2</strong> - {{ $t("help.f2") }}</li>
        <li><strong>DEL</strong> - {{ $t("help.del") }}</li>
        <li><strong>ESC</strong> - {{ $t("help.esc") }}</li>
        <li><strong>CTRL + S</strong> - {{ $t("help.ctrl.s") }}</li>
        <li><strong>CTRL + SHIFT + F</strong> - {{ $t("help.ctrl.f") }}</li>
        <li><strong>CTRL + Click</strong> - {{ $t("help.ctrl.click") }}</li>
        <li><strong>Click</strong> - {{ $t("help.click") }}</li>
        <li><strong>Double click</strong> - {{ $t("help.doubleClick") }}</li>
      </ul>
    </div>

    <div
      class="flex flex-wrap justify-end items-center gap-2 px-6 py-4 bg-gray-50 dark:bg-gray-900 rounded-b-lg"
    >
      <button
        id="focus-prompt"
        type="submit"
        @click="closeHovers"
        class="btn btn-blue btn-soft"
        :aria-label="$t('buttons.ok')"
        :title="$t('buttons.ok')"
        tabindex="1"
      >
        {{ $t("buttons.ok") }}
      </button>
    </div>
  </div>
</template>

<script>
import { mapActions } from "pinia";
import { useLayoutStore } from "@/stores/layout";

export default {
  name: "help",
  methods: {
    ...mapActions(useLayoutStore, ["closeHovers"]),
  },
};
</script>
