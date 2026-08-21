<template>
  <div class="relative w-full h-full" @wheel.stop @touchmove.stop>
    <canvas ref="canvasEl" class="w-full h-full block"></canvas>

    <!-- Light-on-dark in both themes: the stage behind the canvas is dark.
         During playback the previous frame stays up while the next loads, so
         the spinner only shows before there is anything to look at. -->
    <div
      v-if="loading && !surface"
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

        <!-- A wall drawn entirely in the ramp's no-value grey reads as a broken
             viewer, so the one case that produces it says so instead. -->
        <span v-if="noReadings" class="text-amber-700 dark:text-amber-400">
          <i class="fa-solid fa-triangle-exclamation mr-1"></i>
          {{ t("h5View.surfaceNoValues", { scalar: surface.scalar }) }}
        </span>

        <span v-if="edgesHidden" class="text-gray-500 dark:text-gray-400">
          <i class="fa-solid fa-circle-info mr-1"></i>
          {{ t("h5View.surfaceEdgesHidden") }}
        </span>

        <div
          v-if="!noReadings && surface.scalar && surface.values"
          class="flex items-center gap-1.5"
        >
          <span class="shrink-0">{{ surface.scalar }}</span>
          <span
            class="h-2 w-20 rounded-full"
            :style="{ background: `linear-gradient(to right, ${rampCss})` }"
          ></span>
          <span class="tabular-nums">
            {{ formatValue(shownRange[0]) }}–{{ formatValue(shownRange[1]) }}
          </span>
        </div>
      </div>

      <!-- Styled like the legend chip opposite: the canvas is transparent
           over the page, so a fixed white-on-translucent-white treatment
           disappears entirely in the light theme. -->
      <div class="absolute right-2 bottom-2 flex gap-2">
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
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
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
  LineBasicMaterial,
  LineSegments,
  Mesh,
  MeshBasicMaterial,
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
import { fetchSurface, type SurfaceResolution } from "@/utils/surfaceCache";
import { pngFilename, saveViewPng } from "@/utils/viewCapture";

const props = defineProps<{
  path: string;
  stream: string;
  scalar?: string;
  representation?: "surface" | "edges" | "wireframe";
  resolution?: SurfaceResolution;
  // Pins the colour ramp to a fixed range — playback locks it so the colours
  // keep one meaning across frames instead of re-normalising every step.
  range?: [number, number] | null;
}>();

const emit = defineEmits<{
  boundaries: [boundaries: SurfaceBoundaryInfo[]];
  // The frame's own value range, or null when the load failed. Playback paces
  // itself on this signal.
  loaded: [range: [number, number] | null];
}>();

const { t } = useI18n({});

const canvasEl = ref<HTMLCanvasElement | null>(null);
const loading = ref(true);
const error = ref("");
const surface = ref<h5.H5Surface | null>(null);
const edgesHidden = ref(false);

const rep = () => props.representation ?? "surface";

let renderer: WebGLRenderer | null = null;
let controls: OrbitControls | null = null;
let scene: Scene | null = null;
let camera: PerspectiveCamera | null = null;
let model: Group | null = null;
let observer: ResizeObserver | null = null;
let frameId = 0;
let invalidated = true;
let loadToken = 0;
let radius = 1;
let center: [number, number, number] = [0, 0, 0];
let half: [number, number, number] = [0, 0, 0];
let colorAttr: Float32BufferAttribute | null = null;
let edgeLines: LineSegments[] = [];
let edgeSegments = 0;
let hasView = false;

const shownRange = computed(
  () => props.range ?? surface.value?.range ?? [0, 0]
);

// The field resolved at no vertex the surface kept — a diverged or unwritten
// variable, or one whose cells the strided face sample never reached. Every
// vertex then draws as NO_VALUE, which is the pale grey wall.
const noReadings = computed(() => {
  const data = surface.value;
  return (
    !!data?.scalar &&
    !!data.values &&
    data.vertices > 0 &&
    data.unresolved === data.vertices
  );
});

const edgeMaterial = new LineBasicMaterial({
  color: 0x0d0d0d,
  transparent: true,
});

const EDGE_FADE_MIN_PITCH = 0.9;
const EDGE_FADE_MAX_PITCH = 2.5;
const EDGE_MIN_OPACITY = 0.02;

