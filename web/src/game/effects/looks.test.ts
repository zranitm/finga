import { VertexBuffer } from '@babylonjs/core/Buffers/buffer';
import { FreeCamera } from '@babylonjs/core/Cameras/freeCamera';
import { Constants } from '@babylonjs/core/Engines/constants';
import { NullEngine } from '@babylonjs/core/Engines/nullEngine';
import type { InputBlock } from '@babylonjs/core/Materials/Node/Blocks/Input/inputBlock';
import type { NodeMaterial } from '@babylonjs/core/Materials/Node/nodeMaterial';
import type { StandardMaterial } from '@babylonjs/core/Materials/standardMaterial';
import type { Color3 } from '@babylonjs/core/Maths/math.color';
import { Vector3 } from '@babylonjs/core/Maths/math.vector';
import { Mesh } from '@babylonjs/core/Meshes/mesh';
import type { CylinderParticleEmitter } from '@babylonjs/core/Particles/EmitterTypes/cylinderParticleEmitter';
import { Scene } from '@babylonjs/core/scene';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { skillById } from '@/progression/skills';
import type { Skill } from '@/progression/skills';
import { servePublic } from '@/testing/public-on-disk';

import type { Beats } from '../world3d/beats';
import { LockRing } from '../world3d/lock-ring';
import { loadMap } from '../world3d/map';
import type { WorldMap } from '../world3d/map';
import { GHOST } from '../world3d/see-through';
import { Palette } from '../world3d/toon';

import { drawnVersion } from './drawn';
import { Fx } from './fx';
import { effectIds } from './ids';
import type { EffectId } from './ids';
import { looks as looksOf } from './looks';
import { emptyManifest } from './manifest';
import type { Body, PlayOptions, Point } from './types';

const BEATS: Beats = { start: 1000, hit: 1000, strikeEnd: 1150, readyAt: 1450, end: 1500 };
const CASTER: Body = { x: 10, y: 0, z: 10, radius: 0.375, height: 1.75 };
const FOES: Body[] = [
  { x: 13, y: 0, z: 10.5, radius: 0.5, height: 0.875 },
  { x: 14, y: 0, z: 9, radius: 0.5, height: 0.875 },
  { x: 12, y: 0, z: 11.5, radius: 0.5, height: 0.875 },
];
const EAST = Math.PI / 2;

let engine: NullEngine;
let scene: Scene;
let map: WorldMap;
let palette: Palette;
let fx: Fx;
let now = 0;

const looks = effectIds.filter((id) => id.startsWith('attack.') || id.startsWith('skill.'));

/** What a scene holds that a look makes, past the shapes fx keeps for the map. */
const made = () => [
  scene.meshes.filter((m) => !m.name.startsWith('kept ')).length,
  scene.transformNodes.length,
  scene.particleSystems.length,
];

const at = (o: Partial<PlayOptions> = {}): PlayOptions => ({
  ...CASTER,
  yaw: EAST,
  beats: BEATS,
  targets: () => FOES,
  follow: () => CASTER,
  ...o,
});

/** Steps fx's plays to t. */
function to(t: number): void {
  now = t;
  fx.update(t);
}

const named = (name: string) => scene.meshes.filter((m) => m.name === name && m.isEnabled());

