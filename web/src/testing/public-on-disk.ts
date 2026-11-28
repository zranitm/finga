// The page's public folder served from disk, as fetch finds it in the browser: the maps (and the hand-kept yards in yards/ beside this file where public has none) and the effects manifest, and the region bakes in regions/ beside this file, which regionbake's tests keep current, else the shipped regions' bakes npm run maps wrote.
import { existsSync, readFileSync } from 'node:fs';
import path from 'node:path';

import { vi } from 'vitest';

const PUBLIC = path.resolve(process.cwd(), 'public');
const REGIONS = path.resolve(process.cwd(), 'src/testing/regions');
const MAPS = path.resolve(process.cwd(), 'src/testing/yards'); // the hand-kept maps with the land features the match maps lack, a village yard's houses and a ridge's mesa edge, which the tests still draw

/** The JSON mapjson wrote for id, or the hand-kept map's of that id. */
export function mapJson(id: string): unknown {
  const own = path.join(PUBLIC, 'maps', `${id}.json`);
  return JSON.parse(
    readFileSync(existsSync(own) ? own : path.join(MAPS, `${id}.json`), 'utf8'),
  ) as unknown;
}

/** Stubs fetch so a JSON file under public comes from disk, or maps/<id>.json from served for the ids it names, and a region's bake file from served by its path, as regions/<id>/<hash>/region.json, else from regions/ whatever its hash; vi.unstubAllGlobals takes it back. */
export function servePublic(served: Readonly<Record<string, unknown>> = {}): void {
  vi.stubGlobal('fetch', (url: string) => {
    const file = /(?:^|\/)((?:maps|assets)\/[\w/.-]+\.json)$/.exec(url)?.[1];
    const id = file && /^maps\/([\w-]+)\.json$/.exec(file)?.[1];
    const baked = /(?:^|\/)regions\/([\w-]+)\/(\w+)\/([\w/.-]+)$/.exec(url);
    try {
      if (baked) {
        const [, id, hash, rel] = baked as unknown as [string, string, string, string];
        const own = `regions/${id}/${hash}/${rel}`;
        if (own in served) {
          return Promise.resolve(Response.json(served[own]));
        }
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
        const own = path.join(PUBLIC, file);
        const fixture = id && path.join(MAPS, `${id}.json`);
        return Promise.resolve(
          Response.json(
            JSON.parse(
              readFileSync(!existsSync(own) && fixture ? fixture : own, 'utf8'),
            ) as unknown,
          ),
        );
      }
    } catch {
      // not on disk: as the server answers
    }
    return Promise.resolve(new Response(null, { status: 404 }));
  });
}
