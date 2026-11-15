// Particles: a fire arrow's burst, and what streams off a look as it goes: an orb's sparkles, a flame, a heal's rise, dust behind a stone or a charge.
import { Color4 } from '@babylonjs/core/Maths/math.color';
import { Vector3 } from '@babylonjs/core/Maths/math.vector';
import type { AbstractMesh } from '@babylonjs/core/Meshes/abstractMesh';
import { SphereParticleEmitter } from '@babylonjs/core/Particles/EmitterTypes/sphereParticleEmitter';
import { ParticleSystem } from '@babylonjs/core/Particles/particleSystem';

import type { Fx } from './fx';
import type { Point, Stop } from './types';

export interface Burst {
  readonly count: number;
  /** 0xrrggbb as it starts and as it dies. */
  readonly color: number;
  readonly fade?: number;
  /** Metres across, from and to. */
  readonly size: readonly [number, number];
  /** Metres a second outward, from and to. */
  readonly speed: readonly [number, number];
  /** Seconds each lives, from and to. */
  readonly life: readonly [number, number];
  /** Where they start: within this many metres of the point. */
  readonly spread?: number;
  /** Metres a second each second, up positive: a puff rises. */
  readonly rise?: number;
  readonly blend: 'add' | 'over';
}

const color4 = (hex: number, a: number) =>
  new Color4(((hex >> 16) & 0xff) / 255, ((hex >> 8) & 0xff) / 255, (hex & 0xff) / 255, a);

/** count particles thrown out from `at` at once, gone within their life; stop takes them away sooner. A system's dispose keeps fx's shared dot, which Babylon's would take with it. */
export function burst(fx: Fx, at: Point, b: Burst): Stop {
  const ps = new ParticleSystem('burst', b.count, fx.scene);
  ps.particleTexture = fx.dot;
  ps.emitter = new Vector3(at.x, at.y, at.z);
  ps.particleEmitterType = new SphereParticleEmitter(b.spread ?? 0.05);
  ps.updateSpeed = 1 / 60; // ages in seconds at any frame rate
  ps.manualEmitCount = b.count;
  ps.minSize = b.size[0];
  ps.maxSize = b.size[1];
  ps.minEmitPower = b.speed[0];
  ps.maxEmitPower = b.speed[1];
  ps.minLifeTime = b.life[0];
  ps.maxLifeTime = b.life[1];
  ps.color1 = color4(b.color, 1);
  ps.color2 = color4(b.color, 1);
  ps.colorDead = color4(b.fade ?? b.color, 0);
  ps.gravity = new Vector3(0, b.rise ?? 0, 0);
  ps.blendMode =
    b.blend === 'add' ? ParticleSystem.BLENDMODE_ADD : ParticleSystem.BLENDMODE_STANDARD;
  ps.start();
  const now = fx.now();
  return fx.everyFrame(
    now,
    now + b.life[1] * 1000,
    () => undefined,
    () => ps.dispose(false),
  );
}

export interface Stream {
  /** Particles a second. */
  readonly rate: number;
  readonly color: number;
  readonly size: readonly [number, number];
  readonly speed: readonly [number, number];
  readonly life: readonly [number, number];
  readonly spread?: number;
  readonly rise?: number;
  readonly blend?: 'add' | 'over';
}

/** Particles given off from `from` as it moves, a mesh or a point, until the returned stop takes them away. */
export function stream(fx: Fx, from: AbstractMesh | Vector3, s: Stream): Stop {
  const ps = new ParticleSystem('stream', Math.ceil(s.rate * s.life[1]) + 4, fx.scene);
  ps.particleTexture = fx.dot;
  ps.emitter = from;
  ps.particleEmitterType = new SphereParticleEmitter(s.spread ?? 0.05);
  ps.updateSpeed = 1 / 60;
  ps.emitRate = s.rate;
  ps.minSize = s.size[0];
  ps.maxSize = s.size[1];
  ps.minEmitPower = s.speed[0];
  ps.maxEmitPower = s.speed[1];
  ps.minLifeTime = s.life[0];
  ps.maxLifeTime = s.life[1];
  ps.color1 = color4(s.color, 1);
  ps.color2 = color4(s.color, 0.8);
  ps.colorDead = color4(s.color, 0);
  ps.gravity = new Vector3(0, s.rise ?? 0, 0);
  ps.blendMode =
    (s.blend ?? 'add') === 'add' ? ParticleSystem.BLENDMODE_ADD : ParticleSystem.BLENDMODE_STANDARD;
  ps.start();
  let live = true;
  return () => {
    if (live) {
      live = false;
      ps.dispose(false);
    }
  };
}
