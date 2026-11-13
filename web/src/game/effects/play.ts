// Plays an effect by id: its sheet and sound where they loaded, else its drawn version; a missing file is logged once.
import type Phaser from 'phaser';

import { DEPTH } from '../depth';
import { ART_SCALE } from '../keys';
import { REGISTRY } from '../registry';

import { drawnVersion } from './drawn';
import { effectIds } from './ids';
import type { EffectId } from './ids';
import { MANIFEST_FILE, emptyManifest, parseManifest } from './manifest';
import type { Manifest, Sheet } from './manifest';
import type { PlayOptions, Stop } from './types';

const MANIFEST_KEY = 'effects-manifest';
/** An effect's sheet: its texture and its animation. */
const sheetKey = (id: EffectId): string => `fx:${id}`;
const soundKey = (id: EffectId): string => `fx-sound:${id}`;

const UNTIMED_MS = 300; // how long a sheet plays for an effect with no beats: a hit's spark, the cancel hint

const logged = new Set<string>();
function warnOnce(message: string): void {
  if (!logged.has(message)) {
    logged.add(message);
    console.warn(`finga: ${message}`);
  }
}

export function play(scene: Phaser.Scene, id: EffectId, o: PlayOptions): Stop {
  const now = scene.time.now;
  const entry = (scene.registry.get(REGISTRY.effects) as Manifest | undefined)?.entries.get(id);
  if (entry?.sound !== undefined) {
    if (scene.cache.audio.exists(soundKey(id))) {
      scene.sound.play(soundKey(id));
    } else {
      warnOnce(`effect ${id}: ${entry.sound} did not load, so it plays silent`);
    }
  }
  if (entry?.sheet) {
    if (scene.textures.exists(sheetKey(id)) && scene.anims.exists(sheetKey(id))) {
      return playSheet(scene, id, entry.sheet, o, now);
    }
    warnOnce(`effect ${id}: ${entry.sheet.file} did not load, so its drawn version plays`);
  }
  const drawn = drawnVersion(id);
  if (!drawn) {
    warnOnce(`effect ${id} has no drawn version`);
    return () => undefined;
  }
  return drawn(scene, o, now);
}

/** The sheet's animation once over the effect's time: to the cancel point with beats, else a moment. */
function playSheet(
  scene: Phaser.Scene,
  id: EffectId,
  sheet: Sheet,
  o: PlayOptions,
  now: number,
): Stop {
  const sprite = scene.add
    .sprite(o.x, o.y, sheetKey(id))
    .setOrigin(sheet.anchor[0], sheet.anchor[1])
    .setScale(ART_SCALE)
    .setFlipX(o.facing < 0)
    .setDepth(DEPTH.effect);
  const stop = () => sprite.destroy();
  sprite.once('animationcomplete', stop);
  sprite.play({
    key: sheetKey(id),
    duration: o.beats ? Math.max(1, o.beats.cancelAt - now) : UNTIMED_MS,
  });
  return stop;
}

/** Fetches the manifest with the other assets; once it's in, its sheets and sounds join the load. */
export function loadEffects(load: Phaser.Loader.LoaderPlugin, done: (m: Manifest) => void): void {
  load.json(MANIFEST_KEY, MANIFEST_FILE);
  load.once(`filecomplete-json-${MANIFEST_KEY}`, (_key: string, _type: string, raw: unknown) => {
    const m = parseManifest(raw, effectIds);
    for (const p of m.problems) {
      console.warn(`finga: effects manifest: ${p}`);
    }
    for (const [id, e] of m.entries) {
      if (e.sheet) {
        load.spritesheet(sheetKey(id), `assets/${e.sheet.file}`, {
          frameWidth: e.sheet.frameWidth,
          frameHeight: e.sheet.frameHeight,
        });
      }
      if (e.sound !== undefined) {
        load.audio(soundKey(id), `assets/${e.sound}`);
      }
    }
    done(m);
  });
}

/** Animates each sheet that loaded and hands play the manifest, less sheets it couldn't animate; with none read, all draw. */
export function readyEffects(scene: Phaser.Scene, m: Manifest | null): void {
  const manifest = m ?? emptyManifest;
  const entries = new Map(manifest.entries);
  for (const [id, e] of manifest.entries) {
    if (e.sheet && !animated(scene, id, e.sheet)) {
      if (e.sound === undefined) {
        entries.delete(id);
      } else {
        entries.set(id, { sound: e.sound });
      }
    }
  }
  scene.registry.set(REGISTRY.effects, { entries, problems: manifest.problems });
}

/** Whether a sheet that loaded has its animation now; one with fewer frames than listed, or that Phaser refuses, is left to the drawn version. */
function animated(scene: Phaser.Scene, id: EffectId, sheet: Sheet): boolean {
  const key = sheetKey(id);
  if (!scene.textures.exists(key) || scene.anims.exists(key)) {
    return true; // one that didn't load falls back as it plays, and says so then
  }
  const held = scene.textures.get(key).frameTotal - 1; // a texture counts its whole image as a frame too
  if (sheet.frames > held) {
    warnOnce(
      `effect ${id}: ${sheet.file} holds ${held} frames, not ${sheet.frames}, so its drawn version plays`,
    );
    return false;
  }
  let why = '';
  try {
    scene.anims.create({
      key,
      frames: scene.anims.generateFrameNumbers(key, { start: 0, end: sheet.frames - 1 }),
      frameRate: sheet.fps,
    });
  } catch (err) {
    why = ` (${String(err)})`;
  }
  if (!scene.anims.exists(key)) {
    warnOnce(`effect ${id}: ${sheet.file} can't be animated${why}, so its drawn version plays`);
    return false;
  }
  return true;
}
