// A sea's bubbles (underground-and-underwater.md section 7): streams from its vents, and a few from each near body's head every 3–5 s.
import { RawTexture } from '@babylonjs/core/Materials/Textures/rawTexture';
import { Texture } from '@babylonjs/core/Materials/Textures/texture';
import { Color4 } from '@babylonjs/core/Maths/math.color';
import { Vector3 } from '@babylonjs/core/Maths/math.vector';
import { ParticleSystem } from '@babylonjs/core/Particles/particleSystem';
import type { Scene } from '@babylonjs/core/scene';

const VENT_RATE = 5; // bubbles a second from each vent streaming
const VENT_REACH_M = 40; // vents this near the camera's target stream; the fog hides those past it
const VENT_SPREAD_M = 0.08; // how far from a vent's mouth a bubble starts
const VENTS_MAX = 64; // the most vents a frame streams from, so the system's room is known
const HEAD_REACH_M = 25; // bodies this near the camera's target breathe out bubbles
const BREATH_MS = [3000, 5000] as const; // the wait between a body's breaths
const BREATH = [2, 4] as const; // bubbles a breath
const HEAD_SPREAD_M = 0.12;
const BREATHS_MAX = 256; // the most bubbles from heads at once, and in a frame's breaths
const SIZE_M = [0.05, 0.12] as const;
const SPEED_M_S = [0.5, 0.9] as const; // up as it leaves
const BUOYANCY = 0.35; // and faster each second, in metres a second
const LIFE_S = [3, 4.5] as const;
const BUBBLE = new Color4(0.85, 0.97, 1, 0.7); // pale cyan, a little see-through
const RING_PX = 32;

/** A white ring with a bright fleck at its upper left, see-through inside: a bubble, for particles. */
function ringTexture(scene: Scene): RawTexture {
  const px = new Uint8Array(RING_PX * RING_PX * 4);
  const mid = (RING_PX - 1) / 2;
  for (let y = 0; y < RING_PX; y++) {
    for (let x = 0; x < RING_PX; x++) {
      const r = Math.hypot(x - mid, y - mid) / mid;
      const rim = Math.max(0, 1 - Math.abs(r - 0.82) / 0.16);
      const fleck = Math.max(0, 1 - Math.hypot(x - mid * 0.6, y - mid * 0.6) / (mid * 0.22));
      const i = (y * RING_PX + x) * 4;
      px.fill(255, i, i + 3);
      px[i + 3] = r > 1 ? 0 : Math.round(255 * Math.min(1, Math.max(rim, fleck, 0.12)));
    }
  }
  return RawTexture.CreateRGBATexture(
    px,
    RING_PX,
    RING_PX,
    scene,
    false,
    false,
    Texture.BILINEAR_SAMPLINGMODE,
  );
}

const between = (r: readonly [number, number]) => r[0] + Math.random() * (r[1] - r[0]);

/** A sea's bubbles, from its vents and its bodies' heads near the camera's target. */
export class SeaBubbles {
  private readonly ring: RawTexture;
  private readonly fromVents: ParticleSystem;
  private readonly fromHeads: ParticleSystem;
  private readonly near: Vector3[] = []; // the vents streaming this frame, its first nearCount
  private nearCount = 0;
  private readonly due = new WeakMap<object, number>(); // each body's next breath, in ms
  private readonly breaths = Array.from({ length: BREATHS_MAX }, () => new Vector3()); // where this frame's breath bubbles start, its first queued
  private queued = 0;
  private taken = 0; // of those, how many the heads' system has started

  constructor(scene: Scene) {
    this.ring = ringTexture(scene);
    this.fromVents = this.system(
      'vent bubbles',
      scene,
      Math.ceil(VENT_RATE * VENTS_MAX * LIFE_S[1]),
    );
    this.fromVents.startPositionFunction = (_, at) => {
      const v =
        this.nearCount > 0 ? this.near[Math.floor(Math.random() * this.nearCount)] : undefined;
      if (v) {
        at.set(
          v.x + (Math.random() - 0.5) * VENT_SPREAD_M,
          v.y,
          v.z + (Math.random() - 0.5) * VENT_SPREAD_M,
        );
      }
    };
    this.fromVents.emitRate = 0;
    this.fromHeads = this.system('breath bubbles', scene, BREATHS_MAX);
    this.fromHeads.manualEmitCount = 0;
    this.fromHeads.startPositionFunction = (_, at) => {
      const from = this.taken < this.queued ? this.breaths[this.taken++] : undefined;
      if (from) {
        at.set(
          from.x + (Math.random() - 0.5) * HEAD_SPREAD_M,
          from.y,
          from.z + (Math.random() - 0.5) * HEAD_SPREAD_M,
        );
      }
    };
  }

  /** A system of bubbles rising and slowing, at most capacity at once; Babylon calls back for each one's start. */
  private system(name: string, scene: Scene, capacity: number): ParticleSystem {
    const ps = new ParticleSystem(name, capacity, scene);
    ps.particleTexture = this.ring;
    ps.emitter = Vector3.Zero(); // each bubble's start is set by its source
    ps.updateSpeed = 1 / 60; // ages in seconds at any frame rate
    ps.minSize = SIZE_M[0];
    ps.maxSize = SIZE_M[1];
    ps.minEmitPower = SPEED_M_S[0];
    ps.maxEmitPower = SPEED_M_S[1];
    ps.direction1 = new Vector3(-0.08, 1, -0.08); // up, wobbling a little
    ps.direction2 = new Vector3(0.08, 1, 0.08);
    ps.gravity = new Vector3(0, BUOYANCY, 0);
    ps.minLifeTime = LIFE_S[0];
    ps.maxLifeTime = LIFE_S[1];
    ps.color1 = BUBBLE;
    ps.color2 = BUBBLE;
    ps.colorDead = new Color4(BUBBLE.r, BUBBLE.g, BUBBLE.b, 0);
    ps.blendMode = ParticleSystem.BLENDMODE_STANDARD; // fogged into the water like what's behind it
    ps.start();
    return ps;
  }

  /** Starts a frame round the camera's target at (x, z): drawn vents within VENT_REACH_M stream, and last frame's breaths the system didn't take are dropped. */
  frame(x: number, z: number, vents: readonly Vector3[]): void {
    let n = 0;
    for (const v of vents) {
      if (n < VENTS_MAX && Math.hypot(v.x - x, v.z - z) <= VENT_REACH_M) {
        this.near[n++] = v;
      }
    }
    this.nearCount = n;
    this.fromVents.emitRate = VENT_RATE * n;
    this.queued = 0;
    this.taken = 0;
    this.fromHeads.manualEmitCount = 0;
  }

  /** A body keyed by key, head at (x, y, z), now in ms: within HEAD_REACH_M of (cx, cz) it breathes a few bubbles every 3–5 s, the first at a moment of its own. */
  breathe(key: object, x: number, y: number, z: number, cx: number, cz: number, now: number): void {
    if (Math.hypot(x - cx, z - cz) > HEAD_REACH_M) {
      return;
    }
    const due = this.due.get(key);
    if (due === undefined) {
      this.due.set(key, now + Math.random() * BREATH_MS[1]);
      return;
    }
    if (now < due) {
      return;
    }
    const n = Math.round(between(BREATH));
    if (this.queued + n > BREATHS_MAX) {
      return; // a crowd's breaths wait their turn
    }
    for (let i = 0; i < n; i++) {
      this.breaths[this.queued++]!.set(x, y, z);
    }
    this.fromHeads.manualEmitCount = this.queued;
    this.due.set(key, now + between(BREATH_MS));
  }
}
