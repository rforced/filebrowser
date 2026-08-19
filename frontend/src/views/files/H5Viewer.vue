<template>
  <div
    id="h5-viewer-container"
    class="fixed inset-0 z-9998 flex flex-col bg-gray-50 dark:bg-gray-900"
  >
    <header
      class="flex gap-3 items-center justify-between bg-gray-200 dark:bg-gray-900 border-b border-gray-300 dark:border-gray-700 p-3 md:px-6 shrink-0"
    >
      <div class="flex gap-2 items-center min-w-0">
        <button
          v-tooltip="t('buttons.close')"
          type="button"
          class="action shrink-0"
          :aria-label="t('buttons.close')"
          @click="close()"
        >
          <i class="fa-solid fa-xmark text-lg"></i>
        </button>

        <i class="fa-solid shrink-0" :class="kindIcon"></i>
        <span class="font-medium text-gray-900 dark:text-gray-100 truncate">
          {{ fileStore.req?.name ?? "" }}
        </span>
      </div>

      <div class="flex gap-2 items-center shrink-0">
        <a
          v-if="authStore.user?.perm.download"
          :href="downloadUrl"
          target="_blank"
          class="btn btn-flex btn-white btn-soft"
          :aria-label="t('buttons.download')"
        >
          <i class="fa-solid fa-download"></i>
          <span class="hidden md:inline">{{ t("buttons.download") }}</span>
        </a>
      </div>
    </header>

    <div v-if="loading" class="flex-1 flex items-center justify-center">
      <i class="fa-solid fa-spinner fa-spin text-2xl text-gray-400"></i>
    </div>

    <div
      v-else-if="error"
      class="flex-1 flex flex-col gap-3 items-center justify-center p-6 text-center"
    >
      <i class="fa-solid fa-triangle-exclamation text-3xl text-gray-400"></i>
      <p class="text-sm text-gray-600 dark:text-gray-300 max-w-lg">
        {{ error }}
      </p>
      <a
        v-if="authStore.user?.perm.download"
        :href="downloadUrl"
        target="_blank"
        class="btn btn-flex btn-blue btn-soft"
      >
        <i class="fa-solid fa-download"></i>
        <span>{{ t("buttons.download") }}</span>
      </a>
    </div>

    <div
      v-else-if="summary"
      class="flex-1 min-h-0 overflow-y-auto flex flex-col"
    >
      <!-- Header facts: what this file is, when it was written, how big. Nine
           of these stacked wrap into a wall on a narrow viewport, so below md
           the label sits beside its value and the whole strip collapses to a
           line or two. -->
      <div
        class="flex flex-wrap gap-x-4 md:gap-x-8 gap-y-1 md:gap-y-3 px-3 md:px-6 py-2 md:py-4 bg-white dark:bg-gray-800 border-b border-gray-200 dark:border-gray-700 shrink-0"
      >
        <div
          v-for="fact in facts"
          :key="fact.label"
          class="min-w-0 flex items-baseline gap-1.5 md:block"
        >
          <div
            class="text-[11px] md:text-xs uppercase tracking-wider text-gray-500 dark:text-gray-400 shrink-0"
          >
            {{ fact.label }}
          </div>
          <div
            class="text-sm font-medium text-gray-900 dark:text-gray-100 tabular-nums truncate"
            :title="fact.title ?? fact.value"
          >
            {{ fact.value }}
          </div>
        </div>
      </div>

      <div
        v-if="diverged.length"
        class="px-3 md:px-6 py-2.5 bg-red-50 dark:bg-red-950 border-b border-red-200 dark:border-red-900 text-sm text-red-800 dark:text-red-200 shrink-0"
      >
        <i class="fa-solid fa-triangle-exclamation mr-1.5"></i>
        {{
          t("h5View.diverged", {
            fields: diverged.map((d) => d.name).join(", "),
          })
        }}
      </div>

      <div
        class="flex gap-1 px-3 md:px-6 pt-2 md:pt-3 border-b border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-800 shrink-0"
      >
        <button
          v-for="tab in tabs"
          :key="tab.id"
          type="button"
          class="px-3 py-2 text-sm font-medium rounded-t-md transition border-b-2"
          :class="
            activeTab === tab.id
              ? 'border-blue-500 text-blue-600 dark:text-blue-300'
              : 'border-transparent text-gray-600 dark:text-gray-300 hover:text-gray-900 dark:hover:text-white'
          "
          @click="activeTab = tab.id"
        >
          <i class="fa-solid mr-1.5" :class="tab.icon"></i>{{ tab.label }}
        </button>
      </div>

      <!-- Variables: the manifest, with stats and subset download. -->
      <section v-show="activeTab === 'variables'" class="p-3 md:px-6 md:py-4">
        <div v-for="stream in summary.streams" :key="stream.name" class="mb-6">
          <div class="flex flex-wrap gap-3 items-center justify-between mb-2">
            <h3
              class="text-sm font-semibold text-gray-800 dark:text-gray-100 flex items-center gap-2"
            >
              {{ stream.name }}
              <span class="font-normal text-gray-500 dark:text-gray-400">
                {{ streamSubtitle(stream) }}
              </span>
            </h3>

            <div class="flex gap-2 items-center">
              <button
                type="button"
                class="btn btn-flex btn-white btn-soft btn-sm"
                :disabled="statsPending"
                @click="loadStats(stream)"
              >
                <i
                  class="fa-solid"
                  :class="statsPending ? 'fa-spinner fa-spin' : 'fa-calculator'"
                ></i>
                <span>{{ t("h5View.computeStats") }}</span>
              </button>

              <a
                v-if="
                  selected.size > 0 &&
                  selected.size <= MAX_SUBSET &&
                  authStore.user?.perm.download
                "
                :href="subsetHref"
                class="btn btn-flex btn-blue btn-soft btn-sm"
              >
                <i class="fa-solid fa-file-csv"></i>
                <span>{{
                  t("h5View.downloadSelected", { n: selected.size })
                }}</span>
              </a>
            </div>
          </div>

          <p
            v-if="statsError"
            class="mb-2 text-xs text-red-700 dark:text-red-400"
          >
            <i class="fa-solid fa-triangle-exclamation mr-1"></i>
            {{ t("h5View.statsFailed", { message: statsError }) }}
          </p>

          <p
            v-if="selected.size > MAX_SUBSET"
            class="mb-2 text-xs text-amber-700 dark:text-amber-400"
          >
            {{ t("h5View.subsetTooMany", { n: MAX_SUBSET }) }}
          </p>

          <p
            v-if="selected.size > 0"
            class="mb-2 text-xs text-gray-500 dark:text-gray-400"
          >
            {{
              t("h5View.subsetHint", {
                selected: formatBytes(selectedBytes),
                total: formatBytes(summary.size),
              })
            }}
          </p>

          <div class="overflow-x-auto">
            <table class="w-full text-sm">
              <thead>
                <tr
                  class="text-left text-xs uppercase tracking-wider text-gray-500 dark:text-gray-400 border-b border-gray-200 dark:border-gray-700"
                >
                  <th class="py-2 pr-2 w-8">
                    <input
                      type="checkbox"
                      :checked="allSelected(stream)"
                      :aria-label="t('h5View.selectAll')"
                      @change="toggleAll(stream)"
                    />
                  </th>
                  <th class="py-2 pr-4">{{ t("h5View.variable") }}</th>
                  <th class="py-2 pr-4">{{ t("h5View.type") }}</th>
                  <th class="py-2 pr-4 text-right">{{ t("h5View.count") }}</th>
                  <th class="py-2 pr-4 text-right">{{ t("h5View.size") }}</th>
                  <th class="py-2 pr-4 text-right">{{ t("h5View.min") }}</th>
                  <th class="py-2 pr-4 text-right">{{ t("h5View.mean") }}</th>
                  <th class="py-2 text-right">{{ t("h5View.max") }}</th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="v in stream.variables"
                  :key="v.path"
                  class="border-b border-gray-100 dark:border-gray-800 last:border-0 hover:bg-gray-50 dark:hover:bg-gray-800/60"
                >
                  <td class="py-1.5 pr-2">
                    <input
                      type="checkbox"
                      :checked="selected.has(v.path)"
                      :aria-label="v.name"
                      @change="toggleOne(v)"
                    />
                  </td>
                  <td
                    class="py-1.5 pr-4 font-medium text-gray-900 dark:text-gray-100"
                  >
                    {{ v.name }}
                    <i
                      v-if="statsFor(v)?.nan || statsFor(v)?.inf"
                      v-tooltip="t('h5View.nonFinite')"
                      class="fa-solid fa-triangle-exclamation ml-1 text-red-500"
                    ></i>
                  </td>
                  <td class="py-1.5 pr-4 text-gray-500 dark:text-gray-400">
                    {{ v.type }}
                  </td>
                  <td class="py-1.5 pr-4 text-right tabular-nums">
                    {{ formatCount(v.dims.reduce((a, b) => a * b, 1)) }}
                  </td>
                  <td
                    class="py-1.5 pr-4 text-right tabular-nums text-gray-500 dark:text-gray-400"
                  >
                    {{ formatBytes(v.bytes) }}
                  </td>
                  <template v-if="statsFor(v)">
                    <td class="py-1.5 pr-4 text-right tabular-nums">
                      {{ formatValue(statsFor(v)!.min) }}
                    </td>
                    <td class="py-1.5 pr-4 text-right tabular-nums">
                      {{ formatValue(statsFor(v)!.mean) }}
                    </td>
                    <td class="py-1.5 text-right tabular-nums">
                      {{ formatValue(statsFor(v)!.max) }}
                    </td>
                  </template>
                  <td
                    v-else
                    class="py-1.5 text-right text-gray-300"
                    colspan="3"
                  >
                    &mdash;
                  </td>
                </tr>
              </tbody>
            </table>
          </div>

          <p
            v-if="summary.truncated"
            class="mt-2 text-xs text-gray-500 dark:text-gray-400"
          >
            {{ t("h5View.truncated") }}
          </p>
        </div>
      </section>

      <!-- Surface: the wetted boundary, lifted out of the mesh itself. -->
      <section
        v-show="activeTab === 'surface'"
        class="flex flex-col flex-1 min-h-72"
      >
        <p
          v-if="!surfaceStream"
          class="flex-1 flex items-center justify-center p-6 text-center text-sm text-gray-500 dark:text-gray-400"
        >
          {{ t("h5View.noSurface") }}
        </p>

        <template v-else>
          <div
            class="flex flex-wrap gap-2 items-center px-3 md:px-6 py-2 border-b border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-800 shrink-0"
          >
            <label class="text-sm text-gray-600 dark:text-gray-300">
              {{ t("h5View.colourBy") }}
            </label>
            <select
              v-model="surfaceScalar"
              class="form-control py-1 text-sm w-auto"
              :aria-label="t('h5View.colourBy')"
            >
              <option value="">{{ t("h5View.byBoundary") }}</option>
              <option v-for="v in surfaceScalars" :key="v.path" :value="v.name">
                {{ v.name }}
              </option>
            </select>

            <label class="text-sm text-gray-600 dark:text-gray-300">
              {{ t("h5View.representation") }}
            </label>
            <select
              v-model="surfaceRepresentation"
              class="form-control py-1 text-sm w-auto"
              :aria-label="t('h5View.representation')"
            >
              <option value="surface">{{ t("h5View.repSurface") }}</option>
              <option value="edges">{{ t("h5View.repEdges") }}</option>
              <option value="wireframe">{{ t("h5View.repWireframe") }}</option>
            </select>

            <label class="text-sm text-gray-600 dark:text-gray-300">
              {{ t("h5View.resolution") }}
            </label>
            <select
              v-model="surfaceResolution"
              class="form-control py-1 text-sm w-auto"
              :aria-label="t('h5View.resolution')"
            >
              <option value="low" :disabled="stepDisabled('low')">
                {{ t("h5View.resLow") }}
              </option>
              <option value="medium" :disabled="stepDisabled('medium')">
                {{ t("h5View.resMedium") }}
              </option>
              <option value="high" :disabled="stepDisabled('high')">
                {{ t("h5View.resHigh") }}
              </option>
              <option value="ultra" :disabled="stepDisabled('ultra')">
                {{ t("h5View.resUltra") }}
              </option>
            </select>

            <div
              v-if="surfaceBoundaries.length"
              class="flex flex-wrap gap-1.5 items-center min-w-0 max-h-20 overflow-y-auto"
            >
              <button
                v-for="b in surfaceBoundaries"
                :key="b.id"
                v-tooltip="
                  t('h5View.surfaceChip', {
                    faces: formatCount(b.faceCount ?? 0),
                    triangles: formatCount(b.triangleCount),
                  })
                "
                type="button"
                class="px-2 py-0.5 rounded-full text-xs font-medium border transition"
                :class="
                  hiddenBoundaries.has(b.id)
                    ? 'border-gray-300 dark:border-gray-600 text-gray-400 dark:text-gray-500 hover:bg-gray-100 dark:hover:bg-gray-700 line-through'
                    : 'border-transparent text-white'
                "
                :style="
                  hiddenBoundaries.has(b.id) ? {} : { backgroundColor: b.color }
                "
                :aria-pressed="!hiddenBoundaries.has(b.id)"
                @click="toggleBoundary(b.id)"
              >
                {{ b.name || t("surfaceView.boundary", { id: b.id }) }}
              </button>
            </div>
          </div>

          <!-- Playback over the sibling post files of this run: the same
             transport as the Catalyst image player, but every frame is a
             fresh surface (AMR remeshes between outputs). -->
          <FramePlayer
            v-if="frames.length > 1"
            :playing="playing"
            :frame-index="frameIndex"
            :total="frames.length"
            :caption="frameCaption"
            :fps="fps"
            :can-rescale="!!(surfaceScalar && lockedRange)"
            :capped="playbackCapped"
            :capped-minutes="PLAYBACK_MAX_MINUTES"
            @toggle="togglePlay"
            @step="stepFrame"
            @scrub="onScrub"
            @update:fps="fps = $event"
            @rescale="rescaleRange"
          />

          <div class="flex-1 min-h-0 bg-gray-800 dark:bg-gray-950">
            <!-- Mounted on first use and kept: the surface is megabytes of
               geometry, so opening any post file must not fetch it. -->
            <BoundarySurface
              v-if="surfaceOpened && surfaceStream"
              ref="surfaceView"
              :path="surfaceShown"
              :stream="surfaceStream"
              :scalar="surfaceScalar || undefined"
              :representation="surfaceRepresentation"
              :resolution="surfaceResolution"
              :range="lockedRange"
              @boundaries="onSurfaceBoundaries"
              @loaded="onSurfaceLoaded"
            />
          </div>
        </template>
      </section>

      <!-- Parcels: a spray cloud needs no mesh, so it renders directly. -->
      <section
        v-show="activeTab === 'parcels'"
        class="flex flex-col flex-1 min-h-72"
      >
        <p
          v-if="!hasParcels"
          class="flex-1 flex items-center justify-center p-6 text-center text-sm text-gray-500 dark:text-gray-400"
        >
          {{ t("h5View.noParcels") }}
        </p>

        <template v-else>
          <div
            v-if="parcelGroups.length"
            class="flex flex-wrap gap-2 items-center px-3 md:px-6 py-2 border-b border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-800 shrink-0"
          >
            <select
              v-if="parcelGroups.length > 1"
              v-model="parcelGroup"
              class="form-control py-1 text-sm w-auto"
              :aria-label="t('h5View.parcelGroup')"
            >
              <option v-for="g in parcelGroups" :key="g.path" :value="g.path">
                {{ g.name }} ({{ formatCount(g.count) }})
              </option>
            </select>

            <label class="text-sm text-gray-600 dark:text-gray-300">
              {{ t("h5View.colourBy") }}
            </label>
            <select
              v-model="parcelScalar"
              class="form-control py-1 text-sm w-auto"
              :aria-label="t('h5View.colourBy')"
            >
              <option value="">{{ t("h5View.uniform") }}</option>
              <option v-for="name in parcelScalars" :key="name" :value="name">
                {{ name }}
              </option>
            </select>
          </div>

          <!-- The spray develops output by output, so the same transport plays
             the parcel cloud through the run. -->
          <FramePlayer
            v-if="frames.length > 1"
            :playing="playing"
            :frame-index="frameIndex"
            :total="frames.length"
            :caption="frameCaption"
            :fps="fps"
            :can-rescale="!!(parcelScalar && parcelLockedRange)"
            :capped="playbackCapped"
            :capped-minutes="PLAYBACK_MAX_MINUTES"
            @toggle="togglePlay"
            @step="stepFrame"
            @scrub="onScrub"
            @update:fps="fps = $event"
            @rescale="rescaleRange"
          />

          <div class="flex-1 min-h-0 bg-gray-800 dark:bg-gray-950">
            <ParcelCloud
              v-if="parcelGroup"
              :key="parcelGroup"
              :path="parcelShown"
              :group="parcelGroup"
              :scalar="parcelScalar || undefined"
              :range="parcelLockedRange"
              @loaded="onParcelLoaded"
            />
          </div>
        </template>
      </section>

      <!-- Boundaries: real names, straight out of the file. -->
      <section v-show="activeTab === 'boundaries'" class="p-3 md:px-6 md:py-4">
        <p
          v-if="!hasBoundaries"
          class="text-sm text-gray-500 dark:text-gray-400"
        >
          {{ t("h5View.noBoundaries") }}
        </p>

        <table v-else class="w-full text-sm max-w-2xl">
          <thead>
            <tr
              class="text-left text-xs uppercase tracking-wider text-gray-500 dark:text-gray-400 border-b border-gray-200 dark:border-gray-700"
            >
              <th class="py-2 pr-4 w-16">{{ t("h5View.id") }}</th>
              <th class="py-2 pr-4">{{ t("h5View.boundary") }}</th>
              <th class="py-2 text-right">{{ t("h5View.faces") }}</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="b in summary.boundaries"
              :key="b.id"
              class="border-b border-gray-100 dark:border-gray-800 last:border-0"
              :class="
                b.elements === 0 ? 'text-gray-400 dark:text-gray-500' : ''
              "
            >
              <td class="py-1.5 pr-4 tabular-nums">{{ b.id }}</td>
              <td class="py-1.5 pr-4">{{ b.name }}</td>
              <td class="py-1.5 text-right tabular-nums">
                {{ formatCount(b.elements) }}
              </td>
            </tr>
          </tbody>
        </table>
      </section>
    </div>
  </div>
