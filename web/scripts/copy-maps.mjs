// The client draws exactly the maps the server simulates; public/maps is git-ignored.
import { cpSync, mkdirSync, readdirSync, rmSync } from 'node:fs';
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
  cpSync(join(source, name), join(target, name));
}
console.log(`copied ${maps.length} maps to public/maps`);
