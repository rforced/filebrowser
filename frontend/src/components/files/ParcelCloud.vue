<template>
  <div class="relative w-full h-full">
    <canvas ref="canvasEl" class="w-full h-full block"></canvas>

    <!-- Light-on-dark in both themes: the stage behind the canvas is dark.
         During playback the previous cloud stays up while the next loads, so
         the spinner only shows before there is anything to look at. -->
    <div
      v-if="loading && !cloud"
      class="absolute inset-0 flex items-center justify-center text-gray-400"
    >
      <i class="fa-solid fa-spinner fa-spin text-2xl"></i>
    </div>

    <div
      v-else-if="error"
      class="absolute inset-0 flex items-center justify-center p-4 text-center text-sm text-gray-300"
    >
      {{ error }}
    </div>

    <div
      v-else-if="cloud"
      class="absolute left-2 bottom-2 flex flex-col gap-1 rounded-md bg-white/85 dark:bg-gray-900/85 px-2.5 py-1.5 text-xs text-gray-700 dark:text-gray-200 backdrop-blur-sm"
    >
      <span>
        {{ t("h5View.parcelCount", { count: formatCount(cloud.count) }) }}
        <template v-if="cloud.stride > 1">
          &middot; {{ t("h5View.showingEvery", { n: cloud.stride }) }}
        </template>
      </span>

      <div
        v-if="cloud.scalar && cloud.values?.length"
        class="flex items-center gap-1.5"
      >
        <span class="shrink-0">{{ cloud.scalar }}</span>
        <span
          class="h-2 w-20 rounded-full"
          :style="{ background: `linear-gradient(to right, ${rampCss})` }"
        ></span>
        <span class="tabular-nums"
          >{{ formatValue(shownRange[0]) }}–{{
            formatValue(shownRange[1])
          }}</span
        >
      </div>
    </div>

    <div v-if="cloud" class="absolute right-2 bottom-2 flex gap-2">
      <button
        type="button"
        class="flex items-center justify-center w-9 h-9 rounded-full cursor-pointer transition-colors backdrop-blur-sm bg-white/85 text-gray-700 hover:bg-white dark:bg-gray-900/85 dark:text-gray-200 dark:hover:bg-gray-800"
        :aria-label="t('buttons.resetView')"
        :title="t('buttons.resetView')"
        @click="resetView"
      >
        <i class="fa-solid fa-crosshairs"></i>
      </button>
      <button
        type="button"
        class="flex items-center justify-center w-9 h-9 rounded-full cursor-pointer transition-colors backdrop-blur-sm bg-white/85 text-gray-700 hover:bg-white dark:bg-gray-900/85 dark:text-gray-200 dark:hover:bg-gray-800"
        :aria-label="t('buttons.savePng')"
        :title="t('buttons.savePng')"
        @click="savePng"
      >
        <i class="fa-solid fa-camera"></i>
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import {
  BufferGeometry,
  Float32BufferAttribute,
  PerspectiveCamera,
  Points,
  PointsMaterial,
  Scene,
  WebGLRenderer,
} from "three";
import { OrbitControls } from "three/addons/controls/OrbitControls.js";

import * as h5 from "@/api/h5";
import { formatCount, formatValue } from "@/utils/convergeH5";
import { NO_VALUE, normalize, rampAt, rampCss } from "@/utils/colorRamp";
import { fetchParcels } from "@/utils/parcelCache";
import { pngFilename, saveViewPng } from "@/utils/viewCapture";

const props = defineProps<{
  path: string;
  group: string;
  scalar?: string;
  // Pins the colour ramp to a fixed range — playback locks it so the colours
  // keep one meaning across frames instead of re-normalising every step.
  range?: [number, number] | null;
}>();

const emit = defineEmits<{
  // The frame's own value range, or null when the load failed. Playback paces
  // itself on this signal; an output written before injection starts is a
  // real, empty frame, not a failure.
  loaded: [range: [number, number] | null];
}>();

const { t } = useI18n({});

const canvasEl = ref<HTMLCanvasElement | null>(null);
const loading = ref(true);
const error = ref("");
const cloud = ref<h5.H5ParcelCloud | null>(null);

let renderer: WebGLRenderer | null = null;
let controls: OrbitControls | null = null;
let scene: Scene | null = null;
let camera: PerspectiveCamera | null = null;
let points: Points | null = null;
let observer: ResizeObserver | null = null;
let frameId = 0;
let invalidated = true;
let loadToken = 0;
let colorAttr: Float32BufferAttribute | null = null;
let center: [number, number, number] = [0, 0, 0];
let radius = 1e-3;
// The camera frames the cloud once; after that it belongs to the user, so a
// frame swap must not yank it back while the spray develops.
let hasView = false;

const shownRange = computed(() => props.range ?? cloud.value?.range ?? [0, 0]);

const disposePoints = () => {
  if (!points) return;
  scene?.remove(points);
  points.geometry.dispose();
  (points.material as PointsMaterial).dispose();
  points = null;
  colorAttr = null;
};

const resize = () => {
  const canvas = canvasEl.value;
  if (!renderer || !camera || !canvas) return;

  const { clientWidth, clientHeight } = canvas;
  if (clientWidth === 0 || clientHeight === 0) return;

  camera.aspect = clientWidth / clientHeight;
  camera.updateProjectionMatrix();
  renderer.setSize(clientWidth, clientHeight, false);
  invalidated = true;
};

const animate = () => {
  frameId = requestAnimationFrame(animate);
  if (!renderer || !scene || !camera || !controls) return;

  if (controls.update() || invalidated) {
    invalidated = false;
    renderer.render(scene, camera);
  }
};