</template>

<script setup lang="ts">
import {
  computed,
  onBeforeUnmount,
  onMounted,
  ref,
  watch,
  watchEffect,
} from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";

import * as api from "@/api";
import * as h5 from "@/api/h5";
import { useAuthStore } from "@/stores/auth";
import { useFileStore } from "@/stores/file";
import BoundarySurface from "@/components/files/BoundarySurface.vue";
import FramePlayer from "@/components/files/FramePlayer.vue";
import ParcelCloud from "@/components/files/ParcelCloud.vue";
import {
  divergenceOf,
  formatBytes,
  formatCount,
  formatSimTime,
  formatValue,
  outputProfile,
  type OutputPoint,
} from "@/utils/convergeH5";
import type { SurfaceBoundaryInfo } from "@/utils/convergeSurface";
import { clearParcelCache, prefetchParcels } from "@/utils/parcelCache";
import {
  clearSurfaceCache,
  DEFAULT_SURFACE_RESOLUTION,
  PLAYBACK_MAX_RESOLUTION,
  PLAYBACK_SURFACE_RESOLUTION,
  prefetchSurface,
  SURFACE_TRIANGLE_LIMITS,
  type SurfaceResolution,
} from "@/utils/surfaceCache";
import url from "@/utils/url";

const authStore = useAuthStore();
const fileStore = useFileStore();
const route = useRoute();
const router = useRouter();
const { t } = useI18n({});

