// A test plan's bake read as the world map the client draws: its ground whole, as mapjson's JSON and its chunks give a region.
import { loadMap } from '@/game/world3d/map';
import type { WorldMap } from '@/game/world3d/map';
import { readChunk } from '@/game/world3d/region-bake';

import { servePublic } from './public-on-disk';
import { fixtureChunks, fixtureFile } from './region-fixture';

/** The plan's region as a map with a gate at its start, every chunk of its ground in; it stubs fetch as servePublic does. */
async function bakedMap(id: string, start: readonly [number, number]): Promise<WorldMap> {
  servePublic({
    [id]: {
      id,
      ground: 'grass',
      sky: 'day',
      box_models: [],
      cylinder_models: [],
      props: [],
      portals: [{ name: 'start', at: start, radius: 1.2, to: id }],
      region: { id, hash: 'fixture' },
    },
  });
  const map = await loadMap(id);
  for (const file of fixtureChunks(id)) {
    map.region!.ground.add(readChunk(fixtureFile(id, file)));
  }
  return map;
}

/** The steep plan: a ridge and a 12 m plateau with a cliff edge, under peaks. */
export const steepMap = () => bakedMap('steep', [24, 30]);

/** The terrace plan: a 5 m terrace and a 9 m shelf with cliff edges, a ridge, and a low edge ring as the shipped regions have. */
export const terraceMap = () => bakedMap('terrace', [24, 30]);