describe('looks', () => {
  beforeEach(async () => {
    engine = new NullEngine();
    scene = new Scene(engine);
    servePublic();
    now = 1000;
    map = await loadMap('forest');
    palette = new Palette(scene);
    fx = new Fx(
      scene,
      map,
      { manifest: emptyManifest, sound: () => undefined },
      () => now,
      palette,
    );
  });

  afterEach(() => {
    fx.dispose();
    scene.dispose();
    engine.dispose();
  });

  it('names every attack and active skill, each with a drawn version to fall back to', () => {
    expect(effectIds).toContain('attack.warrior');
    expect(effectIds).toContain('skill.spark');
    expect(effectIds).not.toContain('skill.second_wind'); // passive: nothing to see
    expect(effectIds.filter((id) => !drawnVersion(id))).toEqual([]);
  });

  it.each(looks)('%s draws through its strike, and stop takes all it made away', (id: EffectId) => {
    const before = made();
    const stop = fx.play(id, at());
    to(BEATS.hit + 60);
    expect(made().some((n, i) => n > before[i]!)).toBe(true);
    stop();
    stop();
    expect(made()).toEqual(before);
  });

  it.each(looks)(
    '%s ends on its own once its shot has flown and its beats are done',
    (id: EffectId) => {
      const before = made();
      fx.play(id, at());
      for (let t = BEATS.hit; t <= BEATS.end + 600; t += 16) {
        to(t);
      }
      expect(made()).toEqual(before);
    },
  );

  it('leaves the particles’ shared texture to the next look when one ends or is stopped', () => {
    fx.play('skill.fire_arrow', at());
    for (let t = BEATS.hit; t <= BEATS.end + 600; t += 16) {
      to(t);
    }
    const fresh = () =>
      at({
        beats: { start: now, hit: now, strikeEnd: now + 150, readyAt: now + 450, end: now + 500 },
      });
    fx.play('skill.heal', fresh())();
    fx.play('skill.heal', fresh());
    to(now + 50);
    expect(scene.particleSystems.length).toBeGreaterThan(0);
    expect(scene.particleSystems.every((p) => p.particleTexture?.getInternalTexture())).toBe(true);
  });

  it('gives heal’s sparkles off just outside the body it heals, feet to head, so the body hides few', () => {
    const healed: Body = { x: 3, y: 1, z: 4, radius: 0.375, height: 1.75 };
    fx.play('skill.heal', { ...at(), targets: () => [healed] });
    to(BEATS.hit + 50);
    const ps = scene.particleSystems.find((p) => p.name === 'stream')!;
    const e = ps.particleEmitterType as CylinderParticleEmitter;
    expect([e.radius, e.height, e.radiusRange]).toEqual([0.375 + 0.15, 1.75, 0]);
    expect(ps.emitter).toEqual(new Vector3(3, 1 + 1.75 / 2, 4)); // the body's middle
  });

  describe('a mob attack', () => {
    const SLIME: Body = { x: 20, y: 0, z: 20, radius: 0.5, height: 0.875 };
    const WINDUP: Beats = { start: 1000, hit: 1600, strikeEnd: 2000, readyAt: 2250, end: 2250 };
    const mobAt = (o: Partial<PlayOptions> = {}): PlayOptions => ({
      ...SLIME,
      yaw: EAST,
      size: SLIME,
      reach: 0.75,
      to: { x: 22, y: 0, z: 20 },
      width: 1,
      beats: WINDUP,
      follow: () => SLIME,
      shots: () => [],
      ...o,
    });
    /** How far the patches' corners reach from (x, z): the area's, then what has filled of it. */
    const reaches = (x: number, z: number) =>
      named('patch').map((m) => {
        const p = m.getVerticesData(VertexBuffer.PositionKind)!;
        let far = 0;
        for (let i = 0; i < p.length; i += 3) {
          far = Math.max(far, Math.hypot(p[i]! - x, p[i + 2]! - z));
        }
        return far;
      });
    const mobLooks = effectIds.filter((id) => id.startsWith('mob.'));
    /** A red area's look, off its decal's blocks: its colour, its alpha and how it blends. */
    const decalOf = (m: Mesh) => {
      const n = m.material as NodeMaterial;
      const alpha = n.getBlockByName('alpha') as InputBlock | null;
      return {
        color: (n.getBlockByName('color') as InputBlock).value as Color3,
        alpha: (alpha?.value as number | undefined) ?? 1,
        mode: n.alphaMode,
        faint: n.getBlockByName('cutFaint') !== null,
      };
    };

    it.each(mobLooks)(
      '%s lays its area red on the ground through the windup, held till it has acted, and stop takes all it made away',
      (id) => {
        const before = made();
        const stop = fx.play(id, mobAt());
        to(WINDUP.start + 300);
        expect(named('patch')).toHaveLength(2);
        expect(named('patch').every((m) => m.visibility > 0)).toBe(true);
        to(WINDUP.hit + 10);
        const spit = id === 'mob.spit'; // its ball is the warning once thrown
        expect(named('patch').every((m) => m.visibility > 0)).toBe(!spit);
        to(WINDUP.strikeEnd + 10);
        expect(named('patch').every((m) => m.visibility === 0)).toBe(true); // gone once it has acted
        stop();
        expect(made()).toEqual(before);
      },
    );

    it.each(mobLooks)(
      '%s edges its area in a status’s colour round its rim while it warns, and takes the edge away with it',
      (id) => {
        const before = made();
        const stop = fx.play(id, mobAt({ edge: 0x5b8cff }));
        const edge = () => named('line').filter((m) => m.visibility > 0);
        to(WINDUP.start + 300);
        expect(
          edge().map((m) => (m.material as StandardMaterial).emissiveColor.toHexString()),
        ).toEqual(['#5B8CFF']);
        const [zone] = named('patch');
        const rim = edge()[0]!;
        rim.refreshBoundingInfo({});
        zone!.refreshBoundingInfo({});
        const [r, z] = [rim, zone!].map((m) => m.getBoundingInfo().boundingBox);
        expect(Vector3.Distance(r!.minimumWorld, z!.minimumWorld)).toBeLessThan(0.15); // round the area, not in it
        expect(Vector3.Distance(r!.maximumWorld, z!.maximumWorld)).toBeLessThan(0.15);
        to(WINDUP.strikeEnd + 10);
        expect(edge()).toEqual([]); // gone with the area, once it has acted
        stop();
        expect(made()).toEqual(before);
      },
    );

    it('holds its red, full, past its hit till the world says it acted, and shows its impact from then', () => {
      const world: { said?: number } = {}; // when the world said it acted
      fx.play('mob.slam', mobAt({ beats: { ...WINDUP, end: 5000 }, landed: () => world.said }));
      const ring = () => named('ring')[0]!;
      to(WINDUP.hit + 300);
      expect(named('patch').every((m) => m.visibility > 0)).toBe(true);
      expect(reaches(SLIME.x, SLIME.z)[1]).toBeCloseTo(reaches(SLIME.x, SLIME.z)[0]!, 3); // filled
      expect(ring().visibility).toBe(0);
      world.said = WINDUP.hit + 350;
      to(WINDUP.hit + 400);
      expect(named('patch').every((m) => m.visibility === 0)).toBe(true);
      expect(ring().visibility).toBeCloseTo(1 - 50 / (WINDUP.readyAt - WINDUP.strikeEnd), 3);
      to(world.said + (WINDUP.readyAt - WINDUP.strikeEnd) + 10);
      expect(ring().visibility).toBe(0);
    });

    it('a bite’s area runs from the mob’s edge to its reach past it, filling out from the edge over its windup', () => {
      fx.play('mob.bite', mobAt());
      to(WINDUP.start + 300);
      const [zone, fill] = reaches(SLIME.x, SLIME.z);
      expect(zone).toBeCloseTo(0.5 + 0.75, 3);
      expect(fill).toBeCloseTo(0.5 + 0.75 / 2, 3);
      const p = named('patch')[0]!.getVerticesData(VertexBuffer.PositionKind)!;
      let near = Infinity;
      for (let i = 0; i < p.length; i += 3) {
        near = Math.min(near, Math.hypot(p[i]! - SLIME.x, p[i + 2]! - SLIME.z));
      }
      expect(near).toBeCloseTo(0.5, 3); // none of it under the body
    });

    it('tints the ground red under its area through the windup, so it reads red on grass, stone or sand, the fill laid over it; both faint where the place opens', () => {
      fx.play('mob.bite', mobAt());
      to(WINDUP.start + 300);
      const [zone, fill] = (named('patch') as Mesh[]).map(decalOf);
      expect(zone!.mode).toBe(Constants.ALPHA_MULTIPLY);
      const c = zone!.color;
      expect(Math.max(c.g, c.b)).toBeLessThan(c.r / 2); // the ground's green or yellow taken down
      expect([fill!.mode, fill!.alpha]).toEqual([Constants.ALPHA_COMBINE, 0.6]);
      expect([zone!.faint, fill!.faint]).toEqual([true, true]); // so neither hangs whole in front of a body in a hole
    });

    it('fills over its own area: halfway through the windup the fill’s rings are the area’s and the points halfway between them, height and all', () => {
      fx.play('mob.hop', mobAt({ reach: 0.375, to: { x: 60.5, y: 0, z: 2.5 } })); // landing on a slope
      to(WINDUP.start + 300);
      const [zone, fill] = named('patch').map((m) => [
        ...m.getVerticesData(VertexBuffer.PositionKind)!,
      ]);
      const rings = 7; // a round area's middle and 6 rings out from it
      const corner = (p: number[] | undefined, col: number, ring: number) =>
        p?.slice((col * rings + ring) * 3, (col * rings + ring) * 3 + 3);
      for (let col = 0; col <= 32; col++) {
        for (let ring = 0; ring < rings; ring++) {
          const [a, b] = [
            corner(zone, col, Math.floor(ring / 2)),
            corner(zone, col, Math.ceil(ring / 2)),
          ];
          corner(fill, col, ring)?.forEach((v, i) =>
            expect(v).toBeCloseTo(((a?.[i] ?? NaN) + (b?.[i] ?? NaN)) / 2, 5),
          );
        }
      }
      expect(new Set(zone?.filter((_, i) => i % 3 === 1)).size).toBeGreaterThan(1); // on uneven ground
    });

    it('a hop’s area is round its landing', () => {
      fx.play('mob.hop', mobAt({ reach: 0.375 }));
      to(WINDUP.start + 300);
      expect(reaches(22, 20)[0]).toBeCloseTo(0.5 + 0.375, 3);
    });

    it('a charge’s lane runs to where the server stops it, its tip at the body’s front', () => {
      fx.play('mob.charge', mobAt({ to: { x: 21.2, y: 0, z: 20 }, width: 1 })); // cut short of its distance, at a wall or a mob
      to(WINDUP.start + 300);
      expect(reaches(SLIME.x, SLIME.z)[0]).toBeCloseTo(Math.hypot(1.2 + 0.5, 0.5), 3);
    });

    it('a spit’s lane runs as far as its ball flies, its tip at the ball’s front', () => {
      fx.play('mob.spit', mobAt({ reach: 2, width: 0.4 }));
      to(WINDUP.start + 300);
      expect(reaches(SLIME.x, SLIME.z)[0]).toBeCloseTo(Math.hypot(2 + 0.2, 0.2), 3);
    });

    it('shows its warnings faintly where the place hides them: a ghost of its area and of each ball, in their shape, showing as they do', () => {
      const balls = [{ x: 21, y: 0.4, z: 20, size: 0.375 }];
      fx.play('mob.spit', mobAt({ color: 0xaa3366, shots: () => balls }));
      to(WINDUP.start + 300);
      const areas = named('patch') as Mesh[];
      const ghosts = named('patch ghost') as Mesh[];
      expect(ghosts.map((g) => g.parent)).toEqual(areas);
      expect(ghosts.map((g) => g.geometry)).toEqual(areas.map((m) => m.geometry));
      const [zone, fill] = areas.map(decalOf);
      expect(areas.map((m) => m.visibility)).toEqual([1, 1]);
      expect(ghosts.map((g) => g.visibility)).toEqual([1, fill!.alpha]); // its area's alpha, which a decal takes from its material
      for (const m of [...ghosts, ...areas]) {
        expect(m.material!.stencil.opStencilDepthPass).toBe(Constants.KEEP); // a decal or a ghost: the mark stays the ground's
      }
      const [zoneGhost, fillGhost] = ghosts.map((g) => g.material as StandardMaterial);
      expect([zoneGhost, fillGhost].map((m) => [m!.depthFunction, m!.stencil.func])).toEqual([
        [Constants.GREATER, Constants.EQUAL],
        [Constants.GREATER, Constants.EQUAL],
      ]);
      expect(zoneGhost!.alphaMode).toBe(Constants.ALPHA_MULTIPLY);
      expect(zoneGhost!.emissiveColor.g).toBeGreaterThan(zone!.color.g); // a fainter tint, as a tint takes no alpha
      expect(fillGhost!.alpha * ghosts[1]!.visibility).toBeCloseTo(fill!.alpha * GHOST, 2);
      to(WINDUP.hit + 100);
      expect(named('patch ghost').every((g) => g.visibility === 0)).toBe(true); // gone with the lane once thrown
      const [ball] = named('ball');
      expect(named('ball ghost').map((g) => g.parent)).toEqual([ball]);
      balls.length = 0;
      to(WINDUP.hit + 150);
      expect(named('ball ghost')).toHaveLength(0);
    });

    it('draws its red areas, then the lock ring, before any effect, so a swing or an impact drawn first never cuts them out', async () => {
      const camera = new FreeCamera('camera', new Vector3(SLIME.x, 12, SLIME.z - 12), scene);
      camera.setTarget(new Vector3(SLIME.x, 0, SLIME.z));
      const ring = new LockRing(scene, map, palette);
      ring.show(SLIME, now);
      fx.play('mob.bite', mobAt());
      const behind = { ...CASTER, x: SLIME.x, z: SLIME.z + 1.5 }; // farther from the camera than the area, so drawn first by distance alone
      fx.play('attack.warrior', at({ ...behind, yaw: Math.PI, follow: () => behind }));
      to(WINDUP.start + 100);
      const drawn: string[] = [];
      for (const m of scene.meshes.filter((m) => m instanceof Mesh)) {
        m.onBeforeDrawObservable.add(() => drawn.push(m.name));
      }
      await scene.whenReadyAsync();
      scene.render();
      const tier = (name: string) =>
        name.startsWith('patch') ? 0 : name.startsWith('lock ring') ? 1 : 2; // a mesh with its ghost, which never draws where it does
      const tiers = drawn.map(tier);
      expect(new Set(tiers)).toEqual(new Set([0, 1, 2])); // the areas, the ring and the swing all drawn
      expect(tiers).toEqual([...tiers].sort());
      ring.dispose();
    });

    it('a spit’s balls show while they fly, in the mob’s colour', () => {
      const balls = [{ x: 21, y: 0.4, z: 20, size: 0.375 }];
      fx.play('mob.spit', mobAt({ color: 0xaa3366, shots: () => balls }));
      to(WINDUP.hit + 100);
      const [b] = named('ball');
      expect([b?.position.x, b?.scaling.x]).toEqual([21, 0.375]);
      balls.length = 0;
      to(WINDUP.hit + 150);
      expect(named('ball')).toHaveLength(0);
    });
  });

  it('draws a status skill, which the table lacks, by its shape in the status colour: an aura for an area, a swing for an arc', () => {
    const focus = skillById('focus')!;
    const none = { base: 0, per_point: 0 };
    const statusSkill = (id: string, shape: Skill['stages'][number]['shape']): Skill => ({
      ...focus,
      id,
      effect: 'status',
      stages: [
        {
          ...focus.stages[0]!,
          shape,
          bonus: {},
          duration_ms: 0,
          status: { status: 'taunt', time_ms: 5000, power: none },
        },
      ],
    });
    const colorOfMesh = (m: { material: unknown }) =>
      (m.material as StandardMaterial).emissiveColor.toHexString();
    const lookOf = (skill: Skill) =>
      looksOf([skill]).find(([id]) => id === `skill.${skill.id}`)![1];
    lookOf(statusSkill('taunt', { kind: 'area', radius: 6 }))(fx, at(), now);
    to(BEATS.hit + 60);
    expect(named('ring').map(colorOfMesh)).toEqual(['#6B8CFF', '#6B8CFF']);
    lookOf(statusSkill('shove', { kind: 'arc', radius: 2, height: 1.75 }))(fx, at(), now);
    to(BEATS.strikeEnd);
    expect(named('band').map(colorOfMesh)).toContain('#6B8CFF');
  });

  it('draws a swing out to its arc’s radius', () => {
    fx.play('attack.warrior', at());
    to(BEATS.strikeEnd);
    const band = named('band')[0] as Mesh;
    const p = band.getVerticesData(VertexBuffer.PositionKind)!;
    let far = 0;
    for (let i = 0; i < p.length; i += 3) {
      far = Math.max(far, Math.hypot(p[i]!, p[i + 2]!));
    }
    expect(far).toBeCloseTo(0.9 * 2.375, 3);
  });

  it('flies a shot from the caster’s chest along the aim at a shot’s pace', () => {
    fx.play('attack.bowman', at());
    to(BEATS.hit + 100);
    const arrow = scene.transformNodes.find((n) => n.name === 'arrow')!;
    expect(arrow.position.x).toBeCloseTo(CASTER.x + 0.3 + 4.375, 3); // 2D's 1.4 px/ms, 100 ms on
    expect(arrow.position.z).toBeCloseTo(CASTER.z, 3);
  });

  it('draws each skill’s tiers differently: Strike cracks the ground, Volley throws three stones, Arc leaps between the foes', () => {
    const lines = () => named('line').length;
    const stopStrike = fx.play('skill.strike', at({ tier: 0 }));
    to(BEATS.strikeEnd);
    const plain = lines();
    stopStrike();
    fx.play('skill.strike', at({ tier: 2 }));
    to(BEATS.strikeEnd);
    expect(lines() - plain).toBe(1);
    fx.dispose();

    fx.play('skill.throw', at({ tier: 0 }));
    fx.play('skill.throw', at({ tier: 2 }));
    to(BEATS.hit + 30);
    expect(named('stone')).toHaveLength(4);
    fx.dispose();

    fx.play('skill.spark', at({ tier: 0 }));
    to(BEATS.hit + 30);
    const bolt = lines();
    fx.dispose();
    fx.play('skill.spark', at({ tier: 2 }));
    to(BEATS.hit + 30);
    expect([bolt, lines()]).toEqual([2, 6]); // a leg to each of three foes, each a violet line round a white one
  });
});