const loading = ref(true);
const error = ref("");
const summary = ref<h5.H5Summary | null>(null);
const stats = ref<Map<string, h5.H5Stats>>(new Map());
const statsPending = ref(false);
// Kept apart from `error`: that one replaces the whole viewer, and a stats
// batch failing is no reason to take the file's manifest off the screen.
const statsError = ref("");
const selected = ref<Set<string>>(new Set());
const activeTab = ref("variables");
const parcelGroup = ref("");
const parcelScalar = ref("");
const surfaceScalar = ref("");
const surfaceRepresentation = ref<"surface" | "edges" | "wireframe">("surface");
const surfaceResolution = ref<SurfaceResolution>(DEFAULT_SURFACE_RESOLUTION);

const PLAYBACK_MAX_MINUTES = 2;
const PLAYBACK_MAX_MS = PLAYBACK_MAX_MINUTES * 60_000;

// Playback drops to the low step the first time it runs, because the wire and
// not the GPU sets the frame rate. It does that by moving the control itself,
// so the menu says what is being drawn — and once the choice is the user's,
// playback never takes it back.
let resolutionChosen = false;

// The steps past the playback ceiling are the ones a sequence cannot afford,
// whoever picked them. They grey out while the player runs rather than
// disappearing, so the menu still shows what the still was drawn at.
const abovePlaybackCap = (step: SurfaceResolution) =>
  SURFACE_TRIANGLE_LIMITS[step] >
  SURFACE_TRIANGLE_LIMITS[PLAYBACK_MAX_RESOLUTION];
