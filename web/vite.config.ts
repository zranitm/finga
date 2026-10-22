/// <reference types="vitest/config" />
import { fileURLToPath, URL } from 'node:url';

import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';

// One config for dev, build and test (page 7). The dev proxy makes the
// client same-origin with the game server on :8080, so cors_origins stays
// empty and VITE_API_ORIGIN unset in Stage 1.
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
    // Phaser in its own chunk (page 7): the game engine changes rarely and
    // the app code often, so browsers keep the big chunk cached.
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
      // The 80% floor from page 7 applies to the layers tested over plain
      // data; scenes, entities and components are checked by playing.
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
