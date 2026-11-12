// The client draws exactly the maps the server simulates, less the boss layer: players never see how many kills bring a boss. public/maps is git-ignored.
import { mkdirSync, readFileSync, readdirSync, rmSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const source = join(here, '..', '..', 'server', 'data', 'maps');
const target = join(here, '..', 'public', 'maps');

rmSync(target, { recursive: true, force: true });
mkdirSync(target, { recursive: true });
const maps = readdirSync(source).filter((name) => name.endsWith('.tmj'));
if (maps.length === 0) {
  throw new Error(`no .tmj maps in ${source}`);
}
for (const name of maps) {
  const map = JSON.parse(readFileSync(join(source, name), 'utf8'));
  map.layers = map.layers.filter((layer) => layer.name !== 'boss');
  writeFileSync(join(target, name), JSON.stringify(map));
}
console.log(`copied ${maps.length} maps to public/maps`);