const stepDisabled = (step: SurfaceResolution) =>
  playing.value && abovePlaybackCap(step);
const surfaceOpened = ref(false);
const surfaceBoundaries = ref<SurfaceBoundaryInfo[]>([]);
const hiddenBoundaries = ref<Set<number>>(new Set());
const surfaceView = ref<InstanceType<typeof BoundarySurface> | null>(null);

const frames = ref<OutputPoint[]>([]);
const frameIndex = ref(0);
// Resource path of the frame the surface view is showing; empty until the
// player first moves, after which it drives the viewer instead of the route.
const shownFrame = ref("");
const playing = ref(false);
const fps = ref(1);
const playbackCapped = ref(false);
const lockedRange = ref<[number, number] | null>(null);
const parcelLockedRange = ref<[number, number] | null>(null);
// The range a settled frame reported, tagged with the field it was measured
// from. The lock is taken when the sequence starts moving, and between picking
// a new field and its first frame arriving the only range in hand still belongs
// to the old one — locking that would colour the new field against another
// quantity's scale.
let lastRange: { field: string; range: [number, number] } | null = null;
let lastParcelRange: { field: string; range: [number, number] } | null = null;
let playTimer = 0;
let scrubTimer = 0;
let frameShownAt = 0;
let playStartedAt = 0;
let framesDir = "";

