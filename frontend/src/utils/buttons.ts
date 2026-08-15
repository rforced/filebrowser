import { reactive } from "vue";

export type ButtonState = "idle" | "loading" | "success";

const states = reactive<Record<string, ButtonState>>({});

const SUCCESS_HOLD_MS = 600;

const timers: Record<string, number> = {};

const clearTimer = (button: string) => {
  if (timers[button] !== undefined) {
    window.clearTimeout(timers[button]);
    delete timers[button];
  }
};

function loading(button: string) {
  clearTimer(button);
  states[button] = "loading";
}

function done(button: string) {
  clearTimer(button);
  states[button] = "idle";
}

function success(button: string) {
  clearTimer(button);
  states[button] = "success";
  timers[button] = window.setTimeout(() => {
    states[button] = "idle";
    delete timers[button];
  }, SUCCESS_HOLD_MS);
}

/** Current state of a button, for components that need more than the icon. */
export const buttonState = (button: string): ButtonState =>
  states[button] ?? "idle";

/**
 * Resolve the Font Awesome classes for a button, substituting a spinner while
 * busy and a tick on success.
 *
 * `defaultIcon` is the button's normal icon, e.g. "fa-trash".
 */
export const buttonIcon = (button: string, defaultIcon: string): string => {
  switch (states[button]) {
    case "loading":
      return "fa-spinner fa-spin";
    case "success":
      return "fa-check";
    default:
      return defaultIcon;
  }
};

/** True while the named button is mid-flight; use to disable it. */
export const isButtonBusy = (button: string): boolean =>
  states[button] === "loading";

export default {
  loading,
  done,
  success,
};
