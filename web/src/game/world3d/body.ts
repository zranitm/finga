// One drawn model (3d-world.md section 10): its other gear hidden, scaled to its height, toon-drawn and inked, its clips layered over the spine, its poses played as actions.
import type { AnimationGroup } from '@babylonjs/core/Animations/animationGroup';
import {
  AnimationGroupMask,
  AnimationGroupMaskMode,
} from '@babylonjs/core/Animations/animationGroupMask';
import type { AssetContainer } from '@babylonjs/core/assetContainer';
import type { Skeleton } from '@babylonjs/core/Bones/skeleton';
import type { ShadowGenerator } from '@babylonjs/core/Lights/Shadows/shadowGenerator';
import type { Material } from '@babylonjs/core/Materials/material';
import type { PBRMaterial } from '@babylonjs/core/Materials/PBR/pbrMaterial';
import { Texture } from '@babylonjs/core/Materials/Textures/texture';
import { Color3 } from '@babylonjs/core/Maths/math.color';
import type { AbstractMesh } from '@babylonjs/core/Meshes/abstractMesh';
import { TransformNode } from '@babylonjs/core/Meshes/transformNode';
import type { Node } from '@babylonjs/core/node';
import type { Scene } from '@babylonjs/core/scene';

import { heldTime } from '../systems/hit-stop';
import type { Hold } from '../systems/hit-stop';

import type { Beats } from './beats';
import { BLEND_MS, Fades, layered } from './blend';
import type { ModelLook } from './looks';
import { upperBody } from './rig';
import type { Joint } from './rig';
import { Swing } from './swing';
import { linearOf } from './toon';
import type { Palette, ToonBase } from './toon';

const MOVING = 'moving';

/** A clip's whole-body group, or its lower body's when it has an upper one. */
interface Layer {
  readonly lower: AnimationGroup;
  readonly upper: AnimationGroup | null;
}

/** How far through its clip an action is at a moment, 0 to 1. */
type Share = (now: number) => number;

/** How a body stands as blows and death leave it: up, a flinch, knocked down, rising again in super armor, or dead. */
export type Pose = 'up' | 'flinch' | 'down' | 'rising' | 'dead';

/** A body getting up from down plays its fall back over this. */
export const RISE_MS = 400;

/** What the world draws a player, mob or NPC as: a model or a stand-in. */
export interface Figure {
  /** Placed and turned by its owner; the body stands on it, feet at its origin. */
  readonly root: TransformNode;
  act(kind: 'attack' | 'skill', beats: Beats): void;
  pose(p: Pose, now: number): void;
  /** Holds the action's pose through a hit-stop, then lets it catch up by the ready point. */
  hold(h: Hold): void;
  update(now: number, dtMs: number, gait: Gait): void;
  dispose(): void;
}

/** How a body moves, which picks its legs' clip and whether an action takes its upper body alone. */
export interface Gait {
  readonly pace: 'still' | 'walk' | 'run';
  readonly onGround: boolean;
}

export const STILL: Gait = { pace: 'still', onGround: true };

export interface Lighting {
  readonly palette: Palette;
  readonly shadows: ShadowGenerator;
}

export class Body implements Figure {
  readonly root: TransformNode;
  private readonly layers = new Map<string, Layer>();
  private readonly actions = new Fades();
  private readonly locomotion = new Fades();
  private readonly moving = new Fades();
  private readonly split: boolean;
  private readonly swing: Swing | null;
  private readonly skeletons: readonly Skeleton[];
  private readonly posed = new Map<string, Share>(); // by clip, while it has weight
  private current: { readonly clip: string; readonly readyAt: number } | null = null;
  private held: Hold | null = null;

  constructor(
    scene: Scene,
    container: AssetContainer,
    private readonly look: ModelLook,
    lighting: Lighting,
  ) {
    const made = container.instantiateModelsToScene((n) => n, false, { doNotInstantiate: true });
    this.skeletons = made.skeletons;
    this.root = new TransformNode(look.file, scene);
    const model = made.rootNodes[0];
    if (!(model instanceof TransformNode)) {
      throw new Error(`${look.file} has no root node`);
    }
    model.parent = this.root;
    const meshes = stand(model, this.root, look);
    for (const m of meshes) {
      if (m.material) {
        m.material = lighting.palette.toon(baseOf(m.material, m.name, look));
      }
      lighting.palette.outline(m);
      lighting.shadows.addShadowCaster(m, false);
    }
    const weapon = look.weapon ? meshes.find((m) => m.name === look.weapon) : undefined;
    this.swing = weapon ? new Swing(scene, weapon) : null;

    const upper = look.spine ? upperBody(jointsOf(model), look.spine) : new Set<string>();
    this.split = upper.size > 0;
    this.layerClips(made.animationGroups, upper);
    const { idle, walk, run, fall } = look.clips;
    for (const clip of new Set([idle, walk, run, fall])) {
      this.each(clip, (g) => g.start(true));
    }
  }

