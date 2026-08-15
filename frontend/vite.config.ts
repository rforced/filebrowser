import fs from "node:fs";
import path from "node:path";
import { defineConfig, type Plugin } from "vite";
import vue from "@vitejs/plugin-vue";
import tailwindcss from "@tailwindcss/vite";
import VueI18nPlugin from "@intlify/unplugin-vue-i18n/vite";
import { compression } from "vite-plugin-compression2";

import { ACE_ASSET_DIR, BUNDLED_MODES } from "./src/utils/aceAssets";

const ACE_SOURCE_DIR = path.resolve(
  __dirname,
  "node_modules/ace-builds/src-min-noconflict"
);

const aceAssetFiles = (): string[] => {
  const themes = fs
    .readdirSync(ACE_SOURCE_DIR)
    .filter((file) => file.startsWith("theme-") && file.endsWith(".js"));

  const exts = fs
    .readdirSync(ACE_SOURCE_DIR)
    .filter((file) => file.startsWith("ext-") && file.endsWith(".js"));

  const modes = BUNDLED_MODES.map((mode) => `mode-${mode}.js`).filter((file) =>
    fs.existsSync(path.join(ACE_SOURCE_DIR, file))
  );

  const snippets = BUNDLED_MODES.map((mode) => `snippets/${mode}.js`).filter(
    (file) => fs.existsSync(path.join(ACE_SOURCE_DIR, file))
  );

  return [...themes, ...exts, ...modes, ...snippets];
};

const aceAssets = (): Plugin => ({
  name: "ace-assets",

  configureServer(server) {
    server.middlewares.use((req, res, next) => {
      const prefix = `/${ACE_ASSET_DIR}/`;
      if (!req.url?.startsWith(prefix)) return next();

      const file = req.url.slice(prefix.length).split("?")[0];
      const full = path.join(ACE_SOURCE_DIR, path.normalize(file));

      if (!full.startsWith(ACE_SOURCE_DIR + path.sep)) return next();
      if (!fs.existsSync(full)) return next();

      res.setHeader("Content-Type", "text/javascript");
      fs.createReadStream(full).pipe(res);
    });
  },

  generateBundle() {
    for (const file of aceAssetFiles()) {
      this.emitFile({
        type: "asset",
        fileName: `${ACE_ASSET_DIR}/${file}`,
        source: fs.readFileSync(path.join(ACE_SOURCE_DIR, file)),
      });
    }
  },
});

const plugins = [
  vue(),
  tailwindcss(),
  aceAssets(),
  VueI18nPlugin({
    include: [path.resolve(__dirname, "./src/i18n/**/*.json")],
  }),
  compression({ include: /\.js$/, deleteOriginalAssets: false }),
];

const resolve = {
  alias: {
    // vue: "@vue/compat",
    "@/": `${path.resolve(__dirname, "src")}/`,
  },
};

// https://vitejs.dev/config/
export default defineConfig(({ command }) => {
  if (command === "serve") {
    return {
      plugins,
      resolve,
      server: {
        proxy: {
          "/api": "http://127.0.0.1:8080",
        },
      },
    };
  } else {
    // command === 'build'
    return {
      plugins,
      resolve,
      base: "",
      build: {
        rollupOptions: {
          input: {
            index: path.resolve(__dirname, "./public/index.html"),
          },
          output: {
            manualChunks: (id) => {
              // bundle dayjs files in a single chunk
              // this avoids having small files for each locale
              if (id.includes("dayjs/")) {
                return "dayjs";
                // bundle i18n in a separate chunk
              } else if (id.includes("i18n/")) {
                return "i18n";
                // keep three's core out of the entry chunk: it is only needed
                // when a 3D model is previewed
              } else if (id.includes("node_modules/three/build/")) {
                return "three";
              }
            },
          },
        },
      },
      experimental: {
        renderBuiltUrl(filename, { hostType }) {
          if (hostType === "js") {
            return { runtime: `window.__prependStaticUrl("${filename}")` };
          } else if (hostType === "html") {
            return `[{[ .StaticURL ]}]/${filename}`;
          } else {
            return { relative: true };
          }
        },
      },
    };
  }
});