let controller: AbortController | null = null;

const path = computed(() => fileStore.req?.path ?? "");

const surfacePath = computed(() => shownFrame.value || path.value);

// Only the visible 3D view follows the player; the hidden one would otherwise
// stream every frame too. It stays frozen where it was and catches up the
// moment its tab is opened.
const surfaceShown = ref("");
const parcelShown = ref("");
watchEffect(() => {
  if (activeTab.value === "surface" || !shownFrame.value) {
    surfaceShown.value = surfacePath.value;
  }
});
watchEffect(() => {
  if (activeTab.value === "parcels" || !shownFrame.value) {
    parcelShown.value = surfacePath.value;
  }
});

const downloadUrl = computed(() =>
  fileStore.req ? api.files.getDownloadURL(fileStore.req, false) : ""
);

const kindIcon = computed(() => {
  switch (summary.value?.kind) {
    case "post":
      return "fa-cubes text-purple-500 dark:text-purple-400";
    case "restart":
      return "fa-rotate-right text-yellow-700 dark:text-yellow-500";
    case "map":
      return "fa-right-left text-green-600 dark:text-green-400";
    case "table":
      return "fa-table-cells text-green-600 dark:text-green-400";
    default:
      return "fa-database text-purple-500 dark:text-purple-400";
  }
});

const parcelGroups = computed(
  () => summary.value?.streams.flatMap((s) => s.parcels ?? []) ?? []
);

const parcelScalars = computed(() => {
  const group = parcelGroups.value.find((g) => g.path === parcelGroup.value);
  // Coordinates are how the cloud is positioned, not something to colour by.
  return (group?.variables ?? []).filter(
    (v) => !v.startsWith("PARCEL_") && !v.startsWith("XX_")
  );
});

// The boundary surface is recoverable from any stream that carries face
// connectivity and names its boundaries — the faces whose owner is negative
// are the wetted wall.
const surfaceStream = computed(
  () =>
    (summary.value?.boundaries?.length
      ? summary.value.streams.find((s) => (s.faces ?? 0) > 0)?.name
      : undefined) ?? ""
);

// Any cell-centred field can colour the wall: its value at a boundary face is
// taken from the cell on the fluid side. BOUND_HTC and BOUND_FLUX are the ones
// worth looking at, but they are written only when the deck asks for them and
// read all-zero in plenty of files, so nothing is chosen by default.
const surfaceScalars = computed(() => {
  const stream = summary.value?.streams.find(
    (s) => s.name === surfaceStream.value
  );
  return (stream?.variables ?? []).filter((v) => !v.type.startsWith("string"));
});

const hasParcels = computed(() =>
  parcelGroups.value.some((g) => g.hasCoords && g.count > 0)
);

const hasBoundaries = computed(() => !!summary.value?.boundaries?.length);

// Every tab is offered for every file. A file that carries nothing behind one
// says so when it is opened, which is a shorter path to knowing than a tab bar
// whose contents change from one output to the next and leave you wondering
// whether the parcels are missing or the viewer is.
const tabs = computed(() => [
  { id: "variables", icon: "fa-table-list", label: t("h5View.variables") },
  { id: "surface", icon: "fa-draw-polygon", label: t("h5View.surface") },
  { id: "parcels", icon: "fa-spray-can", label: t("h5View.parcels") },
  { id: "boundaries", icon: "fa-border-all", label: t("h5View.boundaries") },
]);

