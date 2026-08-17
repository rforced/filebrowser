<template>
  <div class="model-viewer" @wheel.stop @touchmove.stop>
    <!-- Kept mounted and visible so the canvas is laid out before the first
         render; it stays transparent until a model is added to the scene. -->
    <canvas ref="canvasEl" class="model-canvas" />

    <div v-if="loading" class="model-status">
      <div class="spinner">
        <div class="bounce1"></div>
        <div class="bounce2"></div>
        <div class="bounce3"></div>
      </div>
    </div>

    <div v-else-if="error" class="model-status model-error">
      <i class="fa-solid fa-circle-exclamation"></i>
      <span>{{ error }}</span>
    </div>

    <div v-else class="model-actions">
      <button
        class="model-button"
        :aria-label="t('buttons.resetView')"
        :title="t('buttons.resetView')"
        @click="resetView"
      >
        <i class="fa-solid fa-crosshairs"></i>
      </button>
      <button
        class="model-button"
        :class="{ 'model-button--active': wireframe }"
        :aria-label="t('buttons.wireframe')"
        :title="t('buttons.wireframe')"
        @click="toggleWireframe"
      >
        <i class="fa-solid fa-border-all"></i>
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import {
  Box3,
  BufferAttribute,
  BufferGeometry,
  Color,
  DirectionalLight,
  DoubleSide,
  Group,
  HemisphereLight,
  LoadingManager,
  Mesh,
  MeshStandardMaterial,
  PerspectiveCamera,
  Points,
  PointsMaterial,
  Scene,
  Sphere,
  WebGLRenderer,
  type ColorRepresentation,
  type Material,
  type Object3D,
  type Texture,
} from "three";
import { OrbitControls } from "three/addons/controls/OrbitControls.js";
import {
  boundaryColor,
  boundaryColorCss,
  parseSurfaceDat,
  type SurfaceBoundaryInfo,
} from "@/utils/convergeSurface";

// Models are parsed entirely in the browser, so refuse anything large enough to
// risk exhausting memory rather than hanging the tab.
const MODEL_MAX_SIZE = 100 * 1024 * 1024;

interface Props {
  src: string;
  extension: string;
  size: number;
  // Already-fetched file text (surfaces ride along on the resource response);
  // saves refetching the raw file.
  content?: string;
}

const props = defineProps<Props>();

const emit = defineEmits<{
  boundaries: [boundaries: SurfaceBoundaryInfo[]];
  failed: [];
}>();

const { t } = useI18n({});

const canvasEl = ref<HTMLCanvasElement | null>(null);
const loading = ref(true);
const error = ref("");
const wireframe = ref(false);

let renderer: WebGLRenderer | null = null;
let controls: OrbitControls | null = null;
let scene: Scene | null = null;
let camera: PerspectiveCamera | null = null;
let model: Object3D | null = null;
let observer: ResizeObserver | null = null;
let frameId = 0;
let modelRadius = 1;
// Renders are skipped while nothing moves; set whenever the scene changes.
let invalidated = true;
// Guards against a slow load for a previous file overwriting a newer one.
let loadToken = 0;

const createMaterial = (
  vertexColors: boolean,
  color: ColorRepresentation = 0xb9bec5
) =>
  new MeshStandardMaterial({
    color: vertexColors ? 0xffffff : color,
    vertexColors,
    metalness: 0.1,
    roughness: 0.75,
    side: DoubleSide,
    wireframe: wireframe.value,
  });

const meshFromGeometry = (geometry: BufferGeometry) => {
  if (!geometry.hasAttribute("normal")) {
    geometry.computeVertexNormals();
  }
  return new Mesh(geometry, createMaterial(geometry.hasAttribute("color")));
};

