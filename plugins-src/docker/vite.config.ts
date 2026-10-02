import { defineConfig } from 'vite';
import { fileURLToPath } from 'node:url';

const src = (p: string) => fileURLToPath(new URL(`./src/${p}`, import.meta.url));

// One self-contained ES module at plugins/docker/index.js. React is never bundled: `react` and the JSX runtime
// are aliased to shims that forward to the SDK's React (see src/react-shim.ts).
export default defineConfig({
  resolve: {
    alias: [
      { find: /^react\/jsx-(dev-)?runtime$/, replacement: src('jsx-runtime-shim.ts') },
      { find: /^react$/, replacement: src('react-shim.ts') },
    ],
  },
  build: {
    target: 'es2022',
    outDir: '../../plugins/docker',
    emptyOutDir: false, // the folder also holds manifest.json, manifest.sig and assets
    minify: true,
    sourcemap: false,
    cssCodeSplit: false,
    lib: { entry: src('index.ts'), formats: ['es'], fileName: () => 'index.js' },
    rolldownOptions: { output: { codeSplitting: false, minify: true } }, // lib mode keeps whitespace otherwise
  },
});