const facts = computed(() => {
  const s = summary.value;
  if (!s) return [];

  const out: { label: string; value: string; title?: string }[] = [
    { label: t("h5View.kind"), value: t(`h5View.kind_${s.kind}`) },
    { label: t("h5View.fileSize"), value: formatBytes(s.size) },
  ];

  if (s.time) {
    // The unit is not a formatting choice: CRANK_FLAG decides whether this
    // number is seconds or crank-angle degrees, and mislabelling it would be a
    // real error, not a cosmetic one.
    out.push({
      label:
        s.time.unit === "deg" ? t("h5View.crankAngle") : t("h5View.simTime"),
      value: formatSimTime(s.time.value, s.time.unit),
      title:
        s.time.seconds !== undefined
          ? formatSimTime(s.time.seconds, "s")
          : undefined,
    });
  }
  if (s.time?.rpm) {
    out.push({ label: t("h5View.rpm"), value: formatCount(s.time.rpm) });
  }

  const stream = s.streams[0];
  if (stream?.cells) {
    out.push({ label: t("h5View.cells"), value: formatCount(stream.cells) });
  }
  if (stream?.faces) {
    out.push({
      label: t("h5View.facesLabel"),
      value: formatCount(stream.faces),
    });
  }
  if (s.cycle !== undefined) {
    out.push({ label: t("h5View.cycle"), value: formatCount(s.cycle) });
  }
  if (s.ranks !== undefined) {
    out.push({ label: t("h5View.ranks"), value: formatCount(s.ranks) });
  }
  if (s.solver) {
    out.push({ label: t("h5View.solver"), value: s.solver });
  }
  return out;
});

const diverged = computed(() =>
  [...stats.value.values()].filter((s) => divergenceOf(s).diverged)
);

const selectedBytes = computed(() => {
  let total = 0;
  for (const stream of summary.value?.streams ?? []) {
    for (const v of stream.variables) {
      if (selected.value.has(v.path)) total += v.bytes;
    }
  }
  return total;
});

const subsetHref = computed(() =>
  h5.subsetURL(path.value, [...selected.value], authStore.token)
);

const statsFor = (v: h5.H5Variable) => stats.value.get(v.path);

const onSurfaceBoundaries = (info: SurfaceBoundaryInfo[]) => {
  surfaceBoundaries.value = info;
  // Chip choices survive frame swaps: boundary ids are stable across AMR, and
  // unhiding everything on every frame would make the player useless for
  // looking inside a chamber.
  const kept = new Set(
    [...hiddenBoundaries.value].filter((id) => info.some((b) => b.id === id))
  );
  hiddenBoundaries.value = kept;
  for (const id of kept) {
    surfaceView.value?.setBoundaryVisible(id, false);
  }
};

const frameResourcePath = (name: string) =>
  url.removeLastDir(fileStore.req?.path ?? "") + "/" + name;

const surfaceRequest = () => ({
  stream: surfaceStream.value,
  scalar: surfaceScalar.value || undefined,
  edges: surfaceRepresentation.value === "edges",
  resolution: surfaceResolution.value,
});

// What makes one ramp's range comparable to another's. A parcel group is part
// of it: the same variable over a different spray is a different population.
const surfaceField = () => `${surfaceStream.value} ${surfaceScalar.value}`;
const parcelField = () => `${parcelGroup.value} ${parcelScalar.value}`;

// The range of the frame on screen, or null when the field it was measured
// from is no longer the one selected.
const settledSurfaceRange = () =>
  lastRange?.field === surfaceField() ? lastRange.range : null;
const settledParcelRange = () =>
  lastParcelRange?.field === parcelField() ? lastParcelRange.range : null;

const frameCaption = computed(() => {
  const frame = frames.value[frameIndex.value];
  if (!frame || frame.time === null) return "";
  return `${formatSimTime(frame.time, summary.value?.time?.unit)} · ${
    frameIndex.value + 1
  }/${frames.value.length}`;
});

// The sibling post files of this run, ordered by write index so a restart
// that revisits a crank angle doesn't fold the sequence back on itself.
const loadFrames = async () => {
  if (summary.value?.kind !== "post") {
    frames.value = [];
    return;
  }
  const dir = url.removeLastDir(route.path) + "/";
  if (framesDir !== dir) {
    framesDir = dir;
    frames.value = [];
    try {
      const listing = await api.files.fetch(dir);
      if (framesDir !== dir) return;
      frames.value = outputProfile(
        (listing.items ?? []).filter((item) => !item.isDir)
      );
    } catch {
      framesDir = "";
    }
  }
  syncFrameIndex();
};

const syncFrameIndex = () => {
  const name = shownFrame.value
    ? shownFrame.value.split("/").pop()
    : fileStore.req?.name;
  const index = frames.value.findIndex((f) => f.name === name);
  if (index >= 0) frameIndex.value = index;
};

const showFrame = (index: number) => {
  const clamped = Math.max(0, Math.min(index, frames.value.length - 1));
  const frame = frames.value[clamped];
  if (!frame) return;
  // Lock the ramps to the ranges in effect when the sequence starts moving,
  // so the colours keep one meaning instead of re-normalising every frame.
  const settled = settledSurfaceRange();
  if (surfaceScalar.value && !lockedRange.value && settled) {
    lockedRange.value = settled;
  }
  const settledParcels = settledParcelRange();
  if (parcelScalar.value && !parcelLockedRange.value && settledParcels) {
    parcelLockedRange.value = settledParcels;
  }
  frameIndex.value = clamped;
  shownFrame.value = frameResourcePath(frame.name);
  frameShownAt = performance.now();
};

const advance = () => {
  if (!playing.value || frames.value.length < 2) return;

  // Playback loops, so it has no end of its own: a tab left running streams a
  // fresh surface per frame until it is closed, and every one of those is a
  // full pass over a post file on the server. Cap the run rather than trust
  // someone to come back and stop it.
  if (performance.now() - playStartedAt >= PLAYBACK_MAX_MS) {
    playbackCapped.value = true;
    pause();
    return;
  }

  showFrame((frameIndex.value + 1) % frames.value.length);
};

