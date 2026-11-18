// One drawn model (3d-world.md section 10): its other gear hidden, scaled to its height, toon-drawn and inked, its clips layered over the spine, its poses played as actions.
import type { AnimationGroup } from '@babylonjs/core/Animations/animationGroup';
import {
  AnimationGroupMask,
  AnimationGroupMaskMode,
} from '@babylonjs/core/Animations/animationGroupMask';
import type { AssetContainer } from '@babylonjs/core/assetContainer';
import type { Skeleton } from '@babylonjs/core/Bones/skeleton';
import type { DirectionalLight } from '@babylonjs/core/Lights/directionalLight';
import { ShadowGenerator } from '@babylonjs/core/Lights/Shadows/shadowGenerator';
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
import { clipsOf } from './model-looks';
import type { ActionPose } from './model-looks';
import { upperBody } from './rig';
import type { Joint } from './rig';
import { Swing } from './swing';
import { linearOf, warm } from './toon';
import type { Palette, ToonBase } from './toon';

const MOVING = 'moving';

/** A clip's whole-body group, or its lower body's when it has an upper one. */
interface Layer {
  readonly lower: AnimationGroup;
  readonly upper: AnimationGroup | null;
}

/** How far through its clip an action is at a moment, 0 to 1. */
type Share = (now: number) => number;

/** An action as act laid it out: its clip, the share its strike falls at, its length, from when it may start, its recovery after its strike, and how much faster than itself it may play. */
interface Timed {
  readonly clip: string;
  readonly strike: number;
  readonly ms: number;
  readonly begin: number;
  readonly recover: number;
  readonly fastest: number;
}

const SLOWEST = 3; // either side of its strike, an action's clip plays at most this many times slower than itself, so a long windup or recovery doesn't freeze it

/** How a body stands as blows and death leave it: up, a flinch, knocked down, rising again in super armor, or dead. */
export type Pose = 'up' | 'flinch' | 'down' | 'rising' | 'dead';

/** A body getting up from down plays its fall back over this. */
/** How much of a dodging body shows. */
export const FADED = 0.4;

export const RISE_MS = 400;

/** What the world draws a player, mob or NPC as. */
export interface Figure {
  /** Placed and turned by its owner; the body stands on it, feet at its origin. */
  readonly root: TransformNode;
  /** fastest is how much faster than itself its clip may play: past that, a lead-in starts part-way and a follow-through fades out short of its end. */
  act(kind: ActionPose, beats: Beats, fastest?: number): void;
  /** Moves the action's strike to hit, as the world's hit turned out, from where its clip stands at now and within its pace; one already struck, or gone, is left alone. */
  restrike(hit: number, now: number): void;
  pose(p: Pose, now: number): void;
  /** Holds the action's pose through a hit-stop, then lets it catch up by the ready point. */
  hold(h: Hold): void;
  /** A still copy of the figure as posed now, drawn in material, for a cancel's afterimage; null for one that has none. */
  afterimage(material: Material): Still | null;
  /** Draws it faded with its ink hidden, as a dodge shows it, or whole again. */
  fade(on: boolean): void;
  update(now: number, dtMs: number, gait: Gait): void;
  dispose(): void;
}

/** How a body moves, which picks its legs' clip and whether an action takes its upper body alone. */
export interface Gait {
  readonly pace: 'still' | 'walk' | 'run';
  readonly onGround: boolean;
}

export const STILL: Gait = { pace: 'still', onGround: true };

/** A figure's still copy: placed and faded by its owner, hidden till it's posed again. */
export interface Still {
  readonly root: TransformNode;
  readonly meshes: readonly AbstractMesh[];
}

export interface Lighting {
  readonly palette: Palette;
  readonly shadows: ShadowGenerator;
}