  /** Plays an action's clip over its beats; one already playing fades out under it, held where it was cut. */
  act(kind: 'attack' | 'skill', beats: Beats): void {
    const a = this.look.clips[kind];
    if (!a) {
      return;
    }
    this.play(a.clip, (now) => shareAt(beats, a.strike, now), beats.start, beats.readyAt);
    this.swing?.follow(beats);
  }

  hold(h: Hold): void {
    this.held = h;
  }

  /** Plays a pose as an action from now: a flinch's hit clip once, a fall held down, the fall played back to rise; up lets a held one go. */
  pose(p: Pose, now: number): void {
    const { hit, death } = this.look.clips;
    const through = (clip: string) => {
      const ms = this.lengthMs(clip);
      return (t: number) => Math.min(1, Math.max(0, (t - now) / ms));
    };
    switch (p) {
      case 'flinch':
        this.play(hit, through(hit), now, now + this.lengthMs(hit));
        return;
      case 'down':
      case 'dead':
        this.play(death, through(death), now, Infinity);
        return;
      case 'rising':
        this.play(
          death,
          (t) => 1 - Math.min(1, Math.max(0, (t - now) / RISE_MS)),
          now,
          now + RISE_MS,
        );
        return;
      case 'up':
        if (this.current?.readyAt === Infinity) {
          this.current = { ...this.current, readyAt: now };
        }
        return;
    }
  }

  /** Steps the blends by dtMs to now: locomotion by gait, an action fading out to be gone at its ready point, the mask switching as the body starts or stops moving. */
  update(now: number, dtMs: number, gait: Gait): void {
    if (this.current && now >= this.current.readyAt - BLEND_MS) {
      this.actions.aim(this.current.clip, 0);
      this.current = null;
    }
    const { clips } = this.look;
    this.locomotion.only(
      !gait.onGround
        ? clips.fall
        : gait.pace === 'still'
          ? clips.idle
          : gait.pace === 'run'
            ? clips.run
            : clips.walk,
    );
    this.moving.aim(MOVING, gait.pace !== 'still' || !gait.onGround ? 1 : 0);
    this.actions.step(dtMs);
    this.locomotion.step(dtMs);
    this.moving.step(dtMs);
    const moving = this.split ? this.moving.get(MOVING) : 0;
    for (const [clip, w] of layered(this.actions.entries(), this.locomotion.entries(), moving)) {
      const l = this.layer(clip);
      l.lower.weight = w.lower;
      if (l.upper) {
        l.upper.weight = w.upper;
      }
    }
    for (const [clip, share] of this.posed) {
      if (this.actions.get(clip) === 0 && clip !== this.current?.clip) {
        this.each(clip, (g) => g.stop());
        this.posed.delete(clip);
        continue;
      }
      const at = heldTime(this.held, now);
      this.each(clip, (g) => g.goToFrame(g.from + (g.to - g.from) * share(at), true));
    }
    this.swing?.update(heldTime(this.held, now));
  }

  dispose(): void {
    for (const l of this.layers.values()) {
      l.lower.dispose();
      l.upper?.dispose();
    }
    this.swing?.dispose();
    this.root.dispose(); // its materials stay: the palette shares them
    for (const k of this.skeletons) {
      k.dispose();
    }
  }

  /** Starts clip posed by share, the clips already posed held where start cut them, till readyAt. */
  private play(clip: string, share: Share, start: number, readyAt: number): void {
    this.each(clip, (g) => {
      g.stop();
      g.start(false, 1, g.from, g.to);
      g.pause(); // posed each frame, not by the clock
    });
    for (const [c, s] of this.posed) {
      const cut = s(start);
      this.posed.set(c, () => cut);
    }
    this.posed.set(clip, share);
    this.current = { clip, readyAt };
    this.actions.only(clip);
  }

  /** A clip's length in milliseconds at its own frame rate. */
  private lengthMs(clip: string): number {
    const g = this.layer(clip).lower;
    const fps = g.targetedAnimations[0]?.animation.framePerSecond ?? 60;
    return ((g.to - g.from) / fps) * 1000;
  }

