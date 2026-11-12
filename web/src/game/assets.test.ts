import { existsSync, readFileSync } from 'node:fs';
import path from 'node:path';

import { describe, expect, it } from 'vitest';

import { items } from '@/gamedata/items';

import { images, mapFile } from './assets';
import { MAP_IDS, TEX, UNKNOWN_ITEM, itemTexture } from './keys';
import { drawPlaceholders } from './scenes/placeholders';

const publicDir = path.resolve(process.cwd(), 'public');

describe('asset manifest', () => {
  it('lists every texture key exactly once', () => {
    const keys = images.map((a) => a.key).sort();
    expect(keys).toEqual(Object.values(TEX).sort());
  });

  it.each(images.map((a) => ({ key: a.key, file: a.file })))(
    '$key has its file under public/assets ($file)',
    ({ file }) => {
      expect(existsSync(path.join(publicDir, 'assets', file))).toBe(true);
    },
  );

  it.each(MAP_IDS.map((id) => ({ id })))(
    'map $id is under public/maps, without the boss layer that holds its kill count',
    ({ id }) => {
      const file = path.join(publicDir, mapFile(id));
      expect(existsSync(file)).toBe(true);
      const map = JSON.parse(readFileSync(file, 'utf8')) as { layers: { name: string }[] };
      expect(map.layers.map((l) => l.name)).not.toContain('boss');
    },
  );

  it('LICENSES.md names every pack folder in use and the placeholders', () => {
    const text = readFileSync(path.join(publicDir, 'assets', 'LICENSES.md'), 'utf8');
    const folders = new Set(images.map((a) => a.file.split('/')[0]));
    for (const folder of folders) {
      expect(text).toContain(`\`${folder}/\``);
    }
    expect(text).toContain('src/game/scenes/placeholders.ts');
    expect(text).toContain('CC0');
  });

  it.each([
    { template: 'red_potion', texture: TEX.flask },
    { template: 'blue_potion', texture: TEX.flask },
    { template: 'wooden_sword', texture: 'wooden_sword' },
  ])('$template is drawn with $texture', ({ template, texture }) => {
    expect(itemTexture(template)).toBe(texture);
  });

  it('every item in the catalog has a texture, and so does one this client does not know', () => {
    const { drawn } = drawAll();
    const loaded = new Set<string>(images.map((a) => a.key));
    for (const { id } of items) {
      const key = itemTexture(id);
      expect(drawn.has(key) || loaded.has(key), id).toBe(true);
    }
    expect(itemTexture('a_newer_servers_item')).toBe(UNKNOWN_ITEM);
    expect(drawn.has(UNKNOWN_ITEM)).toBe(true);
  });

  it("draws equipment in its stars' colour, the one the item windows frame it with", () => {
    const css = readFileSync(path.resolve(process.cwd(), 'src/styles/global.css'), 'utf8');
    const { colors } = drawAll();
    for (const item of items) {
      if (item.tab !== 'equip') {
        continue;
      }
      const star = /--star-(\d): #([0-9a-f]{6});/g;
      const hex = [...css.matchAll(star)].find((m) => Number(m[1]) === item.stars)?.[2];
      expect(colors.get(item.id), item.id).toContain(Number.parseInt(hex ?? '', 16));
    }
  });
});

/** Runs drawPlaceholders on a scene that only records: the keys it drew, and the colours each used. */
function drawAll(): { drawn: Set<string>; colors: Map<string, number[]> } {
  const drawn = new Set<string>();
  const colors = new Map<string, number[]>();
  let used: number[] = [];
  const g: Record<string, unknown> = {};
  const graphics = new Proxy(g, {
    get: (_, name: string) => {
      if (name === 'generateTexture') {
        return (key: string) => {
          drawn.add(key);
          colors.set(key, used);
          used = [];
          return graphics;
        };
      }
      return (...args: unknown[]) => {
        if (name === 'fillStyle') {
          used.push(args[0] as number);
        } else if (name === 'lineStyle') {
          used.push(args[1] as number);
        }
        return graphics;
      };
    },
  });
  const scene = {
    textures: { exists: (key: string) => drawn.has(key) },
    make: { graphics: () => graphics },
  };
  drawPlaceholders(scene as unknown as Parameters<typeof drawPlaceholders>[0]);
  return { drawn, colors };
}
