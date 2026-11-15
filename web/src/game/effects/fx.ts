// One map's effects (combat-effects.md section 3): each play by id from its manifest art where that loaded, else its drawn version; the plays stepped each frame on the world's clock, and the materials and textures they share.
import { Constants } from '@babylonjs/core/Engines/constants';
import { StandardMaterial } from '@babylonjs/core/Materials/standardMaterial';
import { RawTexture } from '@babylonjs/core/Materials/Textures/rawTexture';
import { Texture } from '@babylonjs/core/Materials/Textures/texture';
import { Color3 } from '@babylonjs/core/Maths/math.color';
import type { Scene } from '@babylonjs/core/scene';
import { Sprite } from '@babylonjs/core/Sprites/sprite';
import { SpriteManager } from '@babylonjs/core/Sprites/spriteManager';
import '@babylonjs/core/Sprites/spriteSceneComponent';

import type { WorldMap } from '../world3d/map';

import { drawnVersion } from './drawn';
import { effectIds } from './ids';
import type { EffectId } from './ids';
import { emptyManifest, MANIFEST_FILE, parseManifest } from './manifest';
import type { Manifest, Sheet } from './manifest';
import type { PlayOptions, Stop } from './types';
import { warnOnce } from './warn';

const UNTIMED_MS = 300; // how long a sheet plays for an effect with no beats: a hit's spark, the cancel hint
const SHEET_PX_PER_M = 128; // 2D's art scale of 0.25 at 32 px a metre
const SPRITES_PER_SHEET = 16;
const DOT_PX = 32;

/** The page's effects art: the manifest's usable entries, and a listed sound's player once it loaded. */
export interface Art {
  readonly manifest: Manifest;
  sound(id: EffectId): void;
}

let pageArt: Promise<Art> | null = null;

/** The manifest and its sounds, read once a page; one that can't be read leaves every effect drawn. */
export function loadArt(): Promise<Art> {
  pageArt ??= readManifest().then(withSounds);
  return pageArt;
}

async function readManifest(): Promise<Manifest> {
  try {
    const res = await fetch(`${import.meta.env.BASE_URL}${MANIFEST_FILE}`);
    if (!res.ok) {
      throw new Error(String(res.status));
    }
    const m = parseManifest(await res.json(), effectIds);
    for (const p of m.problems) {
      warnOnce(`effects manifest: ${p}`);
    }
    return m;
  } catch (err) {
    warnOnce(`effects manifest didn't load (${String(err)}), so every effect is drawn`);
    return emptyManifest;
  }
}

/** The manifest with its sounds loading: Babylon's audio engine comes only with the first listed sound, and a sound plays once it's in. */
function withSounds(manifest: Manifest): Art {
  const sounds = new Map<EffectId, { play(): void }>();
  const listed = [...manifest.entries].flatMap(([id, e]) =>
    e.sound === undefined ? [] : [[id, e.sound] as const],
  );
  if (listed.length > 0) {
    void import('./sounds')
      .then(({ loadSounds }) => loadSounds(listed, sounds))
      .catch((err: unknown) => warnOnce(`effect sounds didn't load (${String(err)})`));
  }
  return {
    manifest,
    sound: (id) => {
      const s = sounds.get(id);
      if (s) {
        s.play();
      } else if (manifest.entries.get(id)?.sound !== undefined) {
        warnOnce(
          `effect ${id}: ${manifest.entries.get(id)?.sound} isn't loaded, so it plays silent`,
        );
      }
    },
  };
}

interface Running {
  readonly until: number;
  readonly step: (now: number) => void;
  stop: Stop;
}

export class Fx {
  private readonly running = new Set<Running>();
  private readonly materials = new Map<string, StandardMaterial>();
  private readonly sheets = new Map<EffectId, SpriteManager>();
  private dotTexture: RawTexture | null = null;

  constructor(
    readonly scene: Scene,
    readonly map: WorldMap,
    private readonly art: Art,
    readonly now: () => number,
  ) {
    for (const [id, e] of art.manifest.entries) {
      if (e.sheet) {
        this.sheets.set(id, sheetManager(scene, id, e.sheet));
      }
    }
  }