/** The sun's shadows, size texels a side and soft, cast by see-through meshes too: a solid faded or gone from the camera's way keeps its shadow. */
export function sunShadows(sun: DirectionalLight, size: number): ShadowGenerator {
  const shadows = new ShadowGenerator(size, sun);
  shadows.usePercentageCloserFiltering = true;
  shadows.transparencyShadow = true;
  return shadows;
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
  private timed: Timed | null = null; // the action act laid out, which restrike may move
  private waiting: {
    readonly clip: string;
    readonly share: Share;
    readonly start: number;
    readonly readyAt: number;
  } | null = null; // an action whose clip hasn't started, till which the body keeps its stance
  private held: Hold | null = null;
  private readonly model: TransformNode;
  private readonly shades: { mesh: AbstractMesh; whole: Material; faded: Material }[] = [];
  private readonly inks: AbstractMesh[] = [];
  private faded = false;
  private still:
    | (Still & {
        readonly model: TransformNode;
        readonly nodes: ReadonlyMap<string, TransformNode>;
        readonly skeletons: readonly Skeleton[];
      })
    | null = null;

  constructor(
    scene: Scene,
    private readonly container: AssetContainer,
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
    this.model = model;
    model.parent = this.root;
    const meshes = stand(model, this.root, look);
    for (const m of meshes) {
      if (m.material) {
        const base = baseOf(m.material, m.name, look);
        m.material = lighting.palette.toon(base);
        const faded = lighting.palette.toon(base, FADED);
        faded.needDepthPrePass = true; // its front alone, not the parts behind it
        this.shades.push({ mesh: m, whole: m.material, faded });
      }
      lighting.palette.outline(m);
      this.inks.push(...m.getChildMeshes(true).filter((c) => c.name === `${m.name} ink`));
      lighting.shadows.addShadowCaster(m, false);
    }
    unstretched(model);
    warm(this.shades.map((s) => [s.faded, s.mesh] as const));
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

  /** Plays an action's clip over its beats, each side of its strike no slower than SLOWEST nor faster than fastest, the body keeping its stance outside; one already playing fades out under it, held where it was cut. */
  act(kind: ActionPose, beats: Beats, fastest = Infinity): void {
    const a = this.look.clips[kind];
    if (!a) {
      return;
    }
    const c: Timed = {
      clip: a.clip,
      strike: a.strike,
      ms: this.lengthMs(a.clip),
      begin: beats.start,
      recover: beats.readyAt - beats.hit,
      fastest,
    };
    this.timed = c;
    this.lay(c, beats.hit, beats.start);
    this.swing?.follow(beats);
  }

  restrike(hit: number, now: number): void {
    const c = this.timed;
    if (!c) {
      return;
    }
    if (this.waiting?.clip === c.clip) {
      this.lay(c, hit, now); // not begun: laid out again
      return;
    }
    const share = this.posed.get(c.clip);
    const at = share?.(now) ?? 1;
    if (this.current?.clip !== c.clip || at >= c.strike) {
      return;
    }
    const left = (c.strike - at) * c.ms; // of its clip, to its strike
    const struck = Math.min(now + SLOWEST * left, Math.max(now + left / c.fastest, hit));
    const run = this.course(c, now, at, struck);
    this.posed.set(c.clip, run.share);
    this.current = { clip: c.clip, readyAt: run.readyAt };
  }

  /** Lays c's clip out to strike on hit, from no sooner than its beats' start or now: playing now, or waiting till it starts. */
  private lay(c: Timed, hit: number, now: number): void {
    const start = Math.max(c.begin, now, hit - SLOWEST * c.strike * c.ms);
    const from = Number.isFinite(c.fastest)
      ? Math.max(0, c.strike - ((hit - start) * c.fastest) / c.ms) // a lead-in too short starts part-way
      : 0;
    const run = this.course(c, start, from, hit);
    if (start > now) {
      this.waiting = { clip: c.clip, share: run.share, start, readyAt: run.readyAt };
    } else {
      this.play(c.clip, run.share, start, run.readyAt);
    }
  }

  /** c's clip from share `from` at t to its strike at hit, then on over its recovery, no slower than SLOWEST nor faster than its fastest; a follow-through that won't fit fades out short of its clip's end. */
  private course(
    c: Timed,
    t: number,
    from: number,
    hit: number,
  ): { share: Share; readyAt: number } {
    const readyAt = hit + Math.min(c.recover, SLOWEST * (1 - c.strike) * c.ms);
    const to = Number.isFinite(c.fastest)
      ? Math.min(1, c.strike + ((readyAt - hit) * c.fastest) / c.ms)
      : 1;
    const part = (now: number, a: number, b: number) =>
      Math.min(1, Math.max(0, (now - a) / Math.max(1, b - a)));
    return {
      share: (now) =>
        now < hit
          ? from + (c.strike - from) * part(now, t, hit)
          : c.strike + (to - c.strike) * part(now, hit, readyAt),
      readyAt,
    };
  }

  hold(h: Hold): void {
    this.held = h;
  }

  /** A copy of the model made the first time, then posed as the body is now each time: every node where the body's is, every mesh in material. */
  fade(on: boolean): void {
    if (on === this.faded) {
      return;
    }
    this.faded = on;
    for (const s of this.shades) {
      s.mesh.material = on ? s.faded : s.whole;
    }
    for (const ink of this.inks) {
      ink.setEnabled(!on);
    }
  }

  afterimage(material: Material): Still {
    const still = (this.still ??= this.copy(material));
    for (const n of this.model.getDescendants(false)) {
      const c = still.nodes.get(n.name); // by name: the body has ink hulls and a blade the copy lacks
      if (n instanceof TransformNode && c) {
        c.position.copyFrom(n.position);
        c.scaling.copyFrom(n.scaling);
        if (n.rotationQuaternion) {
          (c.rotationQuaternion ??= n.rotationQuaternion.clone()).copyFrom(n.rotationQuaternion);
        } else {
          c.rotation.copyFrom(n.rotation);
        }
      }
    }
    still.model.position.copyFrom(this.model.position);
    still.root.position.copyFrom(this.root.position);
    still.root.rotation.copyFrom(this.root.rotation);
    still.root.scaling.copyFrom(this.root.scaling);
    still.root.setEnabled(true);
    return still;
  }

  /** Plays a pose as an action from now: a flinch's hit clip once, a fall held down, the fall played back to rise; up lets a held one go. */
  pose(p: Pose, now: number): void {
    const { hit, death } = this.look.clips;
    this.timed = null; // a flinch or a fall takes over from any action
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
    if (this.waiting && now >= this.waiting.start) {
      const w = this.waiting;
      this.play(w.clip, w.share, w.start, w.readyAt);
    }
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
    this.still?.root.dispose();
    for (const k of [...this.skeletons, ...(this.still?.skeletons ?? [])]) {
      k.dispose();
    }
  }

  /** The model again, with its own skeleton and no clips, its gear hidden as the body's is, every mesh in material. */
  private copy(material: Material): NonNullable<Body['still']> {
    const made = this.container.instantiateModelsToScene((n) => n, false, {
      doNotInstantiate: true,
    });
    for (const g of made.animationGroups) {
      g.dispose();
    }
    const model = made.rootNodes[0] as TransformNode;
    const root = new TransformNode(`${this.look.file} still`, this.root.getScene());
    model.parent = root;
    for (const n of model.getDescendants(false)) {
      if (this.look.hide.includes(n.name)) {
        n.setEnabled(false);
      }
    }
    const meshes = model
      .getChildMeshes(false)
      .filter((m) => m.isEnabled() && m.getTotalVertices() > 0);
    for (const m of meshes) {
      m.material = material;
      m.isPickable = false;
    }
    unstretched(model);
    root.setEnabled(false);
    const nodes = new Map<string, TransformNode>();
    for (const n of model.getDescendants(false)) {
      if (n instanceof TransformNode) {
        nodes.set(n.name, n);
      }
    }
    return { root, model, nodes, meshes, skeletons: made.skeletons };
  }

  /** Starts clip posed by share, the clips already posed held where start cut them, till readyAt; an action still waiting is dropped. */
  private play(clip: string, share: Share, start: number, readyAt: number): void {
    this.waiting = null;
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

/** Hides model's other gear and stands it on root's origin, scaled to the look's height, or less where it would be wider than the look allows; returns the meshes it draws. */
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
  const { min, max } = model.getHierarchyBoundingVectors(
    true,
    (m) => m.isEnabled() && !look.over?.includes(m.name),
  ); // the body alone, a crown worn over it left out
  const across = Math.max(max.x - min.x, max.z - min.z);
  const scale = Math.min(look.height / (max.y - min.y), (look.width ?? Infinity) / across); // a mob drawn wider than its circle would look to overlap the next
  model.position.y -= min.y - (look.flies ? (look.height / scale - (max.y - min.y)) / 2 : 0);
  root.scaling.setAll(scale);
  return model.getChildMeshes(false).filter((m) => m.isEnabled() && m.getTotalVertices() > 0);
}

/** Draws a model's meshes as uniformly scaled, which they are: its clips stray from uniform by under 0.0001, enough to switch a mesh to a shader of its own the first time a pose strays. */
function unstretched(model: TransformNode): void {
  for (const m of model.getChildMeshes(false)) {
    m.ignoreNonUniformScaling = true;
  }
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