const build = (data: h5.H5ParcelCloud) => {
  disposePoints();
  if (!scene || !camera || !controls) return;

  const geometry = new BufferGeometry();
  geometry.setAttribute("position", new Float32BufferAttribute(data.points, 3));

  const coloured = data.values !== undefined && data.values.length > 0;
  const material = new PointsMaterial({
    size: 2.5,
    sizeAttenuation: false,
    vertexColors: coloured,
    // three multiplies the material colour into the vertex colours, so a tint
    // here would pull every point off the ramp the legend is showing.
    color: coloured ? 0xffffff : 0x4b8bd6,
  });

  if (coloured && data.values) {
    const [lo, hi] = props.range ?? data.range;
    const colors = new Float32Array(data.values.length * 3);
    for (let i = 0; i < data.values.length; i++) {
      const v = data.values[i];
      const [r, g, b] = v === null ? NO_VALUE : rampAt(normalize(v, lo, hi));
      colors[i * 3] = r;
      colors[i * 3 + 1] = g;
      colors[i * 3 + 2] = b;
    }
    colorAttr = new Float32BufferAttribute(colors, 3);
    geometry.setAttribute("color", colorAttr);
  }

  points = new Points(geometry, material);
  scene.add(points);

  const [minX, minY, minZ, maxX, maxY, maxZ] = data.bounds;
  center = [(minX + maxX) / 2, (minY + maxY) / 2, (minZ + maxZ) / 2];
  const extent = Math.max(maxX - minX, maxY - minY, maxZ - minZ);
  // A single parcel, or a spray still bunched at the injector, spans nothing.
  // Framing that literally gives a far plane nearer than the near plane — an
  // invalid frustum, and a canvas that draws nothing at all — so fall back to
  // a scene a millimetre across, the scale these sprays start at.
  radius = (Number.isFinite(extent) && extent > 0 ? extent : 1e-3) / 2;

  if (!hasView) {
    resetView();
    hasView = true;
  } else {
    invalidated = true;
  }
};

// Frame the spray: centre on the cloud's bounding box and back the camera
// off by its diagonal.
const resetView = () => {
  if (!camera || !controls) return;
  const [cx, cy, cz] = center;
  controls.target.set(cx, cy, cz);
  camera.position.set(cx + radius * 2.2, cy + radius * 1.6, cz + radius * 2.2);
  camera.near = Math.max(radius / 1000, 1e-6);
  camera.far = radius * 100;
  camera.updateProjectionMatrix();
  controls.update();
  invalidated = true;
};

// A range change is a recolour of what is already on the GPU, never a refetch.
const recolor = () => {
  const data = cloud.value;
  if (!data?.values?.length || !colorAttr) return;
  const [lo, hi] = props.range ?? data.range;
  const rgb = colorAttr.array as Float32Array;
  for (let i = 0; i < data.values.length; i++) {
    const v = data.values[i];
    const [r, g, b] = v === null ? NO_VALUE : rampAt(normalize(v, lo, hi));
    rgb[i * 3] = r;
    rgb[i * 3 + 1] = g;
    rgb[i * 3 + 2] = b;
  }
  colorAttr.needsUpdate = true;
  invalidated = true;
};

const savePng = () => {
  if (!renderer || !scene || !camera) return;
  const base = props.path.split("/").pop() || "parcels";
  saveViewPng(renderer, scene, camera, pngFilename(base, props.scalar));
};

const load = async () => {
  if (!props.path) return;
  const token = ++loadToken;

  loading.value = true;
  error.value = "";

  try {
    // The cache owns the request, so a frame revisited while scrubbing — or
    // reloaded by the route sync on pause — never hits the network twice.
    const data = await fetchParcels(props.path, {
      group: props.group,
      scalar: props.scalar,
    });
    if (token !== loadToken) return;
    cloud.value = data;
    if (data.sent === 0) {
      disposePoints();
      error.value = t("h5View.noParcels");
      emit("loaded", [0, 0]);
      return;
    }
    build(data);
    emit("loaded", [data.range[0] ?? 0, data.range[1] ?? 0]);
  } catch (e: any) {
    if (token !== loadToken || e?.name === "AbortError") return;
    if (e?.status === 404) {
      // The group is not in this frame at all — an output written before
      // injection starts. Empty is the honest picture, and the sequence
      // keeps moving so the spray can be watched appearing.
      disposePoints();
      cloud.value = null;
      error.value = t("h5View.noParcels");
      emit("loaded", [0, 0]);
      return;
    }
    error.value = e?.message ?? String(e);
    emit("loaded", null);
  } finally {
    if (token === loadToken) loading.value = false;
  }
};

onMounted(() => {
  const canvas = canvasEl.value;
  if (!canvas) return;

  renderer = new WebGLRenderer({ canvas, antialias: true, alpha: true });
  renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));

  scene = new Scene();
  scene.background = null;

  camera = new PerspectiveCamera(50, 1, 0.01, 1000);
  controls = new OrbitControls(camera, canvas);
  controls.enableDamping = true;
  controls.addEventListener("change", () => {
    invalidated = true;
  });

  observer = new ResizeObserver(resize);
  observer.observe(canvas);
  resize();
  animate();

  load();
});

watch(
  () => [props.path, props.group, props.scalar],
  () => load()
);

watch(() => props.range, recolor);

onBeforeUnmount(() => {
  loadToken++;
  cancelAnimationFrame(frameId);
  observer?.disconnect();
  disposePoints();
  controls?.dispose();
  renderer?.dispose();
  renderer?.forceContextLoss();
  renderer = null;
  scene = null;
  camera = null;
  controls = null;
});

// Colours are computed from the ramp, so expose it for the legend gradient.
defineExpose({ reload: load });
</script>
