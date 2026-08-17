<template>
  <Card
    v-if="uploadStore.activeUploads.size > 0"
    class="flex flex-col overflow-hidden w-full pointer-events-auto"
  >
    <div class="flex items-start gap-3 px-4 py-3">
      <div class="flex-1 min-w-0 flex flex-col gap-1">
        <h2
          class="text-sm font-medium text-gray-900 dark:text-gray-100 truncate"
        >
          {{
            $t("prompts.uploadFiles", { files: uploadStore.pendingUploadCount })
          }}
        </h2>

        <div
          class="text-xs text-gray-600 dark:text-gray-300 flex flex-wrap gap-x-3 gap-y-0.5 tabular-nums"
        >
          <span>{{ speedText }}/s</span>
          <span>{{ formattedETA }} remaining</span>
          <span>{{ sentPercent }}%</span>
          <span>{{ sentMbytes }} / {{ totalMbytes }}</span>
        </div>
      </div>

      <div class="flex gap-1 shrink-0">
        <button
          v-tooltip="$t('upload.abortUpload')"
          type="button"
          class="w-7 h-7 flex items-center justify-center rounded-md text-gray-500 dark:text-gray-400 hover:text-red-600 dark:hover:text-red-300 hover:bg-gray-200 dark:hover:bg-gray-700 transition"
          aria-label="Abort upload"
          @click="abortAll"
        >
          <i class="fa-solid fa-circle-xmark"></i>
        </button>

        <button
          type="button"
          class="w-7 h-7 flex items-center justify-center rounded-md text-gray-500 dark:text-gray-400 hover:text-gray-800 dark:hover:text-gray-100 hover:bg-gray-200 dark:hover:bg-gray-700 transition"
          aria-label="Toggle file upload list"
          :aria-expanded="open"
          @click="toggle"
        >
          <i
            class="fa-solid fa-chevron-up transition-transform"
            :class="open ? 'rotate-180' : ''"
          ></i>
        </button>
      </div>
    </div>

    <div class="h-1 bg-gray-200 dark:bg-gray-700">
      <div
        class="h-full bg-blue-500 dark:bg-teal-600 transition-[width] duration-200"
        :style="{ width: sentPercent + '%' }"
      ></div>
    </div>

    <div
      v-if="open"
      class="file-icons max-h-64 overflow-y-auto overscroll-contain divide-y divide-gray-100 dark:divide-gray-700"
    >
      <div
        v-for="upload in uploadStore.activeUploads"
        :key="upload.path"
        class="px-4 py-2 flex flex-col gap-1"
        :data-dir="upload.type === 'dir'"
        :data-type="upload.type"
        :aria-label="upload.name"
      >
        <div class="flex items-center gap-2 text-sm min-w-0">
          <i
            class="fa-solid fa-fw shrink-0"
            :class="[iconFor(upload).icon, iconFor(upload).color]"
          ></i>
          <span class="truncate">{{ upload.name }}</span>
        </div>

        <div
          class="h-1 bg-gray-200 dark:bg-gray-700 rounded-full overflow-hidden"
        >
          <div
            class="h-full bg-blue-500 dark:bg-teal-600 transition-[width] duration-200"
            :style="{
              width: (upload.sentBytes / upload.totalBytes) * 100 + '%',
            }"
          ></div>
        </div>
      </div>
    </div>
  </Card>
</template>

<script setup lang="ts">
import { useFileStore } from "@/stores/file";
import { useUploadStore } from "@/stores/upload";
import { storeToRefs } from "pinia";
import { computed, ref, watch } from "vue";
import buttons from "@/utils/buttons";
import { useI18n } from "vue-i18n";
import { partial } from "filesize";
import Card from "@/components/ui/Card.vue";
import { fileIcon } from "@/utils/fileIcons";

const { t } = useI18n({});

const open = ref<boolean>(false);
const speed = ref<number>(0);
const eta = ref<number>(Infinity);

const fileStore = useFileStore();
const uploadStore = useUploadStore();

