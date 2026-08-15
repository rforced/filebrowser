import { fetchURL, fetchJSON, removePrefix, createURL } from "./utils";

export async function list() {
  return fetchJSON<Share[]>("/api/shares");
}

export async function get(url: string) {
  url = removePrefix(url);
  return fetchJSON<Share>(`/api/share${url}`);
}

export async function remove(hash: string) {
  await fetchURL(`/api/share/${hash}`, {
    method: "DELETE",
  });
}

export async function create(
  url: string,
  password = "",
  expires = "",
  unit = "hours"
) {
  url = removePrefix(url);
  return fetchJSON(`/api/share${url}`, {
    method: "POST",
    body: JSON.stringify({ password, expires, unit }),
  });
}

export function getShareURL(share: Share) {
  return createURL("share/" + share.hash, {});
}