describe('moves', () => {
  beforeEach(async () => {
    engine = new NullEngine();
    scene = new Scene(engine);
    servePublic();
    now = 1000;
    map = await loadMap('forest');
    palette = new Palette(scene);
    fx = new Fx(
      scene,
      map,
      { manifest: emptyManifest, sound: () => undefined },
      () => now,
      palette,
    );
  });

  afterEach(() => {
    fx.dispose();
    scene.dispose();
    engine.dispose();
  });

  /** The x each enabled mesh named name spans, and its lowest and highest y. */
  const spans = (name: string) =>
    named(name).map((m) => {
      const v = m.getVerticesData(VertexBuffer.PositionKind)!;
      const xs = v.filter((_, i) => i % 3 === 0);
      const ys = v.filter((_, i) => i % 3 === 1);
      return [Math.min(...xs), Math.max(...xs), Math.min(...ys), Math.max(...ys)];
    });

  /** Plays id for a caster at where(t), stepping 16 ms frames to each of until, calling seen at each. */
  function run(
    id: EffectId,
    where: (t: number) => Point,
    until: number,
    seen: (t: number) => void,
  ): void {
    const body = { ...CASTER, ...where(BEATS.start) };
    now = BEATS.start;
    fx.play(id, at({ ...where(BEATS.start), follow: () => body }));
    for (let t = BEATS.start; t <= until; t += 16) {
      Object.assign(body, where(t));
      to(t);
      seen(t);
    }
  }

  it('a dash trails speed lines along the caster’s last 160 ms of path while it moves faster than a walk can, with its swing, and takes them away after', () => {
    const before = made();
    const x = (t: number) =>
      t < 1100 ? 10 + (5 * (t - 1000)) / 1000 : 10.5 + (23.33 * (Math.min(t, 1400) - 1100)) / 1000; // a walk, then Charge's 7 m in 0.3 s
    const lines: Record<number, number[][]> = {};
    let dust = 0;
    run(
      'skill.charge',
      (t) => ({ x: x(t), y: 0, z: 10 }),
      1800,
      (t) => {
        if ([1096, 1304, 1576].includes(t)) {
          lines[t] = spans('line');
        }
        if (t === 1064) {
          expect(named('band').length).toBeGreaterThan(0); // its swing
        }
        dust += scene.particleSystems.filter((p) => p.name === 'burst').length;
      },
    );
    expect([lines[1096]!.length, lines[1304]!.length, lines[1576]!.length]).toEqual([0, 4, 0]); // gone within 160 ms of the dash's end
    for (const [minX, maxX, minY, maxY] of lines[1304]!) {
      expect(minX).toBeCloseTo(x(1304 - 160), 1);
      expect(maxX).toBeCloseTo(x(1304), 1);
      expect([minY! > 0.3, maxY! < 1.6]).toEqual([true, true]);
    }
    expect(dust).toBe(0); // dust is a leap's
    expect(made()).toEqual(before);
  });

  it('a leap kicks up dust where it takes off, and a move with no shape shows no aura', () => {
    const x = (t: number) => (t < 1050 ? 10 : 10 - (13.3 * (Math.min(t, 1350) - 1050)) / 1000); // Roll Back's 4 m in 0.3 s
    let rings = 0;
    let dustAt = 0;
    run(
      'skill.roll_back',
      (t) => ({ x: x(t), y: 0, z: 10 }),
      1300,
      (t) => {
        rings = Math.max(rings, named('ring').length);
        if (!dustAt && scene.particleSystems.some((p) => p.name === 'burst')) {
          dustAt = t;
        }
      },
    );
    expect(dustAt).toBe(1080); // 0.3 m out: not while it stands
    const dust = scene.particleSystems.filter((p) => p.name === 'burst');
    expect(dust.map((p) => [(p.emitter as Vector3).x, (p.emitter as Vector3).z])).toEqual([
      [10, 10],
    ]);
    expect([rings, spans('line').length]).toEqual([0, 4]);
  });

  it('a blink flashes where the caster stood and again where the body lands, and only once if it never leaves', () => {
    const balls = () => named('ball').map((m) => [m.position.x, m.position.z]);
    const seen: Record<number, number[][]> = {};
    run(
      'skill.blink',
      (t) => ({ x: t < 1050 ? 10 : 16, y: 0, z: 10 }),
      1600,
      (t) => {
        if ([1000, 1064, 1256].includes(t)) {
          seen[t] = balls();
        }
        expect(named('ring')).toEqual([]);
      },
    );
    expect([seen[1000], seen[1064], seen[1256]]).toEqual([
      [[10, 10]],
      [
        [10, 10],
        [16, 10],
      ],
      [[16, 10]], // the first has faded
    ]);
    expect(balls()).toEqual([]);
    let most = 0;
    run(
      'skill.blink',
      () => ({ x: 10, y: 0, z: 10 }),
      1600,
      () => {
        most = Math.max(most, balls().length);
      },
    );
    expect([most, balls().length]).toEqual([1, 0]);
  });

  /** Where the patch named name made i-th lies: its middle and how far its farthest corner is from it. */
  const laid = (i: number) => {
    const m = named('patch')[i]!;
    const v = m.getVerticesData(VertexBuffer.PositionKind)!;
    const xs = v.filter((_, j) => j % 3 === 0);
    const zs = v.filter((_, j) => j % 3 === 2);
    const x = (Math.min(...xs) + Math.max(...xs)) / 2;
    const z = (Math.min(...zs) + Math.max(...zs)) / 2;
    const far = Math.max(...xs.map((px, j) => Math.hypot(px - x, zs[j]! - z)));
    return { x, z, far, shown: m.visibility };
  };

  it('an area at the lock lays its rim where the world centres it: on the lock within reach of the caster, following it, else reach along the aim; no aura', () => {
    const rain = skillById('arrow_rain')!.stages[0]!;
    const { radius = 0, reach = 0 } = rain.shape!;
    const lock = { x: 13, y: 0, z: 10.5, radius: 0.5, height: 0.875 };
    now = BEATS.start;
    fx.play('skill.arrow_rain', at({ lock: () => lock }));
    const centre = () => [laid(0).x, laid(0).z].map((c) => Math.round(c * 100) / 100);
    to(1016);
    expect(centre()).toEqual([13, 10.5]);
    expect(laid(0).far).toBeCloseTo(radius, 4);
    Object.assign(lock, { x: 15, z: 9 });
    to(1032);
    expect(centre()).toEqual([15, 9]);
    Object.assign(lock, { x: CASTER.x + reach + 0.5, z: 10 }); // past reach
    to(1048);
    expect(centre()).toEqual([CASTER.x + reach, CASTER.z]);
    expect(named('ring')).toEqual([]);
    fx.dispose();
    fx = new Fx(
      scene,
      map,
      { manifest: emptyManifest, sound: () => undefined },
      () => now,
      palette,
    );
    fx.play('skill.arrow_rain', at()); // no lock
    to(1064);
    expect(centre()).toEqual([CASTER.x + reach, CASTER.z]);
  });

  it('an area at the lock fills over its windup and flashes at each of its waves, then goes', () => {
    const rain = skillById('arrow_rain')!.stages[0]!;
    const { count, every_ms } = rain.hits!;
    const beats: Beats = { start: 1000, hit: 1300, strikeEnd: 1450, readyAt: 1700, end: 1800 };
    const before = made();
    now = beats.start;
    fx.play('skill.arrow_rain', at({ beats, lock: () => FOES[0] }));
    to(1150);
    expect(laid(1).far / rain.shape!.radius!).toBeCloseTo(0.5, 1); // half way through the windup
    const waves = Array.from({ length: count }, (_, k) => beats.hit + k * every_ms);
    for (const w of waves) {
      to(w);
      expect(laid(1).far).toBeCloseTo(rain.shape!.radius!, 4);
      expect(laid(1).shown).toBe(1);
      to(w + 130);
      expect(laid(1).shown).toBeCloseTo(w === waves.at(-1) ? 0.375 : 0.75, 2); // half its flash gone, and after the last half the circle too
    }
    to(Math.max(beats.end, waves.at(-1)! + 260) + 16);
    expect(made()).toEqual(before);
  });
});