const { sentBytes, totalBytes } = storeToRefs(uploadStore);

const byteToMbyte = partial({ exponent: 2 });
const byteToKbyte = partial({ exponent: 1 });

const sentPercent = computed(() =>
  ((uploadStore.sentBytes / uploadStore.totalBytes) * 100).toFixed(2)
);

const sentMbytes = computed(() => byteToMbyte(uploadStore.sentBytes));
const totalMbytes = computed(() => byteToMbyte(uploadStore.totalBytes));
const speedText = computed(() => {
  const bytes = speed.value;

  if (bytes < 1024 * 1024) {
    const kb = parseFloat(byteToKbyte(bytes));
    return `${kb.toFixed(2)} KB`;
  } else {
    const mb = parseFloat(byteToMbyte(bytes));
    return `${mb.toFixed(2)} MB`;
  }
});

let lastSpeedUpdate: number = 0;
let recentSpeeds: number[] = [];

let lastThrottleTime = 0;

const throttledCalculateSpeed = (sentBytes: number, oldSentBytes: number) => {
  const now = Date.now();
  if (now - lastThrottleTime < 100) {
    return;
  }

  lastThrottleTime = now;
  calculateSpeed(sentBytes, oldSentBytes);
};

const calculateSpeed = (sentBytes: number, oldSentBytes: number) => {
  // Reset the state when the uploads batch is complete
  if (sentBytes === 0) {
    lastSpeedUpdate = 0;
    recentSpeeds = [];

    eta.value = Infinity;
    speed.value = 0;

    return;
  }

  const elapsedTime = (Date.now() - (lastSpeedUpdate ?? 0)) / 1000;
  const bytesSinceLastUpdate = sentBytes - oldSentBytes;
  const currentSpeed = bytesSinceLastUpdate / elapsedTime;

  recentSpeeds.push(currentSpeed);
  if (recentSpeeds.length > 5) {
    recentSpeeds.shift();
  }

  const recentSpeedsAverage =
    recentSpeeds.reduce((acc, curr) => acc + curr) / recentSpeeds.length;

  // Use the current speed for the first update to avoid smoothing lag
  if (recentSpeeds.length === 1) {
    speed.value = currentSpeed;
  }

  speed.value = recentSpeedsAverage * 0.2 + speed.value * 0.8;

  lastSpeedUpdate = Date.now();

  calculateEta();
};

const calculateEta = () => {
  if (speed.value === 0) {
    eta.value = Infinity;

    return Infinity;
  }

  const remainingSize = uploadStore.totalBytes - uploadStore.sentBytes;
  const speedBytesPerSecond = speed.value;

  eta.value = remainingSize / speedBytesPerSecond;
};

watch(sentBytes, throttledCalculateSpeed);

watch(totalBytes, (totalBytes, oldTotalBytes) => {
  if (oldTotalBytes !== 0) {
    return;
  }

  // Mark the start time of a new upload batch
  lastSpeedUpdate = Date.now();
});

const formattedETA = computed(() => {
  if (!eta.value || eta.value === Infinity) {
    return "--:--:--";
  }

  let totalSeconds = eta.value;
  const hours = Math.floor(totalSeconds / 3600);
  totalSeconds %= 3600;
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = Math.round(totalSeconds % 60);

  return `${hours.toString().padStart(2, "0")}:${minutes
    .toString()
    .padStart(2, "0")}:${seconds.toString().padStart(2, "0")}`;
});

const iconFor = (upload: { name: string; type: string }) =>
  fileIcon({
    isDir: upload.type === "dir",
    type: upload.type,
    extension: upload.name.slice(upload.name.lastIndexOf(".")).toLowerCase(),
    name: upload.name,
  });

const toggle = () => {
  open.value = !open.value;
};

const abortAll = () => {
  if (confirm(t("upload.abortUpload"))) {
    buttons.done("upload");
    open.value = false;
    uploadStore.abort();
    fileStore.reload = true; // Trigger reload in the file store
  }
};
</script>
