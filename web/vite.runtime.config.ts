// Builds the plugin frame runtime (web/src/plugins/frame/runtime.tsx) into one self-contained classic script,
// public/plugin-runtime.js, served at /plugin-runtime.js and loaded by the daemon's sandboxed /plugin-frame/<id>.
// One file with React, the UI kit and its CSS + fonts inline: the frame has an opaque origin and a CSP without
// network access, so it cannot fetch chunks, stylesheets or fonts. Run by `npm run dev` and `npm run build`.
import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

const shim = fileURLToPath(new URL('./src/plugins/frame/i18n-shim.ts', import.meta.url));

export default defineConfig({
  plugins: [react()],
  publicDir: false,
  // The UI kit imports '../i18n' (the app's provider-based i18n); in the frame it gets a small stand-in.
  resolve: { alias: [{ find: /^\.\.\/i18n$/, replacement: shim }] },
  define: { 'process.env.NODE_ENV': JSON.stringify('production') },
  build: {
    outDir: 'public',
    emptyOutDir: false,
    sourcemap: false,
    minify: true,
    assetsInlineLimit: () => true,
    copyPublicDir: false,
    lib: {
      entry: fileURLToPath(new URL('./src/plugins/frame/runtime.tsx', import.meta.url)),
      formats: ['iife'],
      name: 'ErvisioPluginRuntime',
      fileName: () => 'plugin-runtime.js',
    },
  },
});
