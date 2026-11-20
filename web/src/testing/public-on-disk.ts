// The page's public folder served from disk, as fetch finds it in the browser: the maps and the effects manifest, and the region bakes in regions/ beside this file, which regionbake's tests keep current, else the shipped regions' bakes npm run maps wrote.
import { existsSync, readFileSync } from 'node:fs';
import path from 'node:path';

import { vi } from 'vitest';

const PUBLIC = path.resolve(process.cwd(), 'public');
const REGIONS = path.resolve(process.cwd(), 'src/testing/regions');

/** The JSON mapjson wrote for id. */
export function mapJson(id: string): unknown {
  return JSON.parse(readFileSync(path.join(PUBLIC, 'maps', `${id}.json`), 'utf8')) as unknown;
}

/** Stubs fetch so a JSON file under public comes from disk, or maps/<id>.json from served for the ids it names, and a region's bake file from regions/ whatever its hash; vi.unstubAllGlobals takes it back. */
export function servePublic(served: Readonly<Record<string, unknown>> = {}): void {
  vi.stubGlobal('fetch', (url: string) => {
    const file = /(?:^|\/)((?:maps|assets)\/[\w/.-]+\.json)$/.exec(url)?.[1];
    const id = file && /^maps\/([\w-]+)\.json$/.exec(file)?.[1];
    const baked = /(?:^|\/)regions\/([\w-]+)\/(\w+)\/([\w/.-]+)$/.exec(url);
    try {
      if (baked) {
        const [, id, hash, rel] = baked as unknown as [string, string, string, string];
        const fixture = path.join(REGIONS, id, rel);
        return Promise.resolve(
          new Response(
            readFileSync(
              existsSync(fixture) ? fixture : path.join(PUBLIC, 'regions', id, hash, rel),
            ),
          ),
        );
      }
      if (id && id in served) {
        return Promise.resolve(Response.json(served[id]));
      }
      if (file) {
        return Promise.resolve(
          Response.json(JSON.parse(readFileSync(path.join(PUBLIC, file), 'utf8')) as unknown),
        );
      }
    } catch {
      // not on disk: as the server answers
    }
    return Promise.resolve(new Response(null, { status: 404 }));
  });
}
