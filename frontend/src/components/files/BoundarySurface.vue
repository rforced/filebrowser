<template>
  <div class="relative w-full h-full" @wheel.stop @touchmove.stop>
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

    <template v-else-if="surface">
      <div
        class="absolute left-2 bottom-2 flex flex-col gap-1 rounded-md bg-white/85 dark:bg-gray-900/85 px-2.5 py-1.5 text-xs text-gray-700 dark:text-gray-200 backdrop-blur-sm"
      >
        <span>
          {{
            t("h5View.surfaceCounts", {
              faces: formatCount(surface.faces),
              triangles: formatCount(surface.triangles),
            })
          }}
        </span>

        <span
          v-if="surface.truncated"
          class="text-amber-700 dark:text-amber-400"
        >
          <i class="fa-solid fa-triangle-exclamation mr-1"></i>
          {{
            t("h5View.surfaceStrided", {
              n: surface.stride,
              total: formatCount(surface.facesTotal),
            })
          }}
        </span>

        <div
          v-if="surface.scalar && surface.values"
          class="flex items-center gap-1.5"
        >
          <span class="shrink-0">{{ surface.scalar }}</span>
          <span
            class="h-2 w-20 rounded-full"
            :style="{ background: `linear-gradient(to right, ${rampCss})` }"
          ></span>
          <span class="tabular-nums">
            {{ formatValue(surface.range[0]) }}–{{
              formatValue(surface.range[1])
            }}
          </span>
        </div>
      </div>

      <div class="absolute right-2 bottom-2 flex gap-2">
        <button
          type="button"
          class="surface-button"
          :aria-label="t('buttons.resetView')"
          :title="t('buttons.resetView')"
          @click="resetView"
        >
          <i class="fa-solid fa-crosshairs"></i>
        </button>
        <button
          type="button"
          class="surface-button"
          :class="{ 'surface-button--active': wireframe }"
          :aria-label="t('buttons.wireframe')"
          :title="t('buttons.wireframe')"
          @click="toggleWireframe"
        >
          <i class="fa-solid fa-border-all"></i>
        </button>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import {
  BufferAttribute,
  BufferGeometry,
  Color,
  DirectionalLight,
  DoubleSide,
  Float32BufferAttribute,
  Group,
  HemisphereLight,
  Mesh,
  MeshStandardMaterial,
  PerspectiveCamera,
  Scene,
  WebGLRenderer,
} from "three";
import { OrbitControls } from "three/addons/controls/OrbitControls.js";

import * as h5 from "@/api/h5";
import { formatCount, formatValue } from "@/utils/convergeH5";
// A NaN value normalizes to NaN, which rampAt already draws as NO_VALUE grey.
import { normalize, rampAt, rampCss } from "@/utils/colorRamp";
import {
  boundaryColor,
  boundaryColorCss,
  type SurfaceBoundaryInfo,
} from "@/utils/convergeSurface";

// What one response may carry. The largest post file measured yields 787k
// triangles; past this the server strides the surface down and says so, which
// the legend then reports rather than presenting a partial wall as the whole.
const TRIANGLE_LIMIT = 2000000;

const props = defineProps<{
  path: string;
  stream: string;
  scalar?: string;
}>();

const emit = defineEmits<{
  boundaries: [boundaries: SurfaceBoundaryInfo[]];
}>();

const { t } = useI18n({});

const canvasEl = ref<HTMLCanvasElement | null>(null);
const loading = ref(true);
const error = ref("");
const wireframe = ref(false);
const surface = ref<h5.H5Surface | null>(null);

let renderer: WebGLRenderer | null = null;
let controls: OrbitControls | null = null;
let scene: Scene | null = null;
let camera: PerspectiveCamera | null = null;
let model: Group | null = null;
let observer: ResizeObserver | null = null;
let frameId = 0;
let invalidated = true;
let loadToken = 0;
let controller: AbortController | null = null;
let radius = 1;
let center: [number, number, number] = [0, 0, 0];

const disposeModel = () => {
  if (!model) return;
  for (const child of model.children) {
    const mesh = child as Mesh;
    mesh.geometry.dispose();
    (mesh.material as MeshStandardMaterial).dispose();
  }
  scene?.remove(model);
  model = null;
};

