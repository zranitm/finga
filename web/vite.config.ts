/// <reference types="vitest/config" />
import { fileURLToPath, URL } from 'node:url';

import react from '@vitejs/plugin-react';
import { defineConfig, type Plugin } from 'vite';

// The dev proxy makes the client same-origin with the server, so no CORS setup is needed.
const server = 'http://localhost:8080';

const BABYLON = /[\\/]@babylonjs[\\/]/;
const GLSL = /[\\/]@babylonjs[\\/]core[\\/]Shaders[\\/]/; // WebGL's shaders, each imported as a material first needs it; ShadersWGSL is WebGPU's
const WORLD = /[\\/]src[\\/]game[\\/]world3d[\\/]run\.ts$/; // the 3D world, imported on entering it (world3d/handle.ts)

/** Babylon in two files where rolldown made about 130: the world's up-front imports and the GLSL shaders; a slow line pays a round trip a file. */
function babylonChunks(): Plugin & { upFront: (id: string) => boolean } {
  const imports = new Map<string, readonly string[]>();
  const entries: string[] = [];
  let world: Set<string> | null = null;
  const reach = (from: readonly string[]) => {
    const seen = new Set<string>();
    for (const todo = [...from]; todo.length > 0;) {
      const id = todo.pop()!;
      if (!seen.has(id)) {
        seen.add(id);
        todo.push(...(imports.get(id) ?? []));
      }
    }
    return seen;
  };
  return {
    name: 'babylon-chunks',
    apply: 'build',
    buildStart() {
      imports.clear();
      entries.length = 0;
      world = null;
    },
    moduleParsed(info) {
      imports.set(info.id, info.importedIds);
      if (info.isEntry) {
        entries.push(info.id);
      }
    },
    // Asked once chunking starts, when every module is parsed: Babylon the world imports up front and the page doesn't.
    upFront(id) {
      if (!world) {
        const page = reach(entries);
        const all = reach([...imports.keys()].filter((m) => WORLD.test(m)));
        world = new Set([...all].filter((m) => BABYLON.test(m) && !page.has(m)));
      }
      return world.has(id);
    },
    // The page never loads Babylon, and the world loads its own code and Babylon's one file up front.
    generateBundle(_, bundle) {
      const chunks = new Map(
        Object.values(bundle).flatMap((c) =>
          c.type === 'chunk' ? [[c.fileName, c] as const] : [],
        ),
      );
      const loads = (from: string[]) => {
        const seen = new Set<string>();
        for (const todo = [...from]; todo.length > 0;) {
          const f = todo.pop()!;
          if (!seen.has(f)) {
            seen.add(f);
            todo.push(...(chunks.get(f)?.imports ?? []));
          }
        }
        return seen;
      };
      const page = loads([...chunks.values()].filter((c) => c.isEntry).map((c) => c.fileName));
      const run = [...chunks.values()].find(
        (c) => c.facadeModuleId && WORLD.test(c.facadeModuleId),
      );
      const babylon = [...page].filter((f) => chunks.get(f)!.name.startsWith('babylon'));
      const extra = [...loads(run ? [run.fileName] : [])].filter(
        (f) => f !== run?.fileName && !page.has(f),
      );
      if (
        !run ||
        babylon.length > 0 ||
        extra.length !== 1 ||
        chunks.get(extra[0]!)!.name !== 'babylon'
      ) {
        this.error(
          `Babylon's chunks: the page loads ${babylon.join(', ') || 'none'} up front and the world ${extra.join(', ') || 'nothing'}; want none, and babylon alone`,
        );
      }
    },
  };
}

const babylon = babylonChunks();

export default defineConfig({
  plugins: [react(), babylon],
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
  },
  build: {
    rolldownOptions: {
      output: {
        codeSplitting: {
          // Each takes only what it names: its modules' own imports would pull the page's in with them.
          groups: [
            {
              name: 'babylon-glsl',
              test: GLSL,
              priority: 2,
              includeDependenciesRecursively: false,
            },
            {
              name: 'babylon',
              test: babylon.upFront,
              priority: 1,
              includeDependenciesRecursively: false,
            },
          ],
        },
      },
    },
  },
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      '/api': server,
      '/ws': { target: server.replace('http', 'ws'), ws: true },
    },
  },
  test: {
    environment: 'jsdom',
    // A worker reuses its modules across files: setting each file up from scratch cost about 3x the tests themselves.
    isolate: false,
    setupFiles: ['src/testing/setup.ts'],
    include: ['src/**/*.test.{ts,tsx}'],
    coverage: {
      provider: 'v8',
      reporter: ['text', 'html'],
      // The 3D world and components are checked by playing, not by the coverage floor.
      include: [
        'src/lib/**',
        'src/protocol/**',
        'src/sim/**',
        'src/net/**',
        'src/state/**',
        'src/game/systems/**',
      ],
      exclude: ['**/*.test.{ts,tsx}', '**/*.lintfixture.ts', 'src/protocol/gen/**'],
      thresholds: { lines: 80, statements: 80, functions: 80, branches: 80 },
    },
  },
});