const play = () => {
  if (frames.value.length < 2) return;
  if (!resolutionChosen) {
    resolutionChosen = true;
    surfaceResolution.value = PLAYBACK_SURFACE_RESOLUTION;
  } else if (abovePlaybackCap(surfaceResolution.value)) {
    surfaceResolution.value = PLAYBACK_MAX_RESOLUTION;
  }
  playing.value = true;
  playbackCapped.value = false;
  playStartedAt = performance.now();
  frameShownAt = performance.now();
  window.clearTimeout(playTimer);
  playTimer = window.setTimeout(advance, 1000 / fps.value);
};

const stopPlayback = () => {
  playing.value = false;
  window.clearTimeout(playTimer);
};

const syncFrameRoute = () => {
  const frame = frames.value[frameIndex.value];
  if (!frame || !shownFrame.value || frame.name === fileStore.req?.name) {
    return;
  }
  router.replace({
    path: url.removeLastDir(route.path) + "/" + encodeURIComponent(frame.name),
    query: route.query,
  });
};

const pause = () => {
  stopPlayback();
  syncFrameRoute();
};

const togglePlay = () => (playing.value ? pause() : play());

const stepFrame = (delta: number) => {
  stopPlayback();
  showFrame(frameIndex.value + delta);
};

const onScrub = (index: number) => {
  stopPlayback();
  frameIndex.value = index;
  // Committing every tick of a drag would fire a fetch per notch; the label
  // tracks the thumb and the frame follows once it settles.
  window.clearTimeout(scrubTimer);
  scrubTimer = window.setTimeout(() => showFrame(frameIndex.value), 150);
};

const rescaleRange = () => {
  if (activeTab.value === "parcels") {
    const settled = settledParcelRange();
    if (settled) parcelLockedRange.value = [settled[0], settled[1]];
    return;
  }
  const settled = settledSurfaceRange();
  if (settled) lockedRange.value = [settled[0], settled[1]];
};

const prefetchNext = () => {
  if (!shownFrame.value || frames.value.length < 2) return;
  const next = frames.value[(frameIndex.value + 1) % frames.value.length];
  if (!next) return;
  if (activeTab.value === "parcels") {
    if (parcelGroup.value) {
      prefetchParcels(frameResourcePath(next.name), {
        group: parcelGroup.value,
        scalar: parcelScalar.value || undefined,
      });
    }
  } else {
    prefetchSurface(frameResourcePath(next.name), surfaceRequest());
  }
};

// Paces playback: the next frame is scheduled only once the current one is on
// screen, so the fps setting is a ceiling and a slow network simply plays
// slower instead of piling up requests. Only the visible view drives the
// transport — a frozen view catching up must not schedule anything.
const paceFrame = (ok: boolean) => {
  if (!ok) {
    stopPlayback();
    return;
  }

  prefetchNext();

  if (playing.value) {
    const wait = Math.max(
      0,
      1000 / fps.value - (performance.now() - frameShownAt)
    );
    window.clearTimeout(playTimer);
    playTimer = window.setTimeout(advance, wait);
  } else {
    // A step or scrub landed: let the rest of the viewer (facts, variables,
    // deep link) catch up with what the view is showing.
    syncFrameRoute();
  }
};

// A load only reports once it is the newest one outstanding, so the field it
// was measured from is the one selected now.
const onSurfaceLoaded = (range: [number, number] | null) => {
  lastRange = range ? { field: surfaceField(), range } : null;
  if (activeTab.value === "surface") paceFrame(range !== null);
};

const onParcelLoaded = (range: [number, number] | null) => {
  lastParcelRange = range ? { field: parcelField(), range } : null;
  if (activeTab.value === "parcels") paceFrame(range !== null);
};

const toggleBoundary = (id: number) => {
  const next = new Set(hiddenBoundaries.value);
  if (next.has(id)) {
    next.delete(id);
  } else {
    next.add(id);
  }
  hiddenBoundaries.value = next;
  surfaceView.value?.setBoundaryVisible(id, !next.has(id));
};

const streamSubtitle = (stream: h5.H5Stream) => {
  const parts: string[] = [];
  if (stream.cells)
    parts.push(t("h5View.nCells", { n: formatCount(stream.cells) }));
  if (stream.vertices) {
    parts.push(t("h5View.nVertices", { n: formatCount(stream.vertices) }));
  }
  parts.push(t("h5View.nVariables", { n: stream.variables.length }));
  return parts.join(" · ");
};

const allSelected = (stream: h5.H5Stream) =>
  stream.variables.length > 0 &&
  stream.variables.every((v) => selected.value.has(v.path));

const toggleAll = (stream: h5.H5Stream) => {
  const next = new Set(selected.value);
  if (allSelected(stream)) {
    stream.variables.forEach((v) => next.delete(v.path));
  } else {
    stream.variables.forEach((v) => next.add(v.path));
  }
  selected.value = next;
};

const toggleOne = (v: h5.H5Variable) => {
  const next = new Set(selected.value);
  if (next.has(v.path)) {
    next.delete(v.path);
  } else {
    next.add(v.path);
  }
  selected.value = next;
};

// Each variable is a full read of that field, so stats are opt-in rather than
// part of the initial load, and requested in bounded batches.
const STATS_BATCH = 32;

// What the server accepts in one subset request. The link is a plain anchor,
// so exceeding it would navigate the tab to a bare "400" page rather than
// failing where the user is looking.
const MAX_SUBSET = 64;

