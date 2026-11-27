// Dressing a body in its outfit pieces (outfits.md section 6): skinned parts bound to its rig, rigid ones on their joints, wings on hinges at the back of its chest that open and beat with its gait.
import type { AssetContainer } from '@babylonjs/core/assetContainer';
import type { Skeleton } from '@babylonjs/core/Bones/skeleton';
import type { Texture } from '@babylonjs/core/Materials/Textures/texture';
import type { AbstractMesh } from '@babylonjs/core/Meshes/abstractMesh';
import type { Mesh } from '@babylonjs/core/Meshes/mesh';
import { TransformNode } from '@babylonjs/core/Meshes/transformNode';

import type { Gait } from './body';
import type { ModelLook } from './looks';
import type { PieceLook } from './outfit-looks';

/** A piece ready to wear: its look, its parts file loaded, and its colourway's atlas if it has one. */
export interface Worn {
  readonly piece: PieceLook;
  readonly parts: AssetContainer;
  readonly atlas: Texture | null;
}

/** A part drawn on a body, with the atlas it's drawn in when a colourway gives one, and whether it glows. */
export interface Part {
  readonly mesh: AbstractMesh;
  readonly atlas: Texture | null;
  readonly glow: boolean;
}

export interface Dressed {
  readonly parts: readonly Part[];
  readonly weapon: AbstractMesh | null; // the weapon look's, in place of the class's own
  readonly hinges: readonly Hinge[];
}

/** A wing half's hinge, and the side its half spreads to: 1 for the model's +x, -1 for its -x. */
export interface Hinge {
  readonly node: TransformNode;
  readonly side: 1 | -1;
}

const isCape = (name: string) => name.endsWith('_Cape');

/** A worn part's name, apart from the class's own of the same mesh. */
const wornName = (mesh: string) => `${mesh} worn`;

/** Hides the class's own meshes each worn piece takes the place of, its headwear on a bare head, its hooded head under headwear and its cape under wings, then draws the pieces' meshes on model: skinned ones on skeleton, rigid ones on their joints, wings on hinges. */
export function dress(
  model: TransformNode,
  skeleton: Skeleton | undefined,
  look: ModelLook,
  worn: readonly Worn[],
): Dressed {
  if (worn.length === 0 && !look.bareHead) {
    return { parts: [], weapon: null, hinges: [] };
  }
  const own = look.own;
  if (!own || !skeleton) {
    throw new Error(`${look.file} wears no pieces`); // a mob's or an NPC's model
  }
  const nodes = new Map<string, TransformNode>();
  for (const n of model.getDescendants(false)) {
    if (n instanceof TransformNode) {
      nodes.set(n.name, n);
    }
  }
  const winged = worn.some((w) => w.piece.slot === 'wings');
  const hidden = [
    ...worn.flatMap((w) =>
      w.piece.slot === 'face' || w.piece.slot === 'wings' ? [] : own[w.piece.slot],
    ),
    ...(look.bareHead ? own.headwear : []),
    ...(worn.some((w) => w.piece.ownHead) ? own.head : []), // a skeleton's own skull
    ...(look.hood && worn.some((w) => w.piece.slot === 'headwear') ? [look.hood.head] : []), // its bare head is worn in its place
  ];
  for (const name of hidden) {
    nodes.get(name)?.setEnabled(false);
  }
  if (winged) {
    for (const [name, n] of nodes) {
      if (isCape(name)) {
        n.setEnabled(false);
      }
    }
  }
  const parts: Part[] = [];
  const hinges: Hinge[] = [];
  let weapon: AbstractMesh | null = null;
  for (const w of worn) {
    const rig = w.parts.skeletons[0];
    if (rig && !sameJoints(rig, skeleton)) {
      throw new Error(`${w.piece.file}'s rig has other joints than ${look.file}'s`);
    }
    for (const name of w.piece.meshes) {
      if (winged && isCape(name)) {
        continue; // wings take the cape's place
      }
      const source = w.parts.meshes.find((m) => m.name === name) as Mesh | undefined;
      if (!source) {
        throw new Error(`${w.piece.file}'s parts have no ${name}`);
      }
      const mesh =
        w.piece.slot === 'wings'
          ? hinged(source, at(nodes, 'chest', look), hinges)
          : source.skeleton
            ? bound(source, at(nodes, source.parent?.name, look), skeleton)
            : hung(source, at(nodes, source.parent?.name, look), w.piece, look);
      parts.push({ mesh, atlas: w.atlas, glow: w.piece.glow === true });
      if (w.piece.slot === 'weapon') {
        weapon ??= mesh;
      }
    }
  }
  return { parts, weapon, hinges };
}

/** Whether two rigs name the same joints in the same order, so a mesh skinned to one is skinned to the other. */
export function sameJoints(a: Skeleton, b: Skeleton): boolean {
  return a.bones.length === b.bones.length && a.bones.every((j, i) => j.name === b.bones[i]!.name);
}

