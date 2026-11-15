// Spike 1's test ground (3d-world.md section 13): a KayKit character on the 3D sim that walks, runs, jumps, attacks and cuts its attack with a skill, among the forest's mobs.
import type { AssetContainer } from '@babylonjs/core/assetContainer';
import { TargetCamera } from '@babylonjs/core/Cameras/targetCamera';
import type { AbstractEngine } from '@babylonjs/core/Engines/abstractEngine';
import { DirectionalLight } from '@babylonjs/core/Lights/directionalLight';
import { ShadowGenerator } from '@babylonjs/core/Lights/Shadows/shadowGenerator';
import '@babylonjs/core/Lights/Shadows/shadowGeneratorSceneComponent';
import { LoadAssetContainerAsync } from '@babylonjs/core/Loading/sceneLoader';
import { Color4 } from '@babylonjs/core/Maths/math.color';
import { Vector3 } from '@babylonjs/core/Maths/math.vector';
import { CreateBox } from '@babylonjs/core/Meshes/Builders/boxBuilder';
import { CreateGround } from '@babylonjs/core/Meshes/Builders/groundBuilder';
import { Scene } from '@babylonjs/core/scene';
import { registerBuiltInLoaders } from '@babylonjs/loaders/dynamic';

import type { Action } from '@/input/actions';
import { skillById } from '@/progression/skills';
import type { Input } from '@/sim/input';
import { step } from '@/sim/step';
import type { Box, Geometry, Params, State } from '@/sim/step';
import { terrain } from '@/sim/terrain';

import { ActionClock, attackTiming, skillTiming } from '../systems/actions';
import type { ActionTiming } from '../systems/actions';
import type { KeyTarget } from '../systems/keyboard';

import { beatsOf } from './beats';
import { Body, STILL } from './body';
import type { Gait } from './body';
import { cameraAt, CAMERA_EASE, turn, yawOf } from './follow';
import { classLook, forestMobs } from './looks';
import { linearOf, Palette } from './toon';

/** Where a model file comes from: its URL in the page, its bytes in a test. */
export type ModelSource = (file: string) => string | ArrayBufferView;

export interface StageEntry {
  readonly cls: string;
  readonly tickMs: number;
}

/** 3d-world.md section 5's body, as spike 2's vectors carry it; movement.yaml takes it in step 4. */
const BODY: Params = {
  dt: 1 / 60,
  walk_speed: 5,
  jump_velocity: 12.34,
  gravity: 38,
  max_fall_speed: 22.5,
  radius: 0.375,
  height: 1.75,
  step: 0.3,
  max_slope: 0.7,
  snap: 0.5,
};
const RUNNING: Params = { ...BODY, walk_speed: 8 }; // the spike's Shift: the sim has one pace

/** A flat square of ground in the sim's metres, walled at its edges, the player starting near its middle. */
const GROUND_M = 60;
const START = { x: 30, z: 26 };
const GROUND = 0x8cc265;
const SKY = new Color4(0.62, 0.8, 0.96, 1);
const SUN = new Vector3(0.35, -1, 0.6);
const SHADOW_MAP = 2048;
const CHEST_M = 0.9; // the camera looks at the player's chest
const TURN_MS = 60; // a turn eases most of the way in this
const MAX_STEP_MS = 100; // a frame after a stall moves no further than this

/** The forest's mobs stand in a row to the north, facing the camera; metre crates stand around, one in the player's way east. */
const MOB_ROW_Z = 33;
const MOB_GAP_M = 4;
const CRATE = 0xa0703c;
const CRATES: readonly Box[] = [
  [24, 23],
  [34, 26],
  [22, 36],
  [39, 38],
  [28, 40],
].map(([x = 0, z = 0]) => ({ x, z, half_w: 0.5, half_d: 0.5, ux: 1, uz: 0, top: 1 }));

let loadersRegistered = false;

export class Stage implements KeyTarget {
  private readonly clock: ActionClock;
  private readonly held = new Set<Action>();
  private running = false;
  private state: State = { ...START, y: 0, vx: 0, vy: 0, vz: 0, on_ground: true, hx: 0, hz: 1 };
  private before = this.state; // the sub-step before, the player drawn between them
  private owed = 0; // seconds of sim not yet stepped
  private yaw = 0;
  private readonly follow = new Vector3(START.x, CHEST_M, START.z);

  private constructor(
    readonly scene: Scene,
    private readonly camera: TargetCamera,
    private readonly player: Body,
    private readonly mobs: readonly Body[],
    private readonly geometry: Geometry,
    private readonly entry: StageEntry,
    private readonly now: () => number,
  ) {
    this.clock = new ActionClock(entry.tickMs, now, () => undefined);
    this.place(0);
  }