const loadStats = async (stream: h5.H5Stream) => {
  const numeric = stream.variables.filter((v) => !v.type.startsWith("string"));
  if (numeric.length === 0) return;

  // Every post file in a case names its variables identically, so a batch
  // that outlives a file switch would file the old file's numbers under the
  // new file's paths and read as perfectly plausible. Bind the whole run to
  // the load that started it.
  const signal = controller?.signal;
  statsPending.value = true;
  statsError.value = "";
  try {
    for (let i = 0; i < numeric.length; i += STATS_BATCH) {
      const batch = numeric.slice(i, i + STATS_BATCH);
      const res = await h5.stats(
        path.value,
        batch.map((v) => v.path),
        signal
      );
      if (signal?.aborted) return;
      const next = new Map(stats.value);
      for (const entry of res) {
        if (!entry.error) next.set(entry.path, entry);
      }
      stats.value = next;
    }
  } catch (e: any) {
    if (e?.name === "AbortError") return;
    statsError.value = e?.message ?? String(e);
  } finally {
    if (!signal?.aborted) statsPending.value = false;
  }
};

const load = async () => {
  if (!path.value) return;
  controller?.abort();
  controller = new AbortController();

  // Moving between sibling frames of one playback sequence is a soft reload:
  // the 3D view stays mounted (tearing it down would refetch megabytes and
  // reset the camera) and only the file-level panels refresh.
  const soft =
    summary.value !== null &&
    framesDir === url.removeLastDir(route.path) + "/" &&
    frames.value.some((f) => f.name === fileStore.req?.name);

  if (soft) {
    stopPlayback();
    if (shownFrame.value) {
      // Keeps the surface honest against the route (browser back included);
      // when they already agree the string is unchanged and nothing reloads.
      shownFrame.value = frameResourcePath(fileStore.req?.name ?? "");
    }
  } else {
    loading.value = true;
    surfaceBoundaries.value = [];
    hiddenBoundaries.value = new Set();
    stopPlayback();
    shownFrame.value = "";
    frames.value = [];
    framesDir = "";
    lockedRange.value = null;
    lastRange = null;
    parcelLockedRange.value = null;
    lastParcelRange = null;
  }
  error.value = "";
  statsError.value = "";
  statsPending.value = false;
  stats.value = new Map();
  selected.value = new Set();

  try {
    summary.value = await h5.summary(path.value, controller.signal);
    const first = parcelGroups.value.find((g) => g.hasCoords && g.count > 0);
    parcelGroup.value = first?.path ?? "";
    parcelScalar.value = parcelScalars.value.includes("TEMP") ? "TEMP" : "";
    if (surfaceOpened.value || activeTab.value === "parcels") loadFrames();
  } catch (e: any) {
    if (e?.name === "AbortError") return;
    error.value =
      e?.status === 415 ? t("h5View.notHDF5") : (e?.message ?? String(e));
  } finally {
    loading.value = false;
  }
};

const close = () => {
  router.push({ path: url.removeLastDir(route.path) + "/" });
};

const keyEvent = (event: KeyboardEvent) => {
  if (event.code === "Escape") {
    close();
    return;
  }
  if (
    (activeTab.value !== "surface" && activeTab.value !== "parcels") ||
    frames.value.length < 2
  ) {
    return;
  }
  const target = event.target as HTMLElement | null;
  if (
    target &&
    ["INPUT", "SELECT", "TEXTAREA", "BUTTON"].includes(target.tagName)
  ) {
    return;
  }
  if (event.code === "Space") {
    event.preventDefault();
    togglePlay();
  } else if (event.code === "ArrowRight") {
    event.preventDefault();
    stepFrame(1);
  } else if (event.code === "ArrowLeft") {
    event.preventDefault();
    stepFrame(-1);
  }
};

watch(path, () => load());

// The surface is fetched only once someone asks for it, and stays mounted
// afterwards so returning to the tab is free.
watch(activeTab, (tab) => {
  if (tab === "surface") {
    surfaceOpened.value = true;
    loadFrames();
  } else if (tab === "parcels") {
    loadFrames();
  }
});

// Picking a step before ever pressing play still counts as choosing it, so
// the first play leaves it alone.
watch(surfaceResolution, () => {
  resolutionChosen = true;
});

// A different field has a different range; carrying the old lock over would
// colour it with another quantity's scale.
watch(surfaceScalar, () => {
  lockedRange.value = null;
});
watch(parcelScalar, () => {
  parcelLockedRange.value = null;
});

// A field the new file does not carry would be a 404 where the wall should be.
watch(surfaceScalars, (list) => {
  if (
    surfaceScalar.value !== "" &&
    !list.some((v) => v.name === surfaceScalar.value)
  ) {
    surfaceScalar.value = "";
  }
});

// Groups do not all carry the same variables; asking for a scalar the new
// group lacks is a 404 where a cloud should be.
watch(parcelGroup, () => {
  if (!parcelScalars.value.includes(parcelScalar.value)) {
    parcelScalar.value = parcelScalars.value.includes("TEMP") ? "TEMP" : "";
  }
});

onMounted(() => {
  window.addEventListener("keydown", keyEvent);
  load();
});

onBeforeUnmount(() => {
  window.removeEventListener("keydown", keyEvent);
  controller?.abort();
  stopPlayback();
  window.clearTimeout(scrubTimer);
  // The frames a session cached are only worth their memory while the viewer
  // that fetched them is on screen.
  clearSurfaceCache();
  clearParcelCache();
});
</script>