  /** Plays effect id: its sound if listed, its sheet where it loaded, else its drawn version. */
  play(id: EffectId, o: PlayOptions): Stop {
    const now = this.now();
    const entry = this.art.manifest.entries.get(id);
    if (entry?.sound !== undefined) {
      this.art.sound(id);
    }
    const stop = entry?.sheet && this.playSheet(id, entry.sheet, o, now);
    if (stop) {
      return stop;
    }
    const drawn = drawnVersion(id);
    if (!drawn) {
      warnOnce(`effect ${id} has no drawn version`);
      return () => undefined;
    }
    return drawn(this, o, now);
  }

  /** Runs step(now) each frame until `until`, then end(); stop ends it sooner, as does the map's end. */
  everyFrame(now: number, until: number, step: (now: number) => void, end: () => void): Stop {
    const r: Running = {
      until,
      step,
      stop: () => {
        if (this.running.delete(r)) {
          end();
        }
      },
    };
    this.running.add(r);
    if (now >= until) {
      r.stop();
    } else {
      step(now);
    }
    return () => r.stop();
  }

  update(now: number): void {
    for (const r of [...this.running]) {
      if (now >= r.until) {
        r.stop();
      } else {
        r.step(now);
      }
    }
  }

  /** An unlit material in color, 0xrrggbb, added to what's behind it or drawn over it; every effect mesh of it shares it and fades by its own visibility. */
  material(color: number, blend: 'add' | 'over'): StandardMaterial {
    const key = `${color} ${blend}`;
    let m = this.materials.get(key);
    if (!m) {
      m = new StandardMaterial(`fx ${key}`, this.scene);
      m.disableLighting = true;
      m.emissiveColor = Color3.FromHexString(`#${color.toString(16).padStart(6, '0')}`);
      m.backFaceCulling = false;
      m.fogEnabled = false;
      m.alpha = 0.999; // blended always, so a mesh's visibility fades it
      m.alphaMode = blend === 'add' ? Constants.ALPHA_ADD : Constants.ALPHA_COMBINE;
      this.materials.set(key, m);
    }
    return m;
  }

  /** A soft white dot, for particles. */
  get dot(): Texture {
    this.dotTexture ??= dotTexture(this.scene);
    return this.dotTexture;
  }

  /** Ends every play, as the map does. */
  dispose(): void {
    for (const r of [...this.running]) {
      r.stop();
    }
    for (const s of this.sheets.values()) {
      s.dispose();
    }
    this.sheets.clear();
    for (const m of this.materials.values()) {
      m.dispose();
    }
    this.materials.clear();
    this.dotTexture?.dispose();
    this.dotTexture = null;
  }

  /** The sheet's animation once over the effect's time, a billboard at its point; undefined when the sheet can't play, which says why once. */
  private playSheet(id: EffectId, sheet: Sheet, o: PlayOptions, now: number): Stop | undefined {
    const manager = this.sheets.get(id);
    const texture = manager?.texture;
    if (!manager || !texture?.isReady()) {
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
    const s = new Sprite(id, manager);
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
}

function sheetManager(scene: Scene, id: EffectId, sheet: Sheet): SpriteManager {
  return new SpriteManager(
    `fx:${id}`,
    `${import.meta.env.BASE_URL}assets/${sheet.file}`,
    SPRITES_PER_SHEET,
    { width: sheet.frameWidth, height: sheet.frameHeight },
    scene,
  );
}

/** A white dot fading from its middle out, for particles. */
function dotTexture(scene: Scene): RawTexture {
  const px = new Uint8Array(DOT_PX * DOT_PX * 4);
  const mid = (DOT_PX - 1) / 2;
  for (let y = 0; y < DOT_PX; y++) {
    for (let x = 0; x < DOT_PX; x++) {
      const r = Math.min(1, Math.hypot(x - mid, y - mid) / mid);
      const i = (y * DOT_PX + x) * 4;
      px.fill(255, i, i + 3);
      px[i + 3] = Math.round(255 * (1 - r) ** 2);
    }
  }
  return RawTexture.CreateRGBATexture(
    px,
    DOT_PX,
    DOT_PX,
    scene,
    false,
    false,
    Texture.BILINEAR_SAMPLINGMODE,
  );
}
