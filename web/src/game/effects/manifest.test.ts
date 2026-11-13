import { existsSync, readFileSync } from 'node:fs';
import path from 'node:path';

import { describe, expect, it } from 'vitest';

import { drawnVersion } from './drawn';
import { effectIds } from './ids';
import { MANIFEST_FILE, parseManifest } from './manifest';

const sheet = { sheet: 'effects/heal.png', frameWidth: 64, frameHeight: 48, frames: 8, fps: 24 };

describe('effects manifest', () => {
  it('names every attack and active skill, each with a drawn version to fall back to', () => {
    expect(effectIds).toContain('attack.warrior');
    expect(effectIds).toContain('skill.spark');
    expect(effectIds).not.toContain('skill.second_wind'); // passive: nothing to see
    expect(effectIds.filter((id) => !drawnVersion(id))).toEqual([]);
  });

  it('what ships parses clean, and every file it lists is there', () => {
    const publicDir = path.resolve(process.cwd(), 'public');
    const raw: unknown = JSON.parse(readFileSync(path.join(publicDir, MANIFEST_FILE), 'utf8'));
    const m = parseManifest(raw, effectIds);
    expect(m.problems).toEqual([]);
    const files = [...m.entries.values()].flatMap((e) => [e.sheet?.file, e.sound]);
    const missing = files.filter((f) => f && !existsSync(path.join(publicDir, 'assets', f)));
    expect(missing).toEqual([]); // the server answers a missing one with the page, which Phaser fails to decode
  });

  it('reads a sheet with its anchor at the centre unless set, and a sound alone', () => {
    const m = parseManifest(
      {
        'skill.heal': { ...sheet, sound: 'effects/heal.ogg' },
        'attack.warrior': { ...sheet, anchor: [0, 1] },
        'skill.spark': { sound: 'effects/zap.ogg' },
      },
      effectIds,
    );
    expect(m.problems).toEqual([]);
    expect(m.entries.get('skill.heal')).toEqual({
      sheet: {
        file: 'effects/heal.png',
        frameWidth: 64,
        frameHeight: 48,
        frames: 8,
        fps: 24,
        anchor: [0.5, 0.5],
      },
      sound: 'effects/heal.ogg',
    });
    expect(m.entries.get('attack.warrior')?.sheet?.anchor).toEqual([0, 1]);
    expect(m.entries.get('skill.spark')).toEqual({ sound: 'effects/zap.ogg' });
  });

  it('refuses an id it does not know and an entry it cannot use, keeping the rest', () => {
    const m = parseManifest(
      {
        'skill.fireball': sheet,
        'skill.heal': sheet,
        'attack.warrior': {},
        'attack.thief': { sound: 3 },
        'attack.bowman': { ...sheet, sheet: '' },
        'attack.magician': { ...sheet, frames: 2.5 },
        'attack.beginner': { ...sheet, fps: Number.NaN },
        'skill.strike': { ...sheet, anchor: [0.5, 2] },
        'skill.throw': 'effects/stone.png',
        'skill.spark': { sheet: 'effects/zap.png' },
        'skill.haste': { ...sheet, frame: 3 },
      },
      effectIds,
    );
    expect([...m.entries.keys()]).toEqual(['skill.heal']);
    expect(m.problems).toEqual([
      '"skill.fireball" is not an effect id',
      '"attack.warrior" names neither a sheet nor a sound',
      '"attack.thief" sound is not a file path',
      '"attack.bowman" sheet is not a file path',
      '"attack.magician" frames is not a whole number',
      '"attack.beginner" fps is not a number',
      '"skill.strike" anchor.1 is above 1',
      '"skill.throw" is not an object',
      '"skill.spark" has a sheet without its frameWidth, frameHeight, frames and fps',
      '"skill.haste" has frame, not a field it knows',
    ]);
  });

  it('refuses a manifest that is not an object of ids', () => {
    expect(parseManifest([sheet], effectIds)).toEqual({
      entries: new Map(),
      problems: ['the manifest is not an object of effect ids'],
    });
  });
});
