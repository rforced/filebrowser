import { isSurfaceDatName } from "@/utils/convergeSurface";

export interface FileIcon {
  /** Font Awesome classes, e.g. `fa-solid fa-file-lines`. */
  icon: string;
  /** Tailwind text-colour classes, including the dark-mode variant. */
  color: string;
}

const RED = "text-red-500 dark:text-red-300";
const ORANGE = "text-orange-500 dark:text-orange-400";
const YELLOW = "text-yellow-700 dark:text-yellow-500";
const GREEN = "text-green-600 dark:text-green-400";
const BLUE = "text-blue-500 dark:text-blue-100";
const VIOLET = "text-purple-500 dark:text-purple-400";
const GRAY = "text-gray-500 dark:text-gray-400";
const TEAL = "text-teal-700 dark:text-teal-400";

const MODEL: FileIcon = { icon: "fa-solid fa-cube", color: TEAL };

const FIELD: FileIcon = { icon: "fa-solid fa-cubes", color: VIOLET };
const RESTART: FileIcon = { icon: "fa-solid fa-rotate-right", color: YELLOW };
const MAPPED: FileIcon = { icon: "fa-solid fa-right-left", color: GREEN };
const LOOKUP: FileIcon = { icon: "fa-solid fa-table-cells", color: GREEN };
const DATASET: FileIcon = { icon: "fa-solid fa-database", color: VIOLET };

const BY_EXTENSION: Record<string, FileIcon> = {
  ".in": { icon: "fa-solid fa-file-pen", color: GREEN },
  ".echo": { icon: "fa-solid fa-file-lines", color: GRAY },
  ".rst": RESTART,
  ".h5": DATASET,
  ".cgns": FIELD,
  ".out": { icon: "fa-solid fa-file-lines", color: GRAY },
  ".stl": MODEL,
  ".obj": MODEL,
  ".3mf": MODEL,

  // Image
  ".ai": { icon: "fa-solid fa-image", color: RED },
  ".odg": { icon: "fa-solid fa-image", color: ORANGE },
  ".xcf": { icon: "fa-solid fa-image", color: ORANGE },
  ".psd": { icon: "fa-solid fa-image", color: RED },
  ".ico": { icon: "fa-solid fa-image", color: BLUE },
  ".emf": { icon: "fa-solid fa-image", color: BLUE },

  // Presentation
  ".odp": { icon: "fa-solid fa-file-powerpoint", color: ORANGE },
  ".ppt": { icon: "fa-solid fa-file-powerpoint", color: ORANGE },
  ".pptx": { icon: "fa-solid fa-file-powerpoint", color: ORANGE },

  // Spreadsheet / database
  ".csv": { icon: "fa-solid fa-table", color: GREEN },
  ".db": { icon: "fa-solid fa-table", color: GREEN },
  ".odb": { icon: "fa-solid fa-table", color: GREEN },
  ".ods": { icon: "fa-solid fa-table", color: GREEN },
  ".xls": { icon: "fa-solid fa-file-excel", color: GREEN },
  ".xlsx": { icon: "fa-solid fa-file-excel", color: GREEN },

  // Document
  ".doc": { icon: "fa-solid fa-file-word", color: BLUE },
  ".docx": { icon: "fa-solid fa-file-word", color: BLUE },
  ".log": { icon: "fa-solid fa-file-lines", color: GRAY },
  ".odt": { icon: "fa-solid fa-file-lines", color: BLUE },
  ".rtf": { icon: "fa-solid fa-file-lines", color: BLUE },
  ".pdf": { icon: "fa-solid fa-file-pdf", color: RED },

  // Code
  ".c": { icon: "fa-solid fa-file-code", color: GRAY },
  ".cpp": { icon: "fa-solid fa-file-code", color: GRAY },
  ".cs": { icon: "fa-solid fa-file-code", color: BLUE },
  ".css": { icon: "fa-solid fa-file-code", color: YELLOW },
  ".go": { icon: "fa-solid fa-file-code", color: GREEN },
  ".h": { icon: "fa-solid fa-file-code", color: GRAY },
  ".html": { icon: "fa-solid fa-file-code", color: ORANGE },
  ".java": { icon: "fa-solid fa-file-code", color: RED },
  ".js": { icon: "fa-solid fa-file-code", color: YELLOW },
  ".json": { icon: "fa-solid fa-file-code", color: YELLOW },
  ".kt": { icon: "fa-solid fa-file-code", color: VIOLET },
  ".php": { icon: "fa-solid fa-file-code", color: VIOLET },
  ".py": { icon: "fa-solid fa-file-code", color: BLUE },
  ".rb": { icon: "fa-solid fa-file-code", color: RED },
  ".rs": { icon: "fa-solid fa-file-code", color: ORANGE },
  ".vue": { icon: "fa-solid fa-file-code", color: ORANGE },
  ".xml": { icon: "fa-solid fa-file-code", color: GRAY },
  ".yml": { icon: "fa-solid fa-file-code", color: GRAY },
  ".yaml": { icon: "fa-solid fa-file-code", color: GRAY },
  ".sh": { icon: "fa-solid fa-terminal", color: GRAY },

  // Executable
  ".apk": { icon: "fa-solid fa-window-maximize", color: GREEN },
  ".dex": { icon: "fa-solid fa-window-maximize", color: GREEN },
  ".bat": { icon: "fa-solid fa-window-maximize", color: BLUE },
  ".exe": { icon: "fa-solid fa-window-maximize", color: BLUE },
  ".jar": { icon: "fa-solid fa-window-maximize", color: RED },
  ".ps1": { icon: "fa-solid fa-terminal", color: BLUE },

  // Installer
  ".deb": { icon: "fa-solid fa-box", color: GRAY },
  ".msi": { icon: "fa-solid fa-box", color: BLUE },
  ".pkg": { icon: "fa-solid fa-box", color: GRAY },
  ".rpm": { icon: "fa-solid fa-box", color: GRAY },

  // Compressed
  ".7z": { icon: "fa-solid fa-file-zipper", color: GRAY },
  ".bz2": { icon: "fa-solid fa-file-zipper", color: GRAY },
  ".cab": { icon: "fa-solid fa-file-zipper", color: BLUE },
  ".gz": { icon: "fa-solid fa-file-zipper", color: GRAY },
  ".rar": { icon: "fa-solid fa-file-zipper", color: VIOLET },
  ".tar": { icon: "fa-solid fa-file-zipper", color: GRAY },
  ".xz": { icon: "fa-solid fa-file-zipper", color: GRAY },
  ".zip": { icon: "fa-solid fa-file-zipper", color: YELLOW },
  ".zst": { icon: "fa-solid fa-file-zipper", color: GRAY },
  ".lz4": { icon: "fa-solid fa-file-zipper", color: GRAY },

  // Disk image
  ".ccd": { icon: "fa-solid fa-compact-disc", color: GRAY },
  ".dmg": { icon: "fa-solid fa-compact-disc", color: BLUE },
  ".iso": { icon: "fa-solid fa-compact-disc", color: VIOLET },
  ".mdf": { icon: "fa-solid fa-compact-disc", color: GRAY },
  ".vdi": { icon: "fa-solid fa-compact-disc", color: GRAY },
  ".vhd": { icon: "fa-solid fa-compact-disc", color: GRAY },
  ".vmdk": { icon: "fa-solid fa-compact-disc", color: GRAY },
  ".wim": { icon: "fa-solid fa-compact-disc", color: BLUE },

  // Font
  ".otf": { icon: "fa-solid fa-font", color: GRAY },
  ".ttf": { icon: "fa-solid fa-font", color: GRAY },
  ".woff": { icon: "fa-solid fa-font", color: GRAY },
  ".woff2": { icon: "fa-solid fa-font", color: GRAY },

  // Media not covered by the server's type classification
  ".aac": { icon: "fa-solid fa-volume-high", color: BLUE },
  ".mp2": { icon: "fa-solid fa-volume-high", color: BLUE },
  ".mp3": { icon: "fa-solid fa-volume-high", color: BLUE },
  ".mp4": { icon: "fa-solid fa-film", color: BLUE },
  ".mpg": { icon: "fa-solid fa-film", color: BLUE },
  ".vob": { icon: "fa-solid fa-film", color: BLUE },
};