  static async open(
    engine: AbstractEngine,
    entry: StageEntry,
    source: ModelSource,
    now: () => number,
  ): Promise<Stage> {
    if (!loadersRegistered) {
      registerBuiltInLoaders();
      loadersRegistered = true;
    }
    const scene = new Scene(engine);
    scene.clearColor = SKY;
    const sun = new DirectionalLight('sun', SUN.normalizeToNew(), scene);
    const shadows = new ShadowGenerator(SHADOW_MAP, sun);
    shadows.usePercentageCloserFiltering = true;
    const palette = new Palette(scene);
    const lighting = { palette, shadows };

    const ground = CreateGround('ground', { width: GROUND_M, height: GROUND_M }, scene);
    ground.position.set(GROUND_M / 2, 0, GROUND_M / 2);
    ground.material = palette.toon({ texture: null, color: linearOf(GROUND) });
    ground.receiveShadows = true;
    for (const c of CRATES) {
      const crate = CreateBox(
        'crate',
        { width: c.half_w * 2, depth: c.half_d * 2, height: c.top },
        scene,
      );
      crate.position.set(c.x, c.top / 2, c.z);
      crate.material = palette.toon({ texture: null, color: linearOf(CRATE) });
      palette.outline(crate);
      shadows.addShadowCaster(crate, false);
    }

    const containers = new Map<string, Promise<AssetContainer>>();
    const load = (file: string) => {
      let c = containers.get(file);
      if (!c) {
        c = LoadAssetContainerAsync(source(file), scene, {
          pluginExtension: '.glb',
          // Quaternius's skins name a joint that isn't their joints' common root; without this Babylon skins them lying down, 100 times over.
          pluginOptions: { gltf: { alwaysComputeSkeletonRootNode: true } },
        });
        containers.set(file, c);
      }
      return c;
    };
    const spots = forestMobs.map((m, i) => ({
      look: m,
      x: START.x + (i - (forestMobs.length - 1) / 2) * MOB_GAP_M,
      z: MOB_ROW_Z + i,
    }));
    const look = classLook(entry.cls);
    const [player, ...mobs] = await Promise.all([
      load(look.file).then((c) => new Body(scene, c, look, lighting)),
      ...spots.map(async ({ look: m, x, z }) => {
        const body = new Body(scene, await load(m.file), m, lighting);
        body.root.position.set(x, 0, z);
        body.root.rotation.y = Math.PI;
        return body;
      }),
    ]);
    const geometry: Geometry = {
      size_x: GROUND_M,
      size_z: GROUND_M,
      terrain: terrain(GROUND_M, 2, 2, [0, 0, 0, 0], BODY.max_slope),
      boxes: CRATES,
      cylinders: [],
      blockers: spots.map(({ look: m, x, z }) => ({
        x,
        z,
        radius: m.radius,
        bottom: 0,
        top: m.height,
      })),
    };

    const camera = new TargetCamera('camera', Vector3.Zero(), scene);
    return new Stage(scene, camera, player, mobs, geometry, entry, now);
  }

  press(action: Action, again: boolean): void {
    this.held.add(action);
    if (!again && action === 'attack') {
      this.attack();
    }
  }

  /** A one-shot key: the first skill key uses Strike. */
  onAction(action: Action): void {
    if (action === 'skill1') {
      this.strike();
    }
  }

  release(action: Action): void {
    this.held.delete(action);
  }

  releaseAll(): void {
    this.held.clear();
  }

  /** The spike's Shift: running pace, which the sim has none of yet. */
  run(on: boolean): void {
    this.running = on;
  }

  /** One frame of dtMs: the sim steps what it owes at 60 a second, the player is drawn between its last two sub-steps, every body's clips blend, the camera eases after. */
  step(dtMs: number): void {
    const dt = Math.min(dtMs, MAX_STEP_MS);
    const now = this.now();
    if (this.held.has('attack')) {
      this.clock.repeatWhenFree(() => {
        if (this.held.has('attack')) {
          this.attack();
        }
      });
    }
    this.owed += dt / 1000;
    const params = this.running ? RUNNING : BODY;
    while (this.owed >= BODY.dt) {
      this.before = this.state;
      this.state = step(this.state, this.input(), this.geometry, params);
      this.owed -= BODY.dt;
    }
    this.place(dt);
    this.player.update(now, dt, this.gait());
    for (const mob of this.mobs) {
      mob.update(now, dt, STILL);
    }
  }

  dispose(): void {
    this.clock.reset();
    this.scene.dispose();
  }

  private input(): Input {
    const h = this.held;
    return {
      left: h.has('left'),
      right: h.has('right'),
      up: h.has('up'),
      down: h.has('down'),
      jump: h.has('jump'),
    };
  }

  private gait(): Gait {
    const s = this.state;
    const moving = s.vx !== 0 || s.vz !== 0;
    return { pace: !moving ? 'still' : this.running ? 'run' : 'walk', onGround: s.on_ground };
  }

  /** Draws the player owed of the way from its sub-step before to its last, turned toward its heading, and eases the camera after. */
  private place(dtMs: number): void {
    const a = this.before;
    const b = this.state;
    const f = this.owed / BODY.dt;
    const root = this.player.root;
    root.position.set(a.x + (b.x - a.x) * f, a.y + (b.y - a.y) * f, a.z + (b.z - a.z) * f);
    this.yaw = turn(this.yaw, yawOf(b.hx, b.hz), Math.min(1, dtMs / TURN_MS));
    root.rotation.y = this.yaw;

    const p = root.position;
    this.follow.x += (p.x - this.follow.x) * CAMERA_EASE;
    this.follow.y += (p.y + CHEST_M - this.follow.y) * CAMERA_EASE;
    this.follow.z += (p.z - this.follow.z) * CAMERA_EASE;
    const [x, y, z] = cameraAt(this.follow.x, this.follow.y, this.follow.z);
    this.camera.position.set(x, y, z);
    this.camera.setTarget(this.follow);
  }

  private attack(): void {
    if (this.clock.press('attack', () => this.attack())) {
      this.begin(attackTiming(undefined, this.entry.cls, this.entry.tickMs));
    }
  }

  private strike(): void {
    const stage = skillById('strike')?.stages[0];
    if (stage && this.clock.press('skill', () => this.strike())) {
      this.begin(skillTiming(stage, this.entry.tickMs));
    }
  }

  private begin(t: ActionTiming): void {
    const start = this.now();
    this.clock.begin(t);
    this.player.act(t.kind, beatsOf(t, start, this.entry.tickMs));
  }
}