const loadObject = async (
  url: string,
  extension: string
): Promise<Object3D> => {
  // Sibling resources (.bin buffers, textures) are resolved relative to the
  // model URL, which drops the auth query string the raw endpoint needs.
  const queryStart = props.src.indexOf("?");
  const query = queryStart === -1 ? "" : props.src.slice(queryStart);
  const manager = new LoadingManager();
  manager.setURLModifier((resource) =>
    query === "" ||
    resource === url ||
    resource.includes("?") ||
    resource.startsWith("data:") ||
    resource.startsWith("blob:")
      ? resource
      : resource + query
  );

  switch (extension) {
    case ".dat": {
      let text = props.content;
      if (text === undefined) {
        const res = await fetch(url);
        if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
        text = await res.text();
      }
      const surface = parseSurfaceDat(text);

      const group = new Group();
      group.userData.surfaceBoundaries = surface.boundaries.map(
        (boundary, slot) => {
          const geometry = new BufferGeometry();
          geometry.setAttribute(
            "position",
            new BufferAttribute(boundary.positions, 3)
          );
          geometry.computeVertexNormals();

          const { h, s, l } = boundaryColor(slot);
          const child = new Mesh(
            geometry,
            createMaterial(false, new Color().setHSL(h / 360, s, l))
          );
          child.userData.boundaryId = boundary.id;
          group.add(child);

          return {
            id: boundary.id,
            triangleCount: boundary.triangleCount,
            color: boundaryColorCss(slot),
          } satisfies SurfaceBoundaryInfo;
        }
      );
      return group;
    }
    case ".stl": {
      const { STLLoader } = await import("three/addons/loaders/STLLoader.js");
      return meshFromGeometry(await new STLLoader(manager).loadAsync(url));
    }
    case ".ply": {
      const { PLYLoader } = await import("three/addons/loaders/PLYLoader.js");
      const geometry = await new PLYLoader(manager).loadAsync(url);
      // PLYLoader only indexes the geometry when the file declares faces;
      // without them the file is a point cloud and has nothing to shade.
      if (geometry.index === null) {
        return new Points(
          geometry,
          new PointsMaterial({
            vertexColors: geometry.hasAttribute("color"),
            color: geometry.hasAttribute("color") ? 0xffffff : 0xb9bec5,
          })
        );
      }
      return meshFromGeometry(geometry);
    }
    case ".obj": {
      const { OBJLoader } = await import("three/addons/loaders/OBJLoader.js");
      return await new OBJLoader(manager).loadAsync(url);
    }
    case ".3mf": {
      const { ThreeMFLoader } =
        await import("three/addons/loaders/3MFLoader.js");
      return await new ThreeMFLoader(manager).loadAsync(url);
    }
    case ".glb":
    case ".gltf": {
      const { GLTFLoader } = await import("three/addons/loaders/GLTFLoader.js");
      const gltf = await new GLTFLoader(manager).loadAsync(url);
      return gltf.scene;
    }
    default:
      throw new Error(`unsupported model extension: ${extension}`);
  }
};

const resetView = () => {
  if (!camera || !controls) {
    return;
  }

  const fov = (camera.fov * Math.PI) / 180;
  const distance = (modelRadius / Math.sin(fov / 2)) * 1.2;

  camera.near = distance / 100;
  camera.far = distance * 100;
  camera.position.set(0.6, 0.5, 1).setLength(distance);
  camera.updateProjectionMatrix();

  controls.target.set(0, 0, 0);
  controls.update();
  invalidated = true;
};

const toggleWireframe = () => {
  wireframe.value = !wireframe.value;

  model?.traverse((child) => {
    const materials = (child as Mesh).material;
    if (!materials) {
      return;
    }
    for (const material of Array.isArray(materials) ? materials : [materials]) {
      if ("wireframe" in material) {
        material.wireframe = wireframe.value;
      }
    }
  });

  invalidated = true;
};

const disposeMaterial = (materials: Material | Material[]) => {
  for (const material of Array.isArray(materials) ? materials : [materials]) {
    for (const value of Object.values(material)) {
      if (value && (value as Texture).isTexture) {
        (value as Texture).dispose();
      }
    }
    material.dispose();
  }
};

const disposeModel = () => {
  if (!model) {
    return;
  }

  model.traverse((child) => {
    const mesh = child as Mesh;
    mesh.geometry?.dispose();
    if (mesh.material) {
      disposeMaterial(mesh.material);
    }
  });

  scene?.remove(model);
  model = null;
};

