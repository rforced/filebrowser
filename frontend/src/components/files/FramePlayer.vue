<template>
  <div
    class="flex items-center gap-2 px-3 md:px-6 py-1.5 border-b border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-800 shrink-0"
  >
    <button
      v-tooltip="t('h5View.prevFrame')"
      type="button"
      class="action disabled:opacity-40"
      :aria-label="t('h5View.prevFrame')"
      :disabled="frameIndex === 0"
      @click="emit('step', -1)"
    >
      <i class="fa-solid fa-backward-step"></i>
    </button>
    <button
      type="button"
      class="action"
      :aria-label="playing ? t('buttons.pause') : t('buttons.play')"
      @click="emit('toggle')"
    >
      <i class="fa-solid" :class="playing ? 'fa-pause' : 'fa-play'"></i>
    </button>
    <button
      v-tooltip="t('h5View.nextFrame')"
      type="button"
      class="action disabled:opacity-40"
      :aria-label="t('h5View.nextFrame')"
      :disabled="frameIndex >= total - 1"
      @click="emit('step', 1)"
    >
      <i class="fa-solid fa-forward-step"></i>
    </button>

    <input
      type="range"
      class="flex-1 min-w-24 accent-blue-500"
      :min="0"
      :max="total - 1"
      :value="frameIndex"
      :aria-label="t('h5View.frame')"
      @input="onScrub"
    />

    <span
      class="text-xs tabular-nums text-gray-600 dark:text-gray-300 shrink-0"
    >
      {{ caption }}
    </span>

    <select
      :value="fps"
      class="form-control py-0.5 text-xs w-auto"
      :aria-label="t('h5View.playbackSpeed')"
      @change="onFps"
    >
      <option v-for="rate in [2, 5, 10]" :key="rate" :value="rate">
        {{ t("sequence.speed", { fps: rate }) }}
      </option>
    </select>

    <button
      v-if="canRescale"
      v-tooltip="t('h5View.rescale')"
      type="button"
      class="action"
      :aria-label="t('h5View.rescale')"
      @click="emit('rescale')"
    >
      <i class="fa-solid fa-arrows-left-right"></i>
    </button>
  </div>

  <div
    v-if="capped"
    class="flex items-center gap-1.5 px-3 md:px-6 py-1.5 border-b border-amber-200 dark:border-amber-900 bg-amber-50 dark:bg-amber-950 text-xs text-amber-800 dark:text-amber-200 shrink-0"
  >
    <i class="fa-solid fa-circle-pause"></i>
    {{ t("h5View.playbackCapped", { minutes: cappedMinutes }) }}
  </div>
</template>

<script setup lang="ts">
import { useI18n } from "vue-i18n";

defineProps<{
  playing: boolean;
  frameIndex: number;
  total: number;
  caption: string;
  fps: number;
  canRescale: boolean;
  capped: boolean;
  cappedMinutes: number;
}>();

const emit = defineEmits<{
  toggle: [];
  step: [delta: number];
  scrub: [index: number];
  "update:fps": [fps: number];
  rescale: [];
}>();

const { t } = useI18n({});

const onScrub = (event: Event) =>
  emit("scrub", Number((event.target as HTMLInputElement).value));

const onFps = (event: Event) =>
  emit("update:fps", Number((event.target as HTMLSelectElement).value));
</script>
