// A Node test over the filesystem (C07 page): every key in TEX is in the
// manifest, every manifest file exists, and the maps are where the
// Preloader looks for them.
import { existsSync, readFileSync } from 'node:fs';
import path from 'node:path';

import { describe, expect, it } from 'vitest';

import { images, mapFile } from './assets';
import { ITEM_PLACEHOLDERS, ITEM_TINTS, MAP_IDS, TEX, itemTexture } from './keys';

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

  it.each(MAP_IDS.map((id) => ({ id })))('map $id is under public/maps', ({ id }) => {
    expect(existsSync(path.join(publicDir, mapFile(id)))).toBe(true);
  });

  it('LICENSES.md names every pack folder in use and the placeholders', () => {
    const text = readFileSync(path.join(publicDir, 'assets', 'LICENSES.md'), 'utf8');
    const folders = new Set(images.map((a) => a.file.split('/')[0]));
    for (const folder of folders) {
      expect(text).toContain(`\`${folder}/\``);
    }
    for (const template of ITEM_PLACEHOLDERS) {
      expect(text).toContain(template);
    }
    expect(text).toContain('CC0');
  });

  it.each([
    { template: 'red_potion', texture: TEX.flask },
    { template: 'blue_potion', texture: TEX.flask },
    { template: 'wooden_sword', texture: 'wooden_sword' },
  ])('$template is drawn with $texture', ({ template, texture }) => {
    expect(itemTexture(template)).toBe(texture);
  });

  it('every item template has art: a placeholder or a tint', () => {
    const templates = [...ITEM_PLACEHOLDERS, ...Object.keys(ITEM_TINTS)].sort();
    expect(templates).toEqual(
      [
        'wooden_sword',
        'short_bow',
        'iron_sword',
        'hunting_bow',
        'leather_cap',
        'leather_vest',
        'red_potion',
        'blue_potion',
        'slime_gel',
        'mushroom_cap',
        'boar_tusk',
        'bat_wing',
      ].sort(),
    );
  });
});
