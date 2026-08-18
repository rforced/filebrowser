import type { Camera, Scene, WebGLRenderer } from "three";

// Exports render at 2x the on-screen size so the saved image holds up when
// zoomed or dropped into a report, even from a 1x display.
const EXPORT_SCALE = 2;

/**
 * pngFilename names a captured view after the file it came from, extension
 * swapped for .png, with an optional qualifier such as the scalar the surface
 * is coloured by.
 */
export function pngFilename(name: string, qualifier?: string): string {
  const base = name.replace(/\.[^./]+$/, "") || name;
  return qualifier ? `${base}_${qualifier}.png` : `${base}.png`;
}

/**
 * saveViewPng downloads the renderer's current view as a PNG. The drawing
 * buffer is not preserved between frames, so the render and the pixel read
 * must happen within one task; copying through a 2D canvas also composites
 * the transparent canvas onto the backdrop the user actually sees.
 */
export function saveViewPng(
  renderer: WebGLRenderer,
  scene: Scene,
  camera: Camera,
  filename: string
): void {
  const canvas = renderer.domElement;
  const { clientWidth: width, clientHeight: height } = canvas;
  if (width === 0 || height === 0) {
    return;
  }

  const ratio = renderer.getPixelRatio();
  const scale = Math.max(ratio, EXPORT_SCALE);
  renderer.setPixelRatio(scale);
  renderer.setSize(width, height, false);
  renderer.render(scene, camera);

  const out = document.createElement("canvas");
  out.width = width * scale;
  out.height = height * scale;
  const ctx = out.getContext("2d");
  if (ctx) {
    ctx.fillStyle = backdrop(canvas);
    ctx.fillRect(0, 0, out.width, out.height);
    ctx.drawImage(canvas, 0, 0);
  }

  renderer.setPixelRatio(ratio);
  renderer.setSize(width, height, false);
  renderer.render(scene, camera);

  if (!ctx) {
    return;
  }
  out.toBlob((blob) => {
    if (!blob) {
      return;
    }
    const href = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = href;
    link.download = filename;
    link.click();
    setTimeout(() => URL.revokeObjectURL(href), 1000);
  });
}

const backdrop = (el: HTMLElement): string => {
  for (let node: HTMLElement | null = el; node; node = node.parentElement) {
    const bg = getComputedStyle(node).backgroundColor;
    if (bg && bg !== "transparent" && bg !== "rgba(0, 0, 0, 0)") {
      return bg;
    }
  }
  return "#fff";
};