/** Fallbacks keyed on the server's coarse type classification. */
const BY_TYPE: Record<string, FileIcon> = {
  audio: { icon: "fa-solid fa-volume-high", color: YELLOW },
  blob: { icon: "fa-solid fa-file", color: GRAY },
  image: { icon: "fa-solid fa-image", color: ORANGE },
  model: MODEL,
  pdf: { icon: "fa-solid fa-file-pdf", color: RED },
  text: { icon: "fa-solid fa-file-lines", color: GRAY },
  video: { icon: "fa-solid fa-film", color: VIOLET },
  invalid_link: { icon: "fa-solid fa-link-slash", color: RED },
};

/** Name rules, tried before the extension map, lowest-cardinality first. */
const BY_NAME: [RegExp, FileIcon][] = [
  [/^post.*\.h5$/, FIELD],
  [/^map.*\.h5$/, MAPPED],
  [/table.*\.h5$/, LOOKUP],
];

const DIRECTORY: FileIcon = { icon: "fa-solid fa-folder", color: BLUE };
const FALLBACK: FileIcon = { icon: "fa-solid fa-file", color: GRAY };

export interface IconSubject {
  isDir?: boolean;
  type?: string;
  extension?: string;
  name?: string;
}

/** Resolve the icon and colour for a listing item. */
export const fileIcon = (item: IconSubject): FileIcon => {
  if (item.isDir) return DIRECTORY;

  const name = item.name?.toLowerCase();
  if (name) {
    if (isSurfaceDatName(name)) return MODEL;
    for (const [pattern, icon] of BY_NAME) {
      if (pattern.test(name)) return icon;
    }
  }

  const ext = item.extension?.toLowerCase();
  if (ext && BY_EXTENSION[ext]) return BY_EXTENSION[ext];

  if (item.type && BY_TYPE[item.type]) return BY_TYPE[item.type];

  return FALLBACK;
};

/**
 * Dotfiles and `.bak` files render dimmed, matching the previous
 * `opacity: 0.33` rules in listing-icons.css.
 */
export const isMutedFile = (item: IconSubject): boolean =>
  item.name?.startsWith(".") === true ||
  item.extension?.toLowerCase() === ".bak";
