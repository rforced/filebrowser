import { describe, expect, it, beforeEach } from "vitest";
import { mount } from "@vue/test-utils";
import { nextTick } from "vue";

import ContextMenu from "../ContextMenu.vue";

const MENU_WIDTH = 200;
const BORDERS = 2;

const mountMenu = (menuHeight: number) => {
  const wrapper = mount(ContextMenu, {
    props: { show: false, pos: { x: 0, y: 0 } },
    slots: { default: "<button>action</button>" },
    attachTo: document.body,
  });

  const el = wrapper.element as HTMLElement;
  Object.defineProperty(el, "scrollHeight", { get: () => menuHeight });
  Object.defineProperty(el, "offsetHeight", {
    get: () => menuHeight + BORDERS,
  });
  Object.defineProperty(el, "clientHeight", { get: () => menuHeight });
  Object.defineProperty(el, "offsetWidth", { get: () => MENU_WIDTH });

  return wrapper;
};

const open = async (
  wrapper: ReturnType<typeof mountMenu>,
  pos: { x: number; y: number }
) => {
  await wrapper.setProps({ show: true, pos });
  await nextTick();
  return (wrapper.element as HTMLElement).style;
};

describe("ContextMenu placement", () => {
  beforeEach(() => {
    window.innerWidth = 1000;
    window.innerHeight = 800;
    window.scrollTo(0, 0);
  });

  it("opens downwards when there is room below the cursor", async () => {
    const style = await open(mountMenu(300), { x: 100, y: 100 });

    expect(style.top).toBe("100px");
    expect(style.left).toBe("100px");
  });

  it("opens upwards when the menu would run off the bottom", async () => {
    const style = await open(mountMenu(300), { x: 100, y: 700 });

    expect(style.top).toBe(`${700 - 302}px`);
  });

  it("accounts for page scroll when deciding to flip", async () => {
    window.scrollTo(0, 1000);
    const style = await open(mountMenu(300), { x: 100, y: 1700 });

    expect(style.top).toBe(`${1700 - 302}px`);
  });

  it("keeps opening downwards while scrolled if there is room below", async () => {
    window.scrollTo(0, 1000);
    const style = await open(mountMenu(300), { x: 100, y: 1100 });

    expect(style.top).toBe("1100px");
  });

  it("clamps to the right edge on the first open", async () => {
    const style = await open(mountMenu(300), { x: 950, y: 100 });

    expect(style.left).toBe(`${1000 - MENU_WIDTH - 8}px`);
  });

  it("scrolls instead of overflowing when taller than the viewport", async () => {
    const style = await open(mountMenu(900), { x: 100, y: 700 });

    expect(style.maxHeight).toBe(`${800 - 16}px`);
    expect(style.top).toBe("8px");
  });
});