const disposeModel = () => {
  if (!model) return;
  for (const child of model.children) {
    const mesh = child as Mesh;
    for (const lines of mesh.children) {
      (lines as LineSegments).geometry.dispose();
    }
    mesh.geometry.dispose();
    (mesh.userData.fill as MeshStandardMaterial).dispose();
    (mesh.userData.wire as MeshBasicMaterial).dispose();
  }
  scene?.remove(model);
  model = null;
  colorAttr = null;
  edgeLines = [];
  edgeSegments = 0;
  edgesHidden.value = false;
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
    const [lo, hi] = props.range ?? data.range;
    const rgb = new Float32Array(data.values.length * 3);
    for (let i = 0; i < data.values.length; i++) {
      const [r, g, b] = rampAt(normalize(data.values[i], lo, hi));
      rgb[i * 3] = r;
      rgb[i * 3 + 1] = g;
      rgb[i * 3 + 2] = b;
    }
    colors = new Float32BufferAttribute(rgb, 3);
  }
  colorAttr = colors;

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
    const color = colors ? 0xffffff : new Color().setHSL(h / 360, s, l);
    const fill = new MeshStandardMaterial({
      // Cut-cell faces are the geometry here, so they are shaded as facets
      // rather than smoothed across. It also sidesteps vertex normals
      // entirely: the faces of a boundary do not share a winding
      // convention, and averaging opposed normals at a shared corner would
      // cancel them into black patches.
      flatShading: true,
      side: DoubleSide,
      vertexColors: colors !== null,
      color,
      metalness: 0.1,
      roughness: 0.75,
      // The fill sits a hair behind its depth so the edge lines cannot
      // z-fight it.
      polygonOffset: true,
      polygonOffsetFactor: 1,
      polygonOffsetUnits: 1,
    });
    // Wireframe is a separate unlit material: with no normal attribute, flat
    // shading derives normals from triangle derivatives, which do not exist
    // on line primitives — a lit wireframe shades as garbage, black on most
    // GPUs.
    const wire = new MeshBasicMaterial({
      wireframe: true,
      vertexColors: colors !== null,
      color,
    });

    const mesh = new Mesh(geometry, rep() === "wireframe" ? wire : fill);
    mesh.userData.boundaryId = boundary.id;
    mesh.userData.fill = fill;
    mesh.userData.wire = wire;
    group.add(mesh);

    if (data.edgeIndices && boundary.edgeCount) {
      const outline = new BufferGeometry();
      outline.setAttribute("position", positions);
      outline.setIndex(
        new BufferAttribute(
          data.edgeIndices.subarray(
            boundary.edgeOffset ?? 0,
            (boundary.edgeOffset ?? 0) + boundary.edgeCount
          ),
          1
        )
      );
      const lines = new LineSegments(outline, edgeMaterial);
      mesh.add(lines);
      edgeLines.push(lines);
      edgeSegments += boundary.edgeCount / 2;
    }

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
  half = [(maxX - minX) / 2, (maxY - minY) / 2, (maxZ - minZ) / 2];
  const extent = Math.max(maxX - minX, maxY - minY, maxZ - minZ);
  radius = (Number.isFinite(extent) && extent > 0 ? extent : 1e-3) / 2;
  if (!hasView) {
    resetView();
    hasView = true;
  } else {
    invalidated = true;
  }
  applyEdgeFade();
};