  /** Keeps the clips the look plays, each split at the spine into a lower and an upper group when the body has one, every group at weight 0. */
  private layerClips(groups: readonly AnimationGroup[], upper: ReadonlySet<string>): void {
    const wanted = clipsOf(this.look);
    for (const g of groups) {
      if (!wanted.includes(g.name)) {
        g.dispose();
      }
    }
    for (const clip of wanted) {
      const lower = groups.find((g) => g.name === clip);
      if (!lower) {
        throw new Error(`${this.look.file} has no clip ${clip}`);
      }
      let top: AnimationGroup | null = null;
      if (this.split) {
        top = lower.clone(`${clip}/upper`, (t: unknown) => t);
        top.mask = new AnimationGroupMask([...upper], AnimationGroupMaskMode.Include);
        lower.mask = new AnimationGroupMask([...upper], AnimationGroupMaskMode.Exclude);
      }
      this.layers.set(clip, { lower, upper: top });
      this.each(clip, (g) => (g.weight = 0)); // Babylon's default, -1, draws a group unweighted
    }
  }

  private layer(clip: string): Layer {
    const l = this.layers.get(clip);
    if (!l) {
      throw new Error(`${this.look.file} plays no clip ${clip}`);
    }
    return l;
  }

  private each(clip: string, f: (g: AnimationGroup) => void): void {
    const l = this.layer(clip);
    f(l.lower);
    if (l.upper) {
      f(l.upper);
    }
  }
}

function clipsOf(look: ModelLook): string[] {
  const c = look.clips;
  const all = [c.idle, c.walk, c.run, c.fall, c.hit, c.death, c.attack?.clip, c.skill?.clip];
  return [...new Set(all.filter((x): x is string => x !== undefined))];
}

/** Hides model's other gear and stands it on root's origin, scaled to the look's height; returns the meshes it draws. */
function stand(model: TransformNode, root: TransformNode, look: ModelLook): AbstractMesh[] {
  const descendants = model.getDescendants(false);
  const named = new Set(descendants.map((n) => n.name));
  for (const m of model.getChildMeshes(false)) {
    if (m.material) {
      named.add(m.material.name);
    }
  }
  const parts = [
    ...look.hide,
    ...(look.tint ? [look.tint.part] : []),
    ...(look.weapon ? [look.weapon] : []),
  ];
  const missing = parts.filter((p) => !named.has(p));
  if (missing.length > 0) {
    throw new Error(`${look.file} has no ${missing.join(', ')}`); // a look out of step with its file
  }
  for (const n of descendants) {
    if (look.hide.includes(n.name)) {
      n.setEnabled(false);
    }
  }
  for (const m of model.getChildMeshes(false)) {
    if (m.skeleton) {
      m.skeleton.prepare(true);
      m.refreshBoundingInfo({ applySkeleton: true }); // its bounds as drawn, for the height and for culling
    }
  }
  const { min, max } = model.getHierarchyBoundingVectors(true, (m) => m.isEnabled());
  model.position.y -= min.y;
  root.scaling.setAll(look.height / (max.y - min.y));
  return model.getChildMeshes(false).filter((m) => m.isEnabled() && m.getTotalVertices() > 0);
}

/** The toon base for a mesh's glTF material: its texture and colour in the look's light, the look's tint in place of the colour on the part it names, a material or a mesh. */
function baseOf(source: Material, mesh: string, look: ModelLook): ToonBase {
  const pbr = source as Partial<Pick<PBRMaterial, 'albedoTexture' | 'albedoColor'>>;
  const tinted = look.tint && (look.tint.part === source.name || look.tint.part === mesh);
  const color =
    look.tint && tinted
      ? linearOf(look.tint.color)
      : (pbr.albedoColor ?? Color3.White()).scale(look.exposure);
  return { texture: pbr.albedoTexture instanceof Texture ? pbr.albedoTexture : null, color };
}

function jointsOf(model: Node): Joint[] {
  return model.getDescendants(false).map((n) => ({ name: n.name, parent: n.parent?.name ?? null }));
}

/** How far through its clip an action is at now: to the strike by the hit, to the end by the ready point. */
function shareAt(b: Beats, strike: number, now: number): number {
  const part = (from: number, to: number) =>
    Math.min(1, Math.max(0, (now - from) / Math.max(1, to - from)));
  return now < b.hit
    ? strike * part(b.start, b.hit)
    : strike + (1 - strike) * part(b.hit, b.readyAt);
}
