<template>
  <div class="relative w-full h-full">
    <canvas ref="canvasEl" class="w-full h-full block"></canvas>

    <div
      v-if="loading"
      class="absolute inset-0 flex items-center justify-center text-gray-500 dark:text-gray-400"
    >
      <i class="fa-solid fa-spinner fa-spin text-2xl"></i>
    </div>

    <div
      v-else-if="error"
      class="absolute inset-0 flex items-center justify-center p-4 text-center text-sm text-gray-600 dark:text-gray-300"
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
          >{{ formatValue(cloud.range[0]) }}–{{
            formatValue(cloud.range[1])
          }}</span
        >
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from "vue";
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

// A parcel cloud needs no mesh or connectivity, so the browser cost is just
// the point count. Beyond this the server strides the cloud down rather than
// sending everything.
const PARCEL_LIMIT = 200000;

const props = defineProps<{
  path: string;
  group: string;
  scalar?: string;
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
let controller: AbortController | null = null;

// Blue through red: a perceptually ordered ramp for a scalar with no inherent
// zero, which is what parcel temperature and radius are.
const RAMP: [number, number, number][] = [
  [0.19, 0.31, 0.75],
  [0.27, 0.65, 0.87],
  [0.55, 0.83, 0.62],
  [0.95, 0.79, 0.32],
  [0.84, 0.28, 0.22],
];

const rampCss = RAMP.map(
  ([r, g, b]) => `rgb(${r * 255} ${g * 255} ${b * 255})`
).join(", ");

const rampAt = (t: number): [number, number, number] => {
  const clamped = Math.min(1, Math.max(0, t));
  const scaled = clamped * (RAMP.length - 1);
  const i = Math.min(RAMP.length - 2, Math.floor(scaled));
  const f = scaled - i;
  const a = RAMP[i];
  const b = RAMP[i + 1];
  return [
    a[0] + (b[0] - a[0]) * f,
    a[1] + (b[1] - a[1]) * f,
    a[2] + (b[2] - a[2]) * f,
  ];
};

const disposePoints = () => {
  if (!points) return;
  scene?.remove(points);
  points.geometry.dispose();
  (points.material as PointsMaterial).dispose();
  points = null;
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

  const material = new PointsMaterial({
    size: 2.5,
    sizeAttenuation: false,
    vertexColors: data.values !== undefined && data.values.length > 0,
    color: 0x4b8bd6,
  });

  if (material.vertexColors && data.values) {
    const [lo, hi] = data.range;
    const span = hi - lo;
    const colors = new Float32Array(data.values.length * 3);
    for (let i = 0; i < data.values.length; i++) {
      const [r, g, b] = rampAt(span === 0 ? 0.5 : (data.values[i] - lo) / span);
      colors[i * 3] = r;
      colors[i * 3 + 1] = g;
      colors[i * 3 + 2] = b;
    }
    geometry.setAttribute("color", new Float32BufferAttribute(colors, 3));
  }

  points = new Points(geometry, material);
  scene.add(points);

  // Frame the spray: centre on the cloud's bounding box and back the camera
  // off by its diagonal.
  const [minX, minY, minZ, maxX, maxY, maxZ] = data.bounds;
  const cx = (minX + maxX) / 2;
  const cy = (minY + maxY) / 2;
  const cz = (minZ + maxZ) / 2;
  const radius =
    Math.max(maxX - minX, maxY - minY, maxZ - minZ, Number.EPSILON) / 2;

  controls.target.set(cx, cy, cz);
  camera.position.set(cx + radius * 2.2, cy + radius * 1.6, cz + radius * 2.2);
  camera.near = Math.max(radius / 1000, 1e-6);
  camera.far = radius * 100;
  camera.updateProjectionMatrix();
  controls.update();
  invalidated = true;
};

const load = async () => {
  const token = ++loadToken;
  controller?.abort();
  controller = new AbortController();

  loading.value = true;
  error.value = "";

  try {
    const data = await h5.parcels(
      props.path,
      props.group,
      { scalar: props.scalar, limit: PARCEL_LIMIT },
      controller.signal
    );
    if (token !== loadToken) return;
    cloud.value = data;
    if (data.sent === 0) {
      error.value = t("h5View.noParcels");
      return;
    }
    build(data);
  } catch (e: any) {
    if (token !== loadToken || e?.name === "AbortError") return;
    error.value = e?.message ?? String(e);
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

onBeforeUnmount(() => {
  controller?.abort();
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