const recolor = () => {
  const data = surface.value;
  if (!data?.values || !colorAttr) return;
  const [lo, hi] = props.range ?? data.range;
  const rgb = colorAttr.array as Float32Array;
  for (let i = 0; i < data.values.length; i++) {
    const [r, g, b] = rampAt(normalize(data.values[i], lo, hi));
    rgb[i * 3] = r;
    rgb[i * 3 + 1] = g;
    rgb[i * 3 + 2] = b;
  }
  colorAttr.needsUpdate = true;
  invalidated = true;
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

const savePng = () => {
  if (!renderer || !scene || !camera) return;
  const base = props.path.split("/").pop() || "surface";
  saveViewPng(renderer, scene, camera, pngFilename(base, props.scalar));
};

const facePitchPx = () => {
  if (!camera || !renderer || edgeSegments === 0) return Infinity;

  const height = renderer.domElement.height;
  const dx = camera.position.x - center[0];
  const dy = camera.position.y - center[1];
  const dz = camera.position.z - center[2];
  const distance = Math.sqrt(dx * dx + dy * dy + dz * dz);
  if (!(height > 0) || !(distance > 0)) return Infinity;

  const [hx, hy, hz] = half;
  const silhouette =
    4 *
    ((Math.abs(dx) * hy * hz +
      Math.abs(dy) * hx * hz +
      Math.abs(dz) * hx * hy) /
      distance);

  const scale =
    height / (2 * Math.tan((camera.fov * Math.PI) / 360) * distance);
  const pitch = Math.sqrt((4 * silhouette * scale * scale) / edgeSegments);
  return Number.isFinite(pitch) && pitch > 0 ? pitch : Infinity;
};

const applyEdgeFade = () => {
  const on = rep() === "edges" && edgeLines.length > 0;

  let opacity = 1;
  if (on) {
    const span = EDGE_FADE_MAX_PITCH - EDGE_FADE_MIN_PITCH;
    const t = (facePitchPx() - EDGE_FADE_MIN_PITCH) / span;
    opacity = Math.sqrt(Math.min(Math.max(t, 0), 1));
  }

  const visible = on && opacity > EDGE_MIN_OPACITY;
  edgeMaterial.opacity = opacity;
  for (const lines of edgeLines) lines.visible = visible;
  edgesHidden.value = on && !visible;
};

const applyRepresentation = () => {
  const mode = rep();
  model?.traverse((child) => {
    if ((child as LineSegments).isLineSegments) return;
    const mesh = child as Mesh;
    if (mesh.userData.fill) {
      mesh.material =
        mode === "wireframe" ? mesh.userData.wire : mesh.userData.fill;
    }
  });
  applyEdgeFade();
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
    applyEdgeFade();
    renderer.render(scene, camera);
  }
};

// A refusal now names its own ceiling, which is the difference between advice
// that works and advice that cannot. Only the drawn surface is a question of
// detail: a mesh too large to read is a fact about the file, and a scalar too
// large to invert is answered by dropping the colour-by. Sending every 413
// down the "try a lower step" path is what left an unreadable mesh looking
// like a setting the user had got wrong.
const refusalMessage = (e: any): string => {
  if (e?.status !== 413) return e?.message ?? String(e);
  if (e?.code === "meshTooLarge") return t("h5View.meshTooLarge");
  if (e?.code === "scalarTooLarge") return t("h5View.scalarTooLarge");
  return t(
    props.resolution === "low"
      ? "h5View.surfaceTooLarge"
      : "h5View.surfaceTooLargeForStep"
  );
};

const load = async () => {
  if (!props.path) return;
  const token = ++loadToken;

  loading.value = true;
  error.value = "";

  const withEdges = rep() === "edges";
  try {
    // The cache owns the request, so a frame revisited while scrubbing — or
    // reloaded by the route sync on pause — never hits the network twice.
    const data = await fetchSurface(props.path, {
      stream: props.stream,
      scalar: props.scalar,
      edges: withEdges,
      resolution: props.resolution,
    });
    if (token !== loadToken) return;

    surface.value = data;
    if (data.triangles === 0) {
      error.value = t("h5View.noBoundaryFaces");
      emit("loaded", null);
      return;
    }
    build(data);
    resize();
    emit("loaded", [data.range[0], data.range[1]]);

    // The representation moved to edges while this request was in flight
    // without them; go straight back for the same surface with its outline.
    if (!withEdges && rep() === "edges") {
      load();
    }
  } catch (e: any) {
    if (token !== loadToken || e?.name === "AbortError" || e?.is_canceled) {
      return;
    }
    error.value = refusalMessage(e);
    emit("loaded", null);
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
watch(() => [props.path, props.stream, props.scalar, props.resolution], load);

// The representation is local except the first time edges are asked of a
// surface fetched without them — the outline only travels on request.
watch(
  () => props.representation,
  () => {
    if (rep() === "edges" && surface.value && !surface.value.edgeIndices) {
      load();
      return;
    }
    applyRepresentation();
  }
);

watch(() => props.range, recolor);

onBeforeUnmount(() => {
  loadToken++;
  cancelAnimationFrame(frameId);
  observer?.disconnect();
  observer = null;

  disposeModel();
  edgeMaterial.dispose();
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