function at(
  nodes: ReadonlyMap<string, TransformNode>,
  name: string | undefined,
  look: ModelLook,
): TransformNode {
  const n = name === undefined ? undefined : nodes.get(name);
  if (!n) {
    throw new Error(`${look.file} has no joint ${name ?? '(none)'} to wear a piece on`);
  }
  return n;
}

/** A skinned part on the wearer's rig: the same parent as its own body's, its rig swapped for the wearer's. */
function bound(source: Mesh, parent: TransformNode, skeleton: Skeleton): Mesh {
  const mesh = source.clone(wornName(source.name), parent, true);
  mesh.skeleton = skeleton;
  mesh.setEnabled(true);
  skeleton.prepare(true);
  mesh.refreshBoundingInfo({ applySkeleton: true }); // its bounds as drawn, for culling
  return mesh;
}

/** A rigid part on its joint, where it hangs in its own file, moved and scaled by its fit to this body. */
function hung(source: Mesh, joint: TransformNode, piece: PieceLook, look: ModelLook): Mesh {
  const mesh = source.clone(wornName(source.name), joint, true);
  const fit = piece.fit?.[look.file];
  if (fit) {
    mesh.position.addInPlaceFromFloats(...fit.offset);
    mesh.scaling.scaleInPlace(fit.scale);
  }
  mesh.setEnabled(true);
  return mesh;
}

/** A wing half on a hinge at chest, where the half's root lies in its file; the half spreads to the side its middle lies on. */
function hinged(source: Mesh, chest: TransformNode, hinges: Hinge[]): Mesh {
  const node = new TransformNode(`${source.name} hinge`, chest.getScene());
  node.parent = chest;
  node.position.copyFrom(source.position);
  const mesh = source.clone(wornName(source.name), node, true);
  mesh.position.setAll(0);
  mesh.setEnabled(true);
  const middle = source.getBoundingInfo().boundingBox.center.x;
  hinges.push({ node, side: middle < 0 ? -1 : 1 });
  return mesh;
}

/** How a pair of wings moves: a beat's length, the swing either side, and how far open, in degrees from folded straight back to 90, spread flat. */
interface Flap {
  beatMs: number;
  swing: number;
  open: number;
}

/** Each state's flap (outfits.md section 6); folded keeps its beat, with no swing. */
export const FLAPS = {
  standing: { beatMs: 2400, swing: 6, open: 55 },
  moving: { beatMs: 1200, swing: 10, open: 60 },
  sprinting: { beatMs: 600, swing: 16, open: 45 }, // swept back
  air: { beatMs: 1600, swing: 12, open: 80 },
  folded: { swing: 0, open: 10 },
} as const;

export type WingState = keyof typeof FLAPS;

export const WING_EASE_MS = 150;

const RAD = Math.PI / 180;

/** The wings' state for a body's gait, folded while it acts, flinches, falls or lies dead, so they never hide its pose. */
export function wingState(gait: Gait, acting: boolean): WingState {
  if (acting) return 'folded';
  if (!gait.onGround) return 'air';
  switch (gait.pace) {
    case 'still':
      return 'standing';
    case 'sprint':
      return 'sprinting';
    default:
      return 'moving';
  }
}

/** Turns a pair's hinges each frame: open and swinging by the state's flap, eased over WING_EASE_MS from wherever a change of state found them. */
export class Wings {
  private state: WingState = 'standing';
  private readonly from: Flap = { ...FLAPS.standing };
  private readonly now: Flap = { ...FLAPS.standing };
  private changedAt = -Infinity;
  private phase = 0; // of the beat, in beats

  constructor(private readonly hinges: readonly Hinge[]) {
    this.turn();
  }

  update(now: number, dtMs: number, state: WingState): void {
    if (state !== this.state) {
      this.state = state;
      Object.assign(this.from, this.now);
      this.changedAt = now;
    }
    const to = FLAPS[state];
    const k = Math.min(1, Math.max(0, (now - this.changedAt) / WING_EASE_MS));
    const toBeat = 'beatMs' in to ? to.beatMs : this.from.beatMs;
    this.now.beatMs = this.from.beatMs + (toBeat - this.from.beatMs) * k;
    this.now.swing = this.from.swing + (to.swing - this.from.swing) * k;
    this.now.open = this.from.open + (to.open - this.from.open) * k;
    this.phase = (this.phase + dtMs / this.now.beatMs) % 1;
    this.turn();
  }

  /** Turns each half back from spread flat by its open angle less 90, about the chest's up axis; a swing never takes it past either. */
  private turn(): void {
    const swung = this.now.open + this.now.swing * Math.sin(2 * Math.PI * this.phase);
    const open = Math.min(90, Math.max(0, swung));
    for (const h of this.hinges) {
      h.node.rotation.y = h.side * (90 - open) * RAD;
    }
  }
}
