import { fetchURL, removePrefix, createURL } from "./utils";
import { baseURL } from "@/utils/constants";
import { encodePath } from "@/utils/url";

export async function fetch(
  url: string,
  password: string = "",
  captcha: string = ""
) {
  url = removePrefix(url);

  const headers: Record<string, string> = {
    "X-SHARE-PASSWORD": encodeURIComponent(password),
  };

  if (captcha !== "") {
    headers["X-SHARE-CAPTCHA"] = captcha;
  }

  const res = await fetchURL(`/api/public/share${url}`, { headers }, false);

  const data = (await res.json()) as Resource;
  data.url = `/share${url}`;

  if (data.isDir) {
    if (!data.url.endsWith("/")) data.url += "/";
    data.items = data.items.map((item: any, index: any) => {
      item.index = index;
      item.url = `${data.url}${encodeURIComponent(item.name)}`;

      if (item.isDir) {
        item.url += "/";
      }

      return item;
    });
  }

  return data;
}

export function download(
  format: DownloadFormat,
  hash: string,
  password: string,
  ...files: string[]
) {
  let url = `${baseURL}/api/public/dl/${hash}`;

  if (files.length === 1) {
    url += encodePath(files[0]) + "?";
  } else {
    const arg = encodeURIComponent(files.map(encodePath).join(","));
    url += `/?files=${arg}&`;
  }

  if (format) {
    url += `algo=${format}&`;
  }

  if (password) {
    url += `password=${encodeURIComponent(password)}&`;
  }

  window.open(url);
}

export function getDownloadURL(res: Resource, inline = false, password = "") {
  const params: Record<string, string> = {
    ...(inline && { inline: "true" }),
    ...(password && { password }),
  };

  return createURL("api/public/dl/" + res.hash + res.path, params);
}
