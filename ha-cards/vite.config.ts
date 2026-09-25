import { resolve } from 'node:path';
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  base: './',
  input: {
    app: resolve(import.meta.dirname, 'index.html'),
    'ajaxbridge-lovelace': resolve(import.meta.dirname, 'src/ha/register.tsx'),
  },
  plugins: [react()],
  build: {
    license: true,
    manifest: true,
    rolldownOptions: {
      output: {
        entryFileNames: (chunkInfo) =>
          chunkInfo.name === 'ajaxbridge-lovelace' ? 'ajaxbridge-lovelace.js' : 'assets/[name].js',
        chunkFileNames: 'assets/[name]-[hash].js',
        assetFileNames: 'assets/[name]-[hash][extname]',
      },
    },
  },
  server: {
    port: 5173,
    strictPort: true,
    forwardConsole: {
      unhandledErrors: true,
      logLevels: ['warn', 'error'],
    },
  },
});
