/// <reference types="vitest/config" />
import { fileURLToPath, URL } from 'node:url';

import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';

// The dev proxy makes the client same-origin with the server, so no CORS setup is needed.
const server = 'http://localhost:8080';

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
  },
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      '/api': server,
      '/healthz': server,
      '/readyz': server,
      '/ws': { target: server.replace('http', 'ws'), ws: true },
    },
  },
  build: {
    // Phaser in its own chunk: it changes rarely, so browsers keep it cached.
    rolldownOptions: {
      output: {
        codeSplitting: {
          groups: [{ name: 'phaser', test: /[\\/]node_modules[\\/]phaser[\\/]/ }],
        },
      },
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['src/testing/setup.ts'],
    include: ['src/**/*.test.{ts,tsx}'],
    coverage: {
      provider: 'v8',
      reporter: ['text', 'html'],
      // Scenes, entities and components are checked by playing, not by the coverage floor.
      include: [
        'src/lib/**',
        'src/protocol/**',
        'src/sim/**',
        'src/net/**',
        'src/state/**',
        'src/game/systems/**',
      ],
      exclude: ['**/*.test.{ts,tsx}', '**/*.lintfixture.ts'],
      thresholds: { lines: 80, statements: 80, functions: 80, branches: 80 },
    },
  },
});
