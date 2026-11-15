// One map's effects (combat-effects.md section 3): each play by id from its manifest art where that loaded, else its drawn version; the plays stepped each frame on the world's clock, and the materials, textures and shapes they share.
import { Constants } from '@babylonjs/core/Engines/constants';
import { StandardMaterial } from '@babylonjs/core/Materials/standardMaterial';
import { RawTexture } from '@babylonjs/core/Materials/Textures/rawTexture';
import { Texture } from '@babylonjs/core/Materials/Textures/texture';
import { Color3 } from '@babylonjs/core/Maths/math.color';
import { Mesh } from '@babylonjs/core/Meshes/mesh';
import type { Scene } from '@babylonjs/core/scene';

import type { WorldMap } from '../world3d/map';

import { drawnVersion } from './drawn';
import { effectIds } from './ids';
import type { EffectId } from './ids';
import { emptyManifest, MANIFEST_FILE, parseManifest } from './manifest';
import type { Manifest } from './manifest';
import type { Sheets } from './sheets';
import type { PlayOptions, Stop } from './types';
import { warnOnce } from './warn';

const DOT_PX = 32;

/** The page's effects art: the manifest's usable entries, the sheets' player if it lists any, and a listed sound's player once it loaded. */
export interface Art {
  readonly manifest: Manifest;
  readonly sheets?: typeof Sheets | undefined;
  sound(id: EffectId): void;
}

let pageArt: Promise<Art> | null = null;

/** The manifest, its sheets' player and its sounds, read once a page; one that can't be read leaves every effect drawn. */
export function loadArt(): Promise<Art> {
  pageArt ??= readManifest().then(async (m) => withSounds(m, await sheetsFor(m)));
  return pageArt;
}

/** Babylon's sprites, loaded only for a manifest that lists a sheet. */
async function sheetsFor(m: Manifest): Promise<typeof Sheets | undefined> {
  if (![...m.entries.values()].some((e) => e.sheet)) {
    return undefined;
  }
  try {
    return (await import('./sheets')).Sheets;
  } catch (err) {
    warnOnce(`effect sheets didn't load (${String(err)}), so every effect is drawn`);
    return undefined;
  }
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
function withSounds(manifest: Manifest, sheets: typeof Sheets | undefined): Art {
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
    sheets,
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

/** How an effect's mesh meets what's behind it: added, drawn over, or laid on the ground as a decal, over it or tinting it. */
export type Blend = 'add' | 'over' | 'ground' | 'tint';

const MODES: Record<Blend, number> = {
  add: Constants.ALPHA_ADD,
  over: Constants.ALPHA_COMBINE,
  ground: Constants.ALPHA_COMBINE,
  tint: Constants.ALPHA_MULTIPLY, // the ground times its colour, so it keeps its shading
};

interface Running {
  readonly until: number;
  readonly step: (now: number) => void;
  stop: Stop;
}

export class Fx {
  private readonly running = new Set<Running>();
  private readonly materials = new Map<string, StandardMaterial>();
  private readonly shapes = new Map<string, { readonly name: string; readonly mesh: Mesh }>();
  private readonly sheets: Sheets | null;
  private dotTexture: RawTexture | null = null;

  constructor(
    readonly scene: Scene,
    readonly map: WorldMap,
    private readonly art: Art,
    readonly now: () => number,
  ) {
    this.sheets = art.sheets ? new art.sheets(scene, art.manifest.entries) : null;
  }

  /** Plays effect id: its sound if listed, its sheet where it loaded, else its drawn version. */
  play(id: EffectId, o: PlayOptions): Stop {
    const now = this.now();
    const entry = this.art.manifest.entries.get(id);
    if (entry?.sound !== undefined) {
      this.art.sound(id);
    }
    const stop = entry?.sheet && this.sheets?.play(id, entry.sheet, o, now);
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

  /** An unlit material in color, 0xrrggbb, added to what's behind it, drawn over it, or laid on the ground; every effect mesh of it shares it and fades by its own visibility, but a tint shows whole while visible. */
  material(color: number, blend: Blend): StandardMaterial {
    const key = `${color} ${blend}`;
    let m = this.materials.get(key);
    if (!m) {
      m = new StandardMaterial(`fx ${key}`, this.scene);
      m.disableLighting = true;
      m.emissiveColor = Color3.FromHexString(`#${color.toString(16).padStart(6, '0')}`);
      m.backFaceCulling = false;
      m.fogEnabled = false;
      m.alpha = 0.999; // blended always, so a mesh's visibility fades it
      m.alphaMode = MODES[blend];
      if (blend === 'ground' || blend === 'tint') {
        m.disableDepthWrite = true; // a decal: under whatever stands on it
        m.zOffset = -2;
      }
      this.materials.set(key, m);
    }
    return m;
  }

  /** A new mesh in the shape kept under key: make builds it the first time, and it stays hidden till the map ends, each copy sharing its geometry. */
  shape(key: string, make: () => Mesh): Mesh {
    let kept = this.shapes.get(key);
    if (!kept) {
      const mesh = make();
      kept = { name: mesh.name, mesh };
      mesh.name = `kept ${key}`;
      mesh.setEnabled(false);
      this.shapes.set(key, kept);
    }
    const m = new Mesh(kept.name, this.scene);
    kept.mesh.geometry?.applyToMesh(m);
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
    this.sheets?.dispose();
    for (const m of this.materials.values()) {
      m.dispose();
    }
    this.materials.clear();
    for (const k of this.shapes.values()) {
      k.mesh.dispose();
    }
    this.shapes.clear();
    this.dotTexture?.dispose();
    this.dotTexture = null;
  }
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
