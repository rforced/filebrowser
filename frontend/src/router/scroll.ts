import type { RouterScrollBehavior } from "vue-router";

export const scrollOnNavigate: RouterScrollBehavior = (
  to,
  from,
  savedPosition
) => {
  if (savedPosition) return savedPosition;

  if (to.path === from.path) return false;

  return { top: 0 };
};
