<template>
  <div class="flex flex-col">
    <div class="text-lg font-medium text-gray-900 dark:text-gray-100">
      <h2>{{ $t("prompts.currentPassword") }}</h2>
    </div>

    <div class="px-6 py-4 flex flex-col gap-3">
      <p>
        {{ $t("prompts.currentPasswordMessage") }}
      </p>
      <input
        id="focus-prompt"
        class="form-control"
        type="password"
        @keyup.enter="submit"
        v-model="password"
      />
    </div>

    <div
      class="flex flex-wrap justify-end items-center gap-2 px-6 py-4 bg-gray-50 dark:bg-gray-900 rounded-b-lg"
    >
      <button
        class="btn btn-white btn-soft"
        @click="cancel"
        :aria-label="$t('buttons.cancel')"
        :title="$t('buttons.cancel')"
      >
        {{ $t("buttons.cancel") }}
      </button>
      <button
        @click="submit"
        class="btn btn-blue btn-soft"
        type="submit"
        :aria-label="$t('buttons.ok')"
        :title="$t('buttons.ok')"
      >
        {{ $t("buttons.ok") }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from "vue";
import { useLayoutStore } from "@/stores/layout";
const layoutStore = useLayoutStore();

const { currentPrompt } = layoutStore;

const password = ref("");

const submit = (event: Event) => {
  currentPrompt?.confirm(event, password.value);
};

const cancel = () => {
  layoutStore.closeHovers();
};
</script>
