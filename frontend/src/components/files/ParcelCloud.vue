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
import { NO_VALUE, normalize, rampAt, rampCss } from "@/utils/colorRamp";

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
    const [lo, hi] = data.range;
    const colors = new Float32Array(data.values.length * 3);
    for (let i = 0; i < data.values.length; i++) {
      const v = data.values[i];
      const [r, g, b] = v === null ? NO_VALUE : rampAt(normalize(v, lo, hi));
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
  const extent = Math.max(maxX - minX, maxY - minY, maxZ - minZ);
  // A single parcel, or a spray still bunched at the injector, spans nothing.
  // Framing that literally gives a far plane nearer than the near plane — an
  // invalid frustum, and a canvas that draws nothing at all — so fall back to
  // a scene a millimetre across, the scale these sprays start at.
  const radius = (Number.isFinite(extent) && extent > 0 ? extent : 1e-3) / 2;

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