const resize = () => {
  const canvas = canvasEl.value;
  if (!renderer || !camera || !canvas) {
    return;
  }

  const { clientWidth, clientHeight } = canvas;
  if (clientWidth === 0 || clientHeight === 0) {
    return;
  }

  // `false` keeps three from writing inline styles over our CSS sizing.
  renderer.setSize(clientWidth, clientHeight, false);
  camera.aspect = clientWidth / clientHeight;
  camera.updateProjectionMatrix();
  invalidated = true;
};

const animate = () => {
  frameId = requestAnimationFrame(animate);

  if (!renderer || !scene || !camera || !controls) {
    return;
  }

  // update() reports whether damping moved the camera this frame.
  if (controls.update() || invalidated) {
    renderer.render(scene, camera);
    invalidated = false;
  }
};

const initScene = () => {
  const canvas = canvasEl.value;
  if (!canvas) {
    return false;
  }

  try {
    renderer = new WebGLRenderer({ canvas, antialias: true, alpha: true });
  } catch {
    error.value = t("files.modelNoWebgl");
    return false;
  }

  renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));

  scene = new Scene();
  camera = new PerspectiveCamera(50, 1, 0.1, 1000);

  scene.add(new HemisphereLight(0xffffff, 0x202028, 1.5));

  // Parented to the camera so the model stays lit from wherever it is viewed.
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

  return true;
};

const load = async () => {
  const token = ++loadToken;

  loading.value = true;
  error.value = "";
  disposeModel();

  if (props.size > MODEL_MAX_SIZE) {
    error.value = t("files.modelTooLarge");
    loading.value = false;
    return;
  }

  try {
    const object = await loadObject(props.src, props.extension.toLowerCase());

    // A newer file was opened, or the component went away, while we loaded.
    if (token !== loadToken || !scene) {
      return;
    }

    const box = new Box3().setFromObject(object);
    if (box.isEmpty()) {
      throw new Error("model contains no geometry");
    }

    const sphere = box.getBoundingSphere(new Sphere());
    modelRadius = Math.max(sphere.radius, Number.EPSILON);

    // Recenter on the origin so orbiting pivots around the model itself.
    object.position.sub(sphere.center);

    if (object instanceof Points) {
      (object.material as PointsMaterial).size = modelRadius / 250;
    }

    model = object;
    scene.add(object);

    emit(
      "boundaries",
      (object.userData.surfaceBoundaries as SurfaceBoundaryInfo[]) ?? []
    );

    resetView();
    resize();
    loading.value = false;
  } catch (e: any) {
    if (token !== loadToken) {
      return;
    }
    console.error("Failed to load 3D model:", e);
    error.value = t("files.modelLoadFailed");
    loading.value = false;
    emit("failed");
  }
};

const setBoundaryVisible = (id: number, visible: boolean) => {
  model?.traverse((child) => {
    if (child.userData.boundaryId === id) {
      child.visible = visible;
    }
  });
  invalidated = true;
};

defineExpose({ setBoundaryVisible });

onMounted(() => {
  if (!initScene()) {
    loading.value = false;
    return;
  }

  observer = new ResizeObserver(resize);
  observer.observe(canvasEl.value!);

  animate();
  load();
});

watch(() => props.src, load);

onBeforeUnmount(() => {
  // Stops any in-flight load from touching a torn-down scene.
  loadToken++;

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
.model-viewer {
  position: relative;
  width: 100%;
  height: 100%;
}

.model-canvas {
  display: block;
  width: 100%;
  height: 100%;
  touch-action: none;
  outline: none;
}

.model-status {
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  color: #fff;
}

.model-error {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 0.5em;
  font-size: 1.5em;
  text-align: center;
  padding: 0 1em;
}

.model-actions {
  position: absolute;
  right: 1em;
  bottom: 1em;
  display: flex;
  gap: 0.5em;
}

.model-button {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 2.5em;
  height: 2.5em;
  border: 0;
  border-radius: 50%;
  cursor: pointer;
  color: #fff;
  background-color: rgba(255, 255, 255, 0.2);
}

.model-button:hover {
  background-color: rgba(255, 255, 255, 0.35);
}

.model-button--active {
  background-color: var(--blue, rgba(255, 255, 255, 0.5));
}
</style>
