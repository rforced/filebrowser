export const ACE_ASSET_DIR = "ace";
export const BUNDLED_MODES = [
  "batchfile",
  "c_cpp",
  "css",
  "diff",
  "dockerfile",
  "fortran",
  "golang",
  "html",
  "ini",
  "java",
  "javascript",
  "json",
  "latex",
  "lua",
  "makefile",
  "markdown",
  "matlab",
  "perl",
  "php",
  "plain_text",
  "powershell",
  "properties",
  "python",
  "r",
  "ruby",
  "rust",
  "sh",
  "sql",
  "text",
  "toml",
  "typescript",
  "xml",
  "yaml",
] as const;

const BUNDLED = new Set<string>(BUNDLED_MODES);

export const clampMode = (mode: string): string => {
  const name = mode.replace(/^ace\/mode\//, "");
  return BUNDLED.has(name) ? `ace/mode/${name}` : "ace/mode/text";
};
