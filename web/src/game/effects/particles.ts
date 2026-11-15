// Short particle bursts: a hit's spark, a miss's puff, a fire arrow's burst, a dust trail.
import { Color4 } from '@babylonjs/core/Maths/math.color';
import { Vector3 } from '@babylonjs/core/Maths/math.vector';
import { SphereParticleEmitter } from '@babylonjs/core/Particles/EmitterTypes/sphereParticleEmitter';
import { ParticleSystem } from '@babylonjs/core/Particles/particleSystem';
import '@babylonjs/core/Particles/particleSystemComponent';

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

/** count particles thrown out from `at` at once, gone within their life; stop takes them away sooner. */
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
  ps.targetStopDuration = b.life[1];
  ps.disposeOnStop = true;
  ps.start();
  let live = true;
  return () => {
    if (live) {
      live = false;
      ps.dispose();
    }
  };
}
