// The page's maps folder served from public/maps, as fetch finds it in the browser.
import { readFileSync } from 'node:fs';
import path from 'node:path';

import { vi } from 'vitest';

const MAPS = path.resolve(process.cwd(), 'public/maps');

/** The JSON mapjson wrote for id. */
export function mapJson(id: string): unknown {
  return JSON.parse(readFileSync(path.join(MAPS, `${id}.json`), 'utf8')) as unknown;
}

/** Stubs fetch so maps/<id>.json comes from disk, or from served for the ids it names; vi.unstubAllGlobals takes it back. */
export function serveMaps(served: Readonly<Record<string, unknown>> = {}): void {
  vi.stubGlobal('fetch', (url: string) => {
    const id = /maps\/([\w-]+)\.json$/.exec(url)?.[1];
    if (!id) {
      return Promise.resolve(new Response(null, { status: 404 }));
    }
    try {
      return Promise.resolve(Response.json(id in served ? served[id] : mapJson(id)));
    } catch {
      return Promise.resolve(new Response(null, { status: 404 }));
    }
  });
}
