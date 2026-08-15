interface ResourceBase {
  path: string;
  name: string;
  size: number;
  extension: string;
  modified: string; // ISO 8601 datetime
  mode: number;
  isDir: boolean;
  isSymlink: boolean;
  type: ResourceType;
  url: string;
}

interface Resource extends ResourceBase {
  items: ResourceItem[];
  numDirs: number;
  numFiles: number;
  sorting: Sorting;
  hash?: string;
  index: number;
  content?: string;
}

interface ResourceItem extends ResourceBase {
  index: number;
}

type ResourceType =
  | "dir"
  | "video"
  | "audio"
  | "image"
  | "pdf"
  | "text"
  | "blob"
  | "model"
  | "textImmutable";

type DownloadFormat = "zip" | "tar" | "targz" | "tarlz4" | "tarzst" | null;

interface ClipItem {
  from: string;
  name: string;
  size?: number;
  isDir?: boolean;
  modified?: string;
}

interface BreadCrumb {
  name: string;
  url: string;
}

interface ConflictingItem {
  lastModified: number | string | undefined;
  size: number | undefined;
}

interface ConflictingResource {
  index: number;
  name: string;
  origin: ConflictingItem;
  dest: ConflictingItem;
  checked: Array<"origin" | "dest">;
  isSmallerOnServer?: boolean;
}

interface RecursiveEntry {
  path: string;
  name: string;
  size: number;
  modified: string;
  isDir: boolean;
}

interface RecursiveListing {
  items: RecursiveEntry[];
  truncated: boolean;
}
