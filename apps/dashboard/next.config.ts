import path from "node:path";

import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // §4.35: sem depender de nada da Vercel. §3 fixa deploy em VPS com Coolify.
  output: "standalone",
  // @vhook/i18n é TypeScript cru no workspace; sem isto o Next não o compila.
  transpilePackages: ["@vhook/i18n"],
  // A raiz do workspace tem outro package.json; sem apontar o Next para ela, o
  // standalone copia a árvore errada de node_modules.
  outputFileTracingRoot: path.join(__dirname, "../.."),
  cacheComponents: true,
  partialPrefetching: true,
  turbopack: {
    rules: {
      "*.css": {
        loaders: ["@tailwindcss/turbopack"],
        as: "*.css",
      },
    },
  },
};

export default nextConfig;