const build = (data: h5.H5Surface) => {
  disposeModel();
  if (!scene) return;

  // One vertex buffer for the whole surface, shared by every boundary mesh:
  // three uploads an attribute once per object identity, so the boundaries
  // cost one buffer between them and only their index arrays are separate.
  const positions = new Float32BufferAttribute(data.positions, 3);

  let colors: Float32BufferAttribute | null = null;
  if (data.values) {
    const [lo, hi] = data.range;
    const rgb = new Float32Array(data.values.length * 3);
    for (let i = 0; i < data.values.length; i++) {
      const [r, g, b] = rampAt(normalize(data.values[i], lo, hi));
      rgb[i * 3] = r;
      rgb[i * 3 + 1] = g;
      rgb[i * 3 + 2] = b;
    }
    colors = new Float32BufferAttribute(rgb, 3);
  }

  const group = new Group();
  const legend: SurfaceBoundaryInfo[] = [];

  data.boundaries.forEach((boundary, slot) => {
    const geometry = new BufferGeometry();
    geometry.setAttribute("position", positions);
    if (colors) geometry.setAttribute("color", colors);
    geometry.setIndex(
      new BufferAttribute(
        data.indices.subarray(
          boundary.indexOffset,
          boundary.indexOffset + boundary.indexCount
        ),
        1
      )
    );

    const { h, s, l } = boundaryColor(slot);
    const mesh = new Mesh(
      geometry,
      new MeshStandardMaterial({
        // Cut-cell faces are the geometry here, so they are shaded as facets
        // rather than smoothed across. It also sidesteps vertex normals
        // entirely: the faces of a boundary do not share a winding
        // convention, and averaging opposed normals at a shared corner would
        // cancel them into black patches.
        flatShading: true,
        side: DoubleSide,
        vertexColors: colors !== null,
        color: colors ? 0xffffff : new Color().setHSL(h / 360, s, l),
        metalness: 0.1,
        roughness: 0.75,
        wireframe: wireframe.value,
      })
    );
    mesh.userData.boundaryId = boundary.id;
    group.add(mesh);

    legend.push({
      id: boundary.id,
      name: boundary.name,
      triangleCount: boundary.triangles,
      faceCount: boundary.faces,
      color: boundaryColorCss(slot),
    });
  });

  model = group;
  scene.add(group);
  emit("boundaries", legend);

  const [minX, minY, minZ, maxX, maxY, maxZ] = data.bounds;
  center = [(minX + maxX) / 2, (minY + maxY) / 2, (minZ + maxZ) / 2];
  const extent = Math.max(maxX - minX, maxY - minY, maxZ - minZ);
  radius = (Number.isFinite(extent) && extent > 0 ? extent : 1e-3) / 2;
  resetView();
};

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

const toggleWireframe = () => {
  wireframe.value = !wireframe.value;
  model?.traverse((child) => {
    const material = (child as Mesh).material as MeshStandardMaterial;
    if (material && "wireframe" in material) {
      material.wireframe = wireframe.value;
    }
  });
  invalidated = true;
};

const setBoundaryVisible = (id: number, visible: boolean) => {
  model?.traverse((child) => {
    if (child.userData.boundaryId === id) child.visible = visible;
  });
  invalidated = true;
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

const load = async () => {
  const token = ++loadToken;
  controller?.abort();
  controller = new AbortController();

  loading.value = true;
  error.value = "";

  try {
    const data = await h5.surface(
      props.path,
      {
        stream: props.stream,
        scalar: props.scalar,
        limit: TRIANGLE_LIMIT,
      },
      controller.signal
    );
    if (token !== loadToken) return;

    surface.value = data;
    if (data.triangles === 0) {
      error.value = t("h5View.noBoundaryFaces");
      return;
    }
    build(data);
    resize();
  } catch (e: any) {
    if (token !== loadToken || e?.name === "AbortError" || e?.is_canceled) {
      return;
    }
    error.value = e?.message ?? String(e);
  } finally {
    if (token === loadToken) loading.value = false;
  }
};

defineExpose({ setBoundaryVisible, resetView });

onMounted(() => {
  const canvas = canvasEl.value;
  if (!canvas) return;

  try {
    renderer = new WebGLRenderer({ canvas, antialias: true, alpha: true });
  } catch {
    error.value = t("files.modelNoWebgl");
    loading.value = false;
    return;
  }
  renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));

  scene = new Scene();
  camera = new PerspectiveCamera(50, 1, 0.01, 1000);

  scene.add(new HemisphereLight(0xffffff, 0x202028, 1.5));
  // Parented to the camera so the wall stays lit from wherever it is viewed.
  const keyLight = new DirectionalLight(0xffffff, 2.5);
  keyLight.position.set(1, 2, 3);
  camera.add(keyLight);
  scene.add(camera);

  controls = new OrbitControls(camera, canvas);
  controls.enableDamping = true;
  controls.dampingFactor = 0.1;
  controls.addEventListener("change", () => {
    invalidated = true;
  });

  observer = new ResizeObserver(resize);
  observer.observe(canvas);
  resize();
  animate();

  load();
});

// The scalar is resolved server-side against the adjacent cell of each face,
// so changing it is a refetch rather than a recolour.
watch(() => [props.path, props.stream, props.scalar], load);

onBeforeUnmount(() => {
  loadToken++;
  controller?.abort();
  cancelAnimationFrame(frameId);
  observer?.disconnect();
  observer = null;

  disposeModel();
  controls?.dispose();
  controls = null;

  renderer?.dispose();
  // Frees the GPU context immediately; browsers cap how many stay alive.
  renderer?.forceContextLoss();
  renderer = null;

  scene = null;
  camera = null;
});
</script>

<style scoped>
.surface-button {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 2.25em;
  height: 2.25em;
  border: 0;
  border-radius: 50%;
  cursor: pointer;
  color: #fff;
  background-color: rgba(255, 255, 255, 0.2);
}

.surface-button:hover {
  background-color: rgba(255, 255, 255, 0.35);
}

.surface-button--active {
  background-color: var(--blue, rgba(255, 255, 255, 0.5));
}
</style>
