import tippy, { type Instance, type Props } from "tippy.js";
import type { Directive, DirectiveBinding } from "vue";

type TooltipValue = string | undefined | null | false | Partial<Props>;

interface TooltipElement extends HTMLElement {
  _tippy?: Instance;
}

const optionsFor = (value: TooltipValue): Partial<Props> | null => {
  if (!value) return null;

  const options: Partial<Props> =
    typeof value === "string" ? { content: value } : { ...value };

  if (!options.content) return null;

  return {
    // Matches Horizon's defaults so timings feel identical between the two apps.
    delay: [400, 0],
    duration: [150, 100],
    placement: "top",
    ...options,
  };
};

const apply = (el: TooltipElement, binding: DirectiveBinding<TooltipValue>) => {
  const options = optionsFor(binding.value);

  if (!options) {
    el._tippy?.destroy();
    delete el._tippy;
    return;
  }

  if (el._tippy) {
    el._tippy.setProps(options);
    return;
  }

  el._tippy = tippy(el, options);
};

export const tooltip: Directive<TooltipElement, TooltipValue> = {
  mounted: apply,
  updated: apply,
  beforeUnmount(el) {
    // tippy appends its box to <body>; without this it outlives the element.
    el._tippy?.destroy();
    delete el._tippy;
  },
};
