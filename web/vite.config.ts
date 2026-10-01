import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

const target = process.env.LINUXADMIN_API ?? 'http://127.0.0.1:9090';

export default defineConfig({
  plugins: [react()],
  server: {
    host: '127.0.0.1',
    port: 5173,
    proxy: {
      '/api': { target, ws: true, changeOrigin: false },
      '/plugins/': { target, changeOrigin: false },
    },
  },
  // assetsInlineLimit 0: never inline fonts/images as data: URIs (strict CSP served by linuxadmind)
  build: { outDir: 'dist', sourcemap: false, chunkSizeWarningLimit: 700, assetsInlineLimit: 0 },
});
