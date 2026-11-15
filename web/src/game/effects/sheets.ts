// The effects' sprite sheets (combat-effects.md section 3): each listed sheet played as a billboard animation; Babylon's sprites come with the page's art only when the manifest lists a sheet.
import type { Scene } from '@babylonjs/core/scene';
import { Sprite } from '@babylonjs/core/Sprites/sprite';
import { SpriteManager } from '@babylonjs/core/Sprites/spriteManager';
import '@babylonjs/core/Sprites/spriteSceneComponent';

import type { EffectId } from './ids';
import type { Manifest, Sheet } from './manifest';
import type { PlayOptions, Stop } from './types';
import { warnOnce } from './warn';

const UNTIMED_MS = 300; // how long a sheet plays for an effect with no beats: a hit's spark, the cancel hint
const SHEET_PX_PER_M = 128; // 2D's art scale of 0.25 at 32 px a metre
const SPRITES_PER_SHEET = 16;

/** One map's sheets, each loading into its scene as the map starts. */
export class Sheets {
  private readonly managers = new Map<EffectId, SpriteManager>();

  constructor(scene: Scene, entries: Manifest['entries']) {
    for (const [id, e] of entries) {
      if (e.sheet) {
        this.managers.set(id, manager(scene, id, e.sheet));
      }
    }
  }

  /** The sheet's animation once over the effect's time, a billboard at its point; undefined when the sheet can't play, which says why once. */
  play(id: EffectId, sheet: Sheet, o: PlayOptions, now: number): Stop | undefined {
    const m = this.managers.get(id);
    const texture = m?.texture;
    if (!m || !texture?.isReady()) {
      if (texture?.loadingError) {
        warnOnce(`effect ${id}: ${sheet.file} did not load, so its drawn version plays`);
      }
      return undefined; // still loading: drawn this once
    }
    const { width, height } = texture.getSize();
    const held = Math.floor(width / sheet.frameWidth) * Math.floor(height / sheet.frameHeight);
    if (sheet.frames > held) {
      warnOnce(
        `effect ${id}: ${sheet.file} holds ${held} frames, not ${sheet.frames}, so its drawn version plays`,
      );
      return undefined;
    }
    const w = sheet.frameWidth / SHEET_PX_PER_M;
    const h = sheet.frameHeight / SHEET_PX_PER_M;
    const s = new Sprite(id, m);
    s.width = w;
    s.height = h;
    // The camera looks north, so screen right is east: the anchor shifts the billboard along x and y.
    s.position.set(o.x + (0.5 - sheet.anchor[0]) * w, o.y + (sheet.anchor[1] - 0.5) * h, o.z);
    s.invertU = Math.sin(o.yaw) < 0; // facing west, mirrored as 2D flipped it
    let live = true;
    const stop = () => {
      if (live) {
        live = false;
        s.dispose();
      }
    };
    const ms = o.beats ? Math.max(1, o.beats.readyAt - now) : UNTIMED_MS;
    s.playAnimation(0, sheet.frames - 1, false, ms / sheet.frames, stop);
    return stop;
  }

  dispose(): void {
    for (const m of this.managers.values()) {
      m.dispose();
    }
    this.managers.clear();
  }
}

function manager(scene: Scene, id: EffectId, sheet: Sheet): SpriteManager {
  return new SpriteManager(
    `fx:${id}`,
    `${import.meta.env.BASE_URL}assets/${sheet.file}`,
    SPRITES_PER_SHEET,
    { width: sheet.frameWidth, height: sheet.frameHeight },
    scene,
  );
}
