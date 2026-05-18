/**
 * Builds the target path for the catch-all route.
 *
 * Kept in its own module (with zero non-stdlib imports) so it can be unit
 * tested without dragging the full router graph — i18n, pinia stores, every
 * view component — into the test environment.
 *
 * Vue Router's behavior for an unmatched catch-all param has historically
 * differed between versions (sometimes `undefined`, sometimes an empty array,
 * sometimes a single string). The previous implementation did
 * `[...to.params.catchAll].join("/")`, which crashed when the router started
 * returning `undefined` for the root URL (vue-router 5.0.7), taking the whole
 * SPA — including the login page — down with it. This helper normalizes every
 * shape into an array before joining.
 */
export function catchAllRedirect(
  param: string | string[] | undefined | null
): string {
  const segments = Array.isArray(param) ? param : param ? [param] : [];
  return `/files/${segments.join("/")}`;
}
