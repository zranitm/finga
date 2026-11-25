// The scene's materials (3d-world.md section 11): one toon NodeMaterial per base colour or texture, its light posterized into flat bands, the sun's or a cave's torches', an inverted hull for the ink line, and an unlit glow; all take the scene's fog.
import { Constants } from '@babylonjs/core/Engines/constants';
import type { Material } from '@babylonjs/core/Materials/material';
import { AddBlock } from '@babylonjs/core/Materials/Node/Blocks/addBlock';
import { ClampBlock } from '@babylonjs/core/Materials/Node/Blocks/clampBlock';
import { ColorSplitterBlock } from '@babylonjs/core/Materials/Node/Blocks/colorSplitterBlock';
import { DivideBlock } from '@babylonjs/core/Materials/Node/Blocks/divideBlock';
import { DotBlock } from '@babylonjs/core/Materials/Node/Blocks/dotBlock';
import { FogBlock } from '@babylonjs/core/Materials/Node/Blocks/Dual/fogBlock';
import { LightBlock } from '@babylonjs/core/Materials/Node/Blocks/Dual/lightBlock';
import { TextureBlock } from '@babylonjs/core/Materials/Node/Blocks/Dual/textureBlock';
import { DiscardBlock } from '@babylonjs/core/Materials/Node/Blocks/Fragment/discardBlock';
import { FragCoordBlock } from '@babylonjs/core/Materials/Node/Blocks/Fragment/fragCoordBlock';
import { FragmentOutputBlock } from '@babylonjs/core/Materials/Node/Blocks/Fragment/fragmentOutputBlock';
import { FrontFacingBlock } from '@babylonjs/core/Materials/Node/Blocks/Fragment/frontFacingBlock';
import { AnimatedInputBlockTypes } from '@babylonjs/core/Materials/Node/Blocks/Input/animatedInputBlockTypes';
import { InputBlock } from '@babylonjs/core/Materials/Node/Blocks/Input/inputBlock';
import { LengthBlock } from '@babylonjs/core/Materials/Node/Blocks/lengthBlock';
import { LerpBlock } from '@babylonjs/core/Materials/Node/Blocks/lerpBlock';
import { MaxBlock } from '@babylonjs/core/Materials/Node/Blocks/maxBlock';
import { MinBlock } from '@babylonjs/core/Materials/Node/Blocks/minBlock';
import { ModBlock } from '@babylonjs/core/Materials/Node/Blocks/modBlock';
import { MultiplyBlock } from '@babylonjs/core/Materials/Node/Blocks/multiplyBlock';
import { NormalizeBlock } from '@babylonjs/core/Materials/Node/Blocks/normalizeBlock';
import { OneMinusBlock } from '@babylonjs/core/Materials/Node/Blocks/oneMinusBlock';
import { PosterizeBlock } from '@babylonjs/core/Materials/Node/Blocks/posterizeBlock';
import { ScaleBlock } from '@babylonjs/core/Materials/Node/Blocks/scaleBlock';
import { SmoothStepBlock } from '@babylonjs/core/Materials/Node/Blocks/smoothStepBlock';
import { StepBlock } from '@babylonjs/core/Materials/Node/Blocks/stepBlock';
import { SubtractBlock } from '@babylonjs/core/Materials/Node/Blocks/subtractBlock';
import { TransformBlock } from '@babylonjs/core/Materials/Node/Blocks/transformBlock';
import {
  TrigonometryBlock,
  TrigonometryBlockOperations,
} from '@babylonjs/core/Materials/Node/Blocks/trigonometryBlock';
import { VectorMergerBlock } from '@babylonjs/core/Materials/Node/Blocks/vectorMergerBlock';
import { VectorSplitterBlock } from '@babylonjs/core/Materials/Node/Blocks/vectorSplitterBlock';
import { BonesBlock } from '@babylonjs/core/Materials/Node/Blocks/Vertex/bonesBlock';
import { InstancesBlock } from '@babylonjs/core/Materials/Node/Blocks/Vertex/instancesBlock';
import { VertexOutputBlock } from '@babylonjs/core/Materials/Node/Blocks/Vertex/vertexOutputBlock';
import { NodeMaterialBlockConnectionPointTypes } from '@babylonjs/core/Materials/Node/Enums/nodeMaterialBlockConnectionPointTypes';
import { NodeMaterialSystemValues } from '@babylonjs/core/Materials/Node/Enums/nodeMaterialSystemValues';
import { NodeMaterial } from '@babylonjs/core/Materials/Node/nodeMaterial';
import type { NodeMaterialConnectionPoint } from '@babylonjs/core/Materials/Node/nodeMaterialBlockConnectionPoint';
import type { Texture } from '@babylonjs/core/Materials/Textures/texture';
import { Color3 } from '@babylonjs/core/Maths/math.color';
import { Vector2, Vector3, Vector4 } from '@babylonjs/core/Maths/math.vector';
import type { AbstractMesh } from '@babylonjs/core/Meshes/abstractMesh';
import { Mesh } from '@babylonjs/core/Meshes/mesh';
import { SubMesh } from '@babylonjs/core/Meshes/subMesh';
import type { Scene } from '@babylonjs/core/scene';

import { CUT_MOBS } from './cut-mobs';
import { clearsMark, GHOST, markOthers, onPlace } from './see-through';
import { FLICKERS, FLOOR, TORCH } from './torchlight';
import type { Torchlight } from './torchlight';

/** The light bands a surface steps through: shade, mid and lit; in shade it keeps this share of its colour, a cool grey. */
const BANDS = 3;
const SHADE = new Color3(0.5, 0.5, 0.58).toLinearSpace();
/** A hollow model's inside, which a cut shows: its colour in the shade band, darkened this much again, unlit. */
const INSIDE = 0.6;
/** The ink line's width in metres, pushed out after skinning, and its colour. */
const OUTLINE_M = 0.03;
const INK = new Color3(0.07, 0.06, 0.09);
/** The cut-away round the player (3d-world.md section 10): the place opens where it's this far nearer the camera than the chest, fully within CUT_INNER m of it on screen and dithered out to CUT_OUTER m; below the chest, the way to the feet counts CUT_BELOW times, so walkable floor rising toward the camera stays. */
const CUT_NEARER = 0.5;
const CUT_INNER = 1;
const CUT_OUTER = 1.4;
const CUT_BELOW = 1.5;
/** And round each mob the world picks (cut-mobs.ts): in front of its middle where the place is over CUT_MOB_OVER m above its feet, as a bank's top is but no walkable floor near it, fully within its radius and CUT_MOB_PAD m on screen and dithered CUT_EDGE m further. */
const CUT_MOB_OVER = 1.5;
const CUT_MOB_PAD = 0.5;
const CUT_EDGE = 0.4;
const CUT_OFF = -1; // an empty slot's reach, which opens nothing

/** What a cut material reads each frame: the chest it opens round, in the world, and 1 once there is one; and each mob slot's middle and reach, and the height the place must be over there. */
interface Cut {
  readonly chest: Vector3;
  on: number;
  readonly mobs: readonly Vector4[];
  readonly over: number[];
}

/** A body the place opens round besides the player: its feet in the world, its radius and height. */
export interface Opening {
  readonly x: number;
  readonly y: number;
  readonly z: number;
  readonly radius: number;
  readonly height: number;
}

export interface ToonBase {
  readonly texture: Texture | null;
  readonly color: Color3; // linear, as glTF writes it
  readonly glow?: boolean; // drawn in the lit band only, with a rim, as prestige wings are
  readonly vertexColors?: boolean; // times each vertex's colour, linear, as a region's ground is
}

const RIM_FROM = 0.45; // how far a surface turns from the camera before a glow's rim starts
const RIM = 0.7; // and how much white the rim adds where the surface is edge-on

/** The vertex half both materials share: a skinned position and normal in the world, a thin instance's matrix under the mesh's, out to clip space. */
class Skinned {
  readonly worldPos = new TransformBlock('worldPos');
  readonly worldNormal = new TransformBlock('worldNormal');
  readonly vertex = new VertexOutputBlock('vertexOutput');

  constructor() {
    const instances = new InstancesBlock('instances');
    this.attribute('world0').output.connectTo(instances.world0);
    this.attribute('world1').output.connectTo(instances.world1);
    this.attribute('world2').output.connectTo(instances.world2);
    this.attribute('world3').output.connectTo(instances.world3);
    this.system('world', NodeMaterialSystemValues.World).output.connectTo(instances.world);
    const bones = new BonesBlock('bones');
    this.attribute('matricesIndices').output.connectTo(bones.matricesIndices);
    this.attribute('matricesWeights').output.connectTo(bones.matricesWeights);
    instances.output.connectTo(bones.world);
    this.attribute('position').output.connectTo(this.worldPos.vector);
    bones.output.connectTo(this.worldPos.transform);
    this.worldNormal.complementW = 0;
    this.attribute('normal').output.connectTo(this.worldNormal.vector);
    bones.output.connectTo(this.worldNormal.transform);
  }

  attribute(name: string): InputBlock {
    const b = new InputBlock(name);
    b.setAsAttribute(name);
    return b;
  }

  system(name: string, v: NodeMaterialSystemValues): InputBlock {
    const b = new InputBlock(name);
    b.setAsSystemValue(v);
    return b;
  }

  /** Draws the vertex at world, a point in the world. */
  place(world: NodeMaterialConnectionPoint): void {
    const clip = new TransformBlock('clipPos');
    world.connectTo(clip.vector);
    this.system('viewProjection', NodeMaterialSystemValues.ViewProjection).output.connectTo(
      clip.transform,
    );
    clip.output.connectTo(this.vertex.vector);
  }
}

/** A float input set to v. */
function constant(name: string, v: number): InputBlock {
  const b = new InputBlock(name);
  b.value = v;
  return b;
}

/** How much cut opens a fragment, over CUT_NEARER m nearer the camera than its chest and near it on screen, measured at the chest's depth, or round a mob; and the pixel's noise, from 0 up to 1: the fragment goes where the noise falls under the share, so between CUT_INNER and CUT_OUTER m a dither thins as it goes out. */
function cutShare(
  s: Skinned,
  cut: Cut,
): {
  share: NodeMaterialConnectionPoint;
  seen: NodeMaterialConnectionPoint;
  noise: NodeMaterialConnectionPoint;
} {
  const view = s.system('view', NodeMaterialSystemValues.View);
  const fragment = new TransformBlock('cutFragment');
  s.worldPos.output.connectTo(fragment.vector);
  view.output.connectTo(fragment.transform);
  const chest = new InputBlock(
    'cutChest',
    undefined,
    NodeMaterialBlockConnectionPointTypes.Vector3,
  );
  chest.valueCallback = () => cut.chest;
  const chestView = new TransformBlock('cutChestView');
  chest.output.connectTo(chestView.vector);
  view.output.connectTo(chestView.transform);
  const f = new VectorSplitterBlock('cutF');
  fragment.output.connectTo(f.xyzw);
  const c = new VectorSplitterBlock('cutC');
  chestView.output.connectTo(c.xyzw);

  const nearer = new SubtractBlock('cutNearerBy');
  c.z.connectTo(nearer.left);
  f.z.connectTo(nearer.right);
  const isNearer = new StepBlock('cutIsNearer');
  nearer.output.connectTo(isNearer.value);
  constant('cutNearer', CUT_NEARER).output.connectTo(isNearer.edge);

  const depth = new MaxBlock('cutDepth'); // in front of the camera, as anything drawn is
  f.z.connectTo(depth.left);
  constant('cutNear', 0.01).output.connectTo(depth.right);
  const zero = constant('cutZero', 0);
  const open = hole('cut', f, depth, c, zero, {
    inner: constant('cutInner', CUT_INNER).output,
    outer: constant('cutOuter', CUT_OUTER).output,
    below: constant('cutBelow', CUT_BELOW).output,
  });

  const on = new InputBlock('cutOn', undefined, NodeMaterialBlockConnectionPointTypes.Float);
  on.valueCallback = () => cut.on;
  const where = new MultiplyBlock('cutWhere');
  open.output.connectTo(where.left);
  isNearer.output.connectTo(where.right);
  const share = new MultiplyBlock('cutShare');
  where.output.connectTo(share.left);
  on.output.connectTo(share.right);

  const world = new VectorSplitterBlock('cutWorld');
  s.worldPos.output.connectTo(world.xyzw);
  const edgeBy = constant('cutEdgeBy', CUT_EDGE);
  let most: NodeMaterialConnectionPoint = share.output;
  const chestSeen = new MultiplyBlock('cutChestSeen'); // the chest's share wherever the fragment stands
  open.output.connectTo(chestSeen.left);
  on.output.connectTo(chestSeen.right);
  let seen: NodeMaterialConnectionPoint = chestSeen.output;
  for (let i = 0; i < CUT_MOBS; i++) {
    const name = `cutMob${i}`;
    const slot = new InputBlock(name, undefined, NodeMaterialBlockConnectionPointTypes.Vector4);
    slot.valueCallback = () => cut.mobs[i]!;
    const at = new VectorSplitterBlock(`${name}At`);
    slot.output.connectTo(at.xyzw);
    const middle = new TransformBlock(`${name}View`);
    at.xyzOut.connectTo(middle.vector);
    view.output.connectTo(middle.transform);
    const m = new VectorSplitterBlock(`${name}M`);
    middle.output.connectTo(m.xyzw);
    const outer = new AddBlock(`${name}Outer`);
    at.w.connectTo(outer.left);
    edgeBy.output.connectTo(outer.right);
    const mobOpen = hole(name, f, depth, m, zero, {
      inner: at.w,
      outer: outer.output,
      below: null,
    });

    const by = new SubtractBlock(`${name}By`);
    m.z.connectTo(by.left);
    f.z.connectTo(by.right);
    const front = new StepBlock(`${name}Front`); // nearer the camera than its middle
    by.output.connectTo(front.value);
    zero.output.connectTo(front.edge);
    const over = new InputBlock(
      `${name}Over`,
      undefined,
      NodeMaterialBlockConnectionPointTypes.Float,
    );
    over.valueCallback = () => cut.over[i]!;
    const above = new StepBlock(`${name}Above`);
    world.y.connectTo(above.value);
    over.output.connectTo(above.edge);
    const inFront = new MultiplyBlock(`${name}InFront`);
    front.output.connectTo(inFront.left);
    above.output.connectTo(inFront.right);
    const mobShare = new MultiplyBlock(`${name}Share`);
    mobOpen.output.connectTo(mobShare.left);
    inFront.output.connectTo(mobShare.right);
    const more = new MaxBlock(`cutMost${i}`);
    most.connectTo(more.left);
    mobShare.output.connectTo(more.right);
    most = more.output;
    const moreSeen = new MaxBlock(`cutSeen${i}`); // at any height: a ray down through a front cut above it meets the far wall lower
    seen.connectTo(moreSeen.left);
    mobOpen.output.connectTo(moreSeen.right);
    seen = moreSeen.output;
  }

  // interleaved gradient noise on the pixel, from 0 up to 1: a pixel goes where it falls under the share to drop
  const pixel = new FragCoordBlock('cutPixel');
  const weights = new InputBlock('cutWeights');
  weights.value = new Vector2(0.06711056, 0.00583715);
  const mixed = new DotBlock('cutMixed');
  pixel.xy.connectTo(mixed.left);
  weights.output.connectTo(mixed.right);
  const one = constant('cutOne', 1);
  const inner = new ModBlock('cutInnerFract');
  mixed.output.connectTo(inner.left);
  one.output.connectTo(inner.right);
  const spread = new ScaleBlock('cutSpread');
  inner.output.connectTo(spread.input);
  constant('cutSpreadBy', 52.9829189).output.connectTo(spread.factor);
  const noise = new ModBlock('cutNoise');
  spread.output.connectTo(noise.left);
  one.output.connectTo(noise.right);
  return { share: most, seen, noise: noise.output };
}

/** Drops the fragments cut opens: the place's; and with facing, a hollow model's back faces but where an opening shows them, so its silhouettes stay as they were. */
function cutAway(s: Skinned, cut: Cut, facing: NodeMaterialConnectionPoint | null): DiscardBlock[] {
  const { share, seen, noise } = cutShare(s, cut);
  const discard = new DiscardBlock('cut');
  noise.connectTo(discard.value);
  share.connectTo(discard.cutoff);
  if (!facing) {
    return [discard];
  }
  const hidden = new StepBlock('cutHidden'); // 1 where no opening would drop a fragment nearer than this one
  noise.connectTo(hidden.value);
  seen.connectTo(hidden.edge);
  const shown = new OneMinusBlock('cutShown');
  hidden.output.connectTo(shown.input);
  const kept = new AddBlock('cutBackKept'); // 0 only on a back face no opening shows
  facing.connectTo(kept.left);
  shown.output.connectTo(kept.right);
  const back = new DiscardBlock('cutBack');
  kept.output.connectTo(back.value);
  constant('cutBackKeep', 0.5).output.connectTo(back.cutoff);
  return [discard, back];
}

/** 1 where cut keeps a fragment, GHOST where it drops it: a decal's strength, so its part on opened place shows as faintly as its ghost does where the place hides it. */
function cutFaint(s: Skinned, cut: Cut): NodeMaterialConnectionPoint {
  const { share, noise } = cutShare(s, cut);
  const kept = new StepBlock('cutKept'); // where the place's discard wouldn't go
  noise.connectTo(kept.value);
  share.connectTo(kept.edge);
  const faint = new LerpBlock('cutFaint');
  constant('cutGhost', GHOST).output.connectTo(faint.left);
  constant('cutWhole', 1).output.connectTo(faint.right);
  kept.output.connectTo(faint.gradient);
  return faint.output;
}

/** How much a hole round c opens the fragment f, both in the camera's view: 1 within inner of c on screen, at c's depth, falling to 0 at outer; below c, with below, the way down counts that many times. */
function hole(
  name: string,
  f: VectorSplitterBlock,
  depth: MaxBlock,
  c: VectorSplitterBlock,
  zero: InputBlock,
  reach: {
    readonly inner: NodeMaterialConnectionPoint;
    readonly outer: NodeMaterialConnectionPoint;
    readonly below: NodeMaterialConnectionPoint | null;
  },
): OneMinusBlock {
  const toC = new DivideBlock(`${name}ToC`);
  c.z.connectTo(toC.left);
  depth.output.connectTo(toC.right);
  const across = new ScaleBlock(`${name}Across`); // the fragment's place on screen, as far off as c
  f.xyOut.connectTo(across.input);
  toC.output.connectTo(across.factor);
  const off = new SubtractBlock(`${name}Off`);
  across.output.connectTo(off.left);
  c.xyOut.connectTo(off.right);
  let flat = off.output;
  if (reach.below) {
    const o = new VectorSplitterBlock(`${name}O`);
    off.output.connectTo(o.xyIn);
    const up = new MaxBlock(`${name}Up`); // how far above c, or 0 below it
    o.y.connectTo(up.left);
    zero.output.connectTo(up.right);
    const down = new MinBlock(`${name}Down`); // how far below, or 0 above
    o.y.connectTo(down.left);
    zero.output.connectTo(down.right);
    const further = new ScaleBlock(`${name}Further`);
    down.output.connectTo(further.input);
    reach.below.connectTo(further.factor);
    const y = new AddBlock(`${name}Y`);
    up.output.connectTo(y.left);
    further.output.connectTo(y.right);
    const squashed = new VectorMergerBlock(`${name}Squashed`);
    o.x.connectTo(squashed.x);
    y.output.connectTo(squashed.y);
    flat = squashed.xy;
  }
  const away = new LengthBlock(`${name}Away`);
  flat.connectTo(away.value);
  const edge = new SmoothStepBlock(`${name}Edge`);
  away.output.connectTo(edge.value);
  reach.inner.connectTo(edge.edge0);
  reach.outer.connectTo(edge.edge1);
  const open = new OneMinusBlock(`${name}Open`);
  edge.output.connectTo(open.input);
  return open;
}

/** rgb, a linear colour, fogged by its distance from the camera. */
function fogged(s: Skinned, rgb: NodeMaterialConnectionPoint): NodeMaterialConnectionPoint {
  const fog = new FogBlock('fog');
  s.worldPos.output.connectTo(fog.worldPosition);
  s.system('view', NodeMaterialSystemValues.View).output.connectTo(fog.view);
  rgb.connectTo(fog.input);
  const fogColor = new InputBlock(
    'fogColor',
    undefined,
    NodeMaterialBlockConnectionPointTypes.Color3,
  );
  fogColor.setAsSystemValue(NodeMaterialSystemValues.FogColor);
  fogColor.output.connectTo(fog.fogColor);
  return fog.output;
}

/** Finishes m: rgb, a linear colour, fogged by its distance from the camera, out in gamma space at alpha; the place's clears the stencil for ghosts and decals, and with cut opens round the player's chest. */
function built(
  m: NodeMaterial,
  s: Skinned,
  rgb: NodeMaterialConnectionPoint,
  alpha = 1,
  cut: Cut | null = null,
  place = cut !== null,
  facing: NodeMaterialConnectionPoint | null = null,
) {
  const fragment = new FragmentOutputBlock('fragmentOutput');
  fragment.convertToGammaSpace = true;
  fogged(s, rgb).connectTo(fragment.rgb);
  if (alpha < 1) {
    const a = new InputBlock('alpha');
    a.value = alpha;
    a.output.connectTo(fragment.a);
  }
  m.addOutputNode(s.vertex);
  m.addOutputNode(fragment);
  if (cut) {
    for (const d of cutAway(s, cut, facing)) {
      m.addOutputNode(d);
    }
  }
  if (place) {
    clearsMark(m);
  }
  m.build();
  return m;
}

/** The sun's diffuse light at the fragment: every light on the mesh, the sun alone here. */
function sunlight(s: Skinned): NodeMaterialConnectionPoint {
  const light = new LightBlock('light');
  const normal = new NormalizeBlock('normal'); // the world matrix carries the model's scale
  s.worldNormal.output.connectTo(normal.input);
  s.worldPos.output.connectTo(light.worldPosition);
  normal.output.connectTo(light.worldNormal);
  s.system('cameraPosition', NodeMaterialSystemValues.CameraPosition).output.connectTo(
    light.cameraPosition,
  );
  return light.diffuseOutput;
}

/** A cave's light at the fragment: the light map at its x and z, each group's times its flicker, their sum held to 1. */
function torchlight(s: Skinned, t: Torchlight): NodeMaterialConnectionPoint {
  const at = new VectorSplitterBlock('at');
  s.worldPos.output.connectTo(at.xyzw);
  const xz = new VectorMergerBlock('xz');
  at.x.connectTo(xz.x);
  at.z.connectTo(xz.y);
  const size = new InputBlock('lightSize');
  size.value = new Vector2(...t.size);
  const uv = new DivideBlock('lightUv');
  xz.xyOut.connectTo(uv.left);
  size.output.connectTo(uv.right);
  const map = new TextureBlock('lightMap');
  map.texture = t.texture;
  uv.output.connectTo(map.uv);
  const time = new InputBlock('time', undefined, NodeMaterialBlockConnectionPointTypes.Float);
  time.animationType = AnimatedInputBlockTypes.RealTime;
  const lit = [map.r, map.g].map((group, g) => {
    const lit = new MultiplyBlock(`group${g}`);
    group.connectTo(lit.left);
    flickering(g, time.output).connectTo(lit.right);
    return lit.output;
  });
  const sum = new AddBlock('torches');
  lit[0]!.connectTo(sum.left);
  lit[1]!.connectTo(sum.right);
  const held = new MinBlock('torchlight');
  sum.output.connectTo(held.left);
  constant('full', 1).output.connectTo(held.right);
  return held.output;
}

/** Group's flicker at time, in seconds: 1 plus each of its sines. */
function flickering(group: number, time: NodeMaterialConnectionPoint): NodeMaterialConnectionPoint {
  let f: NodeMaterialConnectionPoint = constant(`flicker${group}`, 1).output;
  FLICKERS[group]!.forEach(({ amp, rate, phase }, k) => {
    const turned = new ScaleBlock(`turned${group}${k}`);
    time.connectTo(turned.input);
    constant(`rate${group}${k}`, rate).output.connectTo(turned.factor);
    const phased = new AddBlock(`phased${group}${k}`);
    turned.output.connectTo(phased.left);
    constant(`phase${group}${k}`, phase).output.connectTo(phased.right);
    const sine = new TrigonometryBlock(`sine${group}${k}`);
    sine.operation = TrigonometryBlockOperations.Sin;
    phased.output.connectTo(sine.input);
    const swing = new ScaleBlock(`swing${group}${k}`);
    sine.output.connectTo(swing.input);
    constant(`amp${group}${k}`, amp).output.connectTo(swing.factor);
    const next = new AddBlock(`flickered${group}${k}`);
    f.connectTo(next.left);
    swing.output.connectTo(next.right);
    f = next.output;
  });
  return f;
}

function toonMaterial(
  name: string,
  scene: Scene,
  base: ToonBase,
  alpha: number,
  cut: Cut | null,
  place = cut !== null,
  hollow = false,
  torches: Torchlight | null = null,
): NodeMaterial {
  const s = new Skinned();
  s.place(s.worldPos.output);

  // Posterize floors the light into 0, ⅓ and ⅔; stretched to 0, ½ and 1, the top band is the colour itself, or in a cave the torches' warm light.
  const steps = new InputBlock('bands');
  steps.value = BANDS;
  const floored = new PosterizeBlock('floored');
  (torches ? torchlight(s, torches) : sunlight(s)).connectTo(floored.value);
  steps.output.connectTo(floored.steps);
  const stretch = new InputBlock('stretch');
  stretch.value = BANDS / (BANDS - 1);
  const stretched = new ScaleBlock('stretched');
  floored.output.connectTo(stretched.input);
  stretch.output.connectTo(stretched.factor);
  const banded = new ClampBlock('banded');
  stretched.output.connectTo(banded.value);
  const shade = new InputBlock('shade');
  shade.value = torches ? FLOOR : SHADE;
  const lit = new InputBlock('lit');
  lit.value = Color3.White();
  const tone = new LerpBlock('tone');
  shade.output.connectTo(tone.left);
  if (torches) {
    const torch = new InputBlock('torch');
    torch.value = TORCH;
    torch.output.connectTo(tone.right);
  } else {
    lit.output.connectTo(tone.right);
  }
  banded.output.connectTo(tone.gradient);

  const color = new InputBlock('color');
  color.value = base.color;
  const albedo = new MultiplyBlock('albedo');
  if (base.texture) {
    const texture = new TextureBlock('texture');
    texture.texture = base.texture;
    texture.convertToLinearSpace = true; // a no-op for an sRGB buffer, which samples linear already
    s.attribute('uv').output.connectTo(texture.uv);
    texture.rgb.connectTo(albedo.left);
  } else if (base.vertexColors) {
    const vertex = new ColorSplitterBlock('vertexColor');
    s.attribute('color').output.connectTo(vertex.rgba);
    vertex.rgbOut.connectTo(albedo.left);
  } else {
    lit.output.connectTo(albedo.left);
  }
  color.output.connectTo(albedo.right);

  const shaded = new MultiplyBlock('shaded');
  albedo.output.connectTo(shaded.left);
  tone.output.connectTo(shaded.right);
  const m = new NodeMaterial(name, scene, { emitComments: false });
  let rgb = base.glow ? rimmed(s, albedo.output, lit.output) : shaded.output;
  let facing: NodeMaterialConnectionPoint | null = null;
  if (hollow) {
    facing = facesCamera();
    rgb = inside(facing, albedo.output, shade.output, rgb);
    m.backFaceCulling = false;
  }
  return built(m, s, rgb, alpha, cut, place, facing);
}

/** 1 on a face turned to the camera, 0 on one turned away, by the triangle's winding, as culling judges it: exact at a silhouette, where a smooth normal can already turn away; gl_FrontFacing counts the other way under Babylon's clockwise front faces. */
function facesCamera(): NodeMaterialConnectionPoint {
  const gl = new FrontFacingBlock('glFrontFacing');
  const front = new OneMinusBlock('front');
  gl.output.connectTo(front.input);
  return front.output;
}

/** A hollow model's colour: rgb on the faces turned to the camera, and on those turned away, its inside an opening shows, albedo in the shade band darkened by INSIDE. */
function inside(
  facing: NodeMaterialConnectionPoint,
  albedo: NodeMaterialConnectionPoint,
  shade: NodeMaterialConnectionPoint,
  rgb: NodeMaterialConnectionPoint,
): NodeMaterialConnectionPoint {
  const shaded = new MultiplyBlock('insideShaded');
  albedo.connectTo(shaded.left);
  shade.connectTo(shaded.right);
  const dark = new ScaleBlock('inside');
  shaded.output.connectTo(dark.input);
  constant('insideBy', INSIDE).output.connectTo(dark.factor);
  const out = new LerpBlock('insideOrOut');
  dark.output.connectTo(out.left);
  rgb.connectTo(out.right);
  facing.connectTo(out.gradient);
  return out.output;
}

/** A glow's colour: albedo in the lit band whatever the light, with white added toward the edges the camera sees side-on. */
function rimmed(
  s: Skinned,
  albedo: NodeMaterialConnectionPoint,
  white: NodeMaterialConnectionPoint,
): NodeMaterialConnectionPoint {
  const toCamera = new SubtractBlock('toCamera');
  s.system('cameraPosition', NodeMaterialSystemValues.CameraPosition).output.connectTo(
    toCamera.left,
  );
  s.worldPos.xyz.connectTo(toCamera.right);
  const view = new NormalizeBlock('view');
  toCamera.output.connectTo(view.input);
  const facing = new NormalizeBlock('facing');
  s.worldNormal.xyz.connectTo(facing.input);
  const dot = new DotBlock('facingDot');
  view.output.connectTo(dot.left);
  facing.output.connectTo(dot.right);
  const edge = new OneMinusBlock('edge');
  dot.output.connectTo(edge.input);
  const from = new InputBlock('rimFrom');
  from.value = RIM_FROM;
  const to = new InputBlock('rimTo');
  to.value = 1;
  const soft = new SmoothStepBlock('rim');
  edge.output.connectTo(soft.value);
  from.output.connectTo(soft.edge0);
  to.output.connectTo(soft.edge1);
  const strength = new InputBlock('rimStrength');
  strength.value = RIM;
  const amount = new MultiplyBlock('rimAmount');
  soft.output.connectTo(amount.left);
  strength.output.connectTo(amount.right);
  const light = new ScaleBlock('rimLight');
  white.connectTo(light.input);
  amount.output.connectTo(light.factor);
  const out = new AddBlock('glowed');
  albedo.connectTo(out.left);
  light.output.connectTo(out.right);
  return out.output;
}

/** The ink: back faces only, pushed out along their normals by OUTLINE_M in the world, so every model's line is the same width. */
function inkMaterial(scene: Scene, cut: Cut | null): NodeMaterial {
  const s = new Skinned();
  const unit = new NormalizeBlock('unit');
  s.worldNormal.xyz.connectTo(unit.input);
  const width = new InputBlock('width');
  width.value = OUTLINE_M;
  const push = new ScaleBlock('push');
  unit.output.connectTo(push.input);
  width.output.connectTo(push.factor);
  const out = new AddBlock('out');
  s.worldPos.xyz.connectTo(out.left);
  push.output.connectTo(out.right);
  s.place(out.output);
  const ink = new InputBlock('ink');
  ink.value = INK.toLinearSpace(); // out in gamma space again, as INK
  const m = new NodeMaterial(cut ? 'ink cut' : 'ink', scene, { emitComments: false });
  m.cullBackFaces = false;
  return built(m, s, ink.output, 1, cut);
}

/** An unlit colour at alpha, lit by nothing, as a portal's glow. */
function glowMaterial(name: string, scene: Scene, color: Color3, alpha: number): NodeMaterial {
  const s = new Skinned();
  s.place(s.worldPos.output);
  const c = new InputBlock('color');
  c.value = color;
  const m = new NodeMaterial(name, scene, { emitComments: false });
  m.backFaceCulling = false;
  return built(m, s, c.output, alpha);
}

const DISC_DARK = 0.55; // a round shadow's darkness at its middle
const DISC_SOFT = 0.35; // and the share of its radius it holds that before fading to nothing at its rim
const GLOW = new Color3(1, 0.55, 0.18).toLinearSpace(); // a flame's glow card, sRGB, the torches' orange
const GLOW_PEAK = 0.5; // its strength at its middle, fading from it to nothing at its rim

/** A soft disc of color, a linear colour, peak at its middle, held to soft of its radius and fading to nothing at its rim; a disc's or plane's uvs run 0 to 1 across it. Fogged, so a glow fades into a cave's black with the flame it's behind; it writes no depth, and blends by mode. */
function discMaterial(
  name: string,
  scene: Scene,
  color: Color3,
  peak: number,
  soft: number,
  mode: number,
): NodeMaterial {
  const s = new Skinned();
  s.place(s.worldPos.output);
  const middle = new InputBlock('middle');
  middle.value = new Vector2(0.5, 0.5);
  const off = new SubtractBlock('off');
  s.attribute('uv').output.connectTo(off.left);
  middle.output.connectTo(off.right);
  const away = new LengthBlock('away');
  off.output.connectTo(away.value);
  const rim = new ScaleBlock('rimShare');
  away.output.connectTo(rim.input);
  constant('toRim', 2).output.connectTo(rim.factor);
  const fade = new SmoothStepBlock('fade');
  rim.output.connectTo(fade.value);
  constant('soft', soft).output.connectTo(fade.edge0);
  constant('edge', 1).output.connectTo(fade.edge1);
  const held = new OneMinusBlock('held');
  fade.output.connectTo(held.input);
  const alpha = new ScaleBlock('discAlpha');
  held.output.connectTo(alpha.input);
  constant('peak', peak).output.connectTo(alpha.factor);
  const c = new InputBlock('color');
  c.value = color;
  const fragment = new FragmentOutputBlock('fragmentOutput');
  fragment.convertToGammaSpace = true;
  fogged(s, c.output).connectTo(fragment.rgb);
  alpha.output.connectTo(fragment.a);
  const m = new NodeMaterial(name, scene, { emitComments: false });
  m.addOutputNode(s.vertex);
  m.addOutputNode(fragment);
  m.build();
  m.backFaceCulling = false;
  m.forceAlphaBlending = true;
  m.alphaMode = mode;
  m.disableDepthWrite = true;
  return m;
}

/** base's texture times its colour at full strength, unlit, fogged: a torch's flame. */
function unlitMaterial(name: string, scene: Scene, base: ToonBase): NodeMaterial {
  const s = new Skinned();
  s.place(s.worldPos.output);
  const color = new InputBlock('color');
  color.value = base.color;
  const albedo = new MultiplyBlock('albedo');
  if (base.texture) {
    const texture = new TextureBlock('texture');
    texture.texture = base.texture;
    texture.convertToLinearSpace = true;
    s.attribute('uv').output.connectTo(texture.uv);
    texture.rgb.connectTo(albedo.left);
  } else {
    constant('white', 1).output.connectTo(albedo.left);
  }
  color.output.connectTo(albedo.right);
  return built(new NodeMaterial(name, scene, { emitComments: false }), s, albedo.output);
}

/** An unlit colour, as written, at alpha: a decal laid on the place, faint where cut opens the place; a premultiplied one's colour fades there with its alpha. */
function decalMaterial(
  name: string,
  scene: Scene,
  color: Color3,
  alpha: number,
  premultiplied: boolean,
  cut: Cut,
): NodeMaterial {
  const s = new Skinned();
  s.place(s.worldPos.output);
  const c = new InputBlock('color');
  c.value = color;
  const strength = cutFaint(s, cut);
  const fragment = new FragmentOutputBlock('fragmentOutput');
  if (premultiplied) {
    const light = new MultiplyBlock('faintColor');
    c.output.connectTo(light.left);
    strength.connectTo(light.right);
    light.output.connectTo(fragment.rgb);
  } else {
    c.output.connectTo(fragment.rgb);
  }
  const a = new InputBlock('alpha');
  a.value = alpha;
  const faded = new MultiplyBlock('faintAlpha');
  a.output.connectTo(faded.left);
  strength.connectTo(faded.right);
  faded.output.connectTo(fragment.a);
  const m = new NodeMaterial(name, scene, { emitComments: false });
  m.addOutputNode(s.vertex);
  m.addOutputNode(fragment);
  m.build();
  return m;
}

/** A scene's materials: toon ones by texture and colour, shared by every mesh drawn with them, and the ink; the place's open round the player's chest, and clear the stencil every other mesh marks. */
export class Palette {
  private readonly made = new Map<string, NodeMaterial>();
  private readonly inks = new Map<boolean, NodeMaterial>();
  private readonly cut: Cut = {
    chest: new Vector3(),
    on: 0,
    mobs: Array.from({ length: CUT_MOBS }, () => new Vector4(0, 0, 0, CUT_OFF)),
    over: Array.from({ length: CUT_MOBS }, () => 0),
  };

  /** A scene's palette, its toon materials lit by a cave's torches when given them, else by the sun. */
  constructor(
    private readonly scene: Scene,
    private readonly torches: Torchlight | null = null,
  ) {
    markOthers(scene);
  }

  /** The toon material for base, drawn at alpha: 1, or less for a solid faded out of the camera's way; cut for the place, which opens round the player; a hollow one, a model the place stands, shows a dark inside where it's cut whole. */
  toon(base: ToonBase, alpha = 1, cut = false, hollow = false): NodeMaterial {
    const color = base.color.clampToRef(0, 1, new Color3());
    const inside = hollow && cut && alpha === 1;
    const key = `${base.texture?.uniqueId ?? '-'} ${color.toHexString()}${base.glow ? ' glow' : ''}${base.vertexColors ? ' vertex' : ''}${cut ? ' cut' : ''}${inside ? ' hollow' : ''}${alpha < 1 ? ` ${alpha}` : ''}`;
    let m = this.made.get(key);
    if (!m) {
      m = toonMaterial(
        `toon ${key}`,
        this.scene,
        { ...base, color },
        alpha,
        cut ? this.cut : null,
        cut,
        inside,
        this.torches,
      );
      this.made.set(key, m);
    }
    return m;
  }

  /** The toon material for the flat ground round and under the map: the place's, so a ghost or a faint decal draws on it where a hole shows it, but never cut, as only the void lies behind it. */
  apron(base: ToonBase): NodeMaterial {
    const color = base.color.clampToRef(0, 1, new Color3());
    return toonMaterial(
      `toon apron ${color.toHexString()}`,
      this.scene,
      { texture: base.texture, color },
      1,
      null,
      true,
      false,
      this.torches,
    );
  }

  /** Opens the place round the player's chest, a point in the world, and round the first CUT_MOBS of mobs. */
  cutAround(x: number, y: number, z: number, mobs: readonly Opening[]): void {
    this.cut.chest.set(x, y, z);
    this.cut.on = 1;
    this.cut.mobs.forEach((slot, i) => {
      const m = mobs[i];
      slot.set(
        m?.x ?? 0,
        m ? m.y + m.height / 2 : 0,
        m?.z ?? 0,
        m ? m.radius + CUT_MOB_PAD : CUT_OFF,
      );
      this.cut.over[i] = m ? m.y + CUT_MOB_OVER : 0;
    });
  }

  /** A new decal in color, as written, at alpha and blended by mode: a boss's area or the lock ring, laid on the place and faint where it opens round the player and the mobs, so none hangs whole in front of a body in a hole; drawn only where the place is nearest. Its owner disposes it. */
  decal(color: Color3, alpha: number, mode: number): NodeMaterial {
    const m = decalMaterial(
      `decal ${color.toHexString()} ${alpha}`,
      this.scene,
      color,
      alpha,
      mode === Constants.ALPHA_PREMULTIPLIED,
      this.cut,
    );
    m.alphaMode = mode;
    m.forceAlphaBlending = true; // faint in a hole, at an alpha of 1 too
    m.backFaceCulling = false;
    m.disableDepthWrite = true; // under whatever stands on it
    m.zOffset = -2;
    onPlace(m);
    return m;
  }

  /** The material of a round shadow, made once: black, DISC_DARK at its middle, fading from DISC_SOFT of its radius. Drawn over the ground, it clears the stencil as the place does, so a hidden telegraph's ghost shows over it as over the ground. */
  disc(): NodeMaterial {
    let m = this.made.get('disc');
    if (!m) {
      m = discMaterial(
        'round shadow',
        this.scene,
        Color3.Black(),
        DISC_DARK,
        DISC_SOFT,
        Constants.ALPHA_COMBINE,
      );
      clearsMark(m);
      this.made.set('disc', m);
    }
    return m;
  }

  /** The material of a flame's glow card, made once: the torches' orange, added to what's behind, fading from its middle. */
  flameGlow(): NodeMaterial {
    let m = this.made.get('flame glow');
    if (!m) {
      m = discMaterial('flame glow', this.scene, GLOW, GLOW_PEAK, 0, Constants.ALPHA_ADD);
      this.made.set('flame glow', m);
    }
    return m;
  }

  /** base drawn unlit at full colour, made once a base: a torch's flame. */
  unlit(base: ToonBase): NodeMaterial {
    const key = `unlit ${base.texture?.uniqueId ?? '-'} ${base.color.toHexString()}`;
    let m = this.made.get(key);
    if (!m) {
      m = unlitMaterial(key, this.scene, base);
      this.made.set(key, m);
    }
    return m;
  }

  /** An unlit glow in color, linear, at alpha. */
  glow(color: Color3, alpha: number): NodeMaterial {
    const key = `glow ${color.toHexString()} ${alpha}`;
    let m = this.made.get(key);
    if (!m) {
      m = glowMaterial(key, this.scene, color, alpha);
      this.made.set(key, m);
    }
    return m;
  }

  /** An ink line round mesh: a clone on it, sharing its geometry and skeleton, drawn in the ink; cut for the place, as its mesh. */
  outline(mesh: AbstractMesh, cut = false): void {
    const hull = mesh.clone(`${mesh.name} ink`, mesh, true);
    if (!hull) {
      return;
    }
    hull.position.setAll(0);
    hull.rotationQuaternion = null;
    hull.rotation.setAll(0);
    hull.scaling.setAll(1);
    hull.isPickable = false;
    let ink = this.inks.get(cut);
    if (!ink) {
      ink = inkMaterial(this.scene, cut ? this.cut : null);
      this.inks.set(cut, ink);
    }
    hull.material = ink;
  }
}

/** An sRGB 0xrrggbb as a linear colour. */
export function linearOf(hex: number): Color3 {
  return Color3.FromInts((hex >> 16) & 0xff, (hex >> 8) & 0xff, hex & 0xff).toLinearSpace();
}

/** Builds each material's shaders for each kind of mesh it draws, and a depth pre-pass's own, so a first fade or afterimage doesn't stall a frame; as forceCompilation does, but stopping once the mesh is gone. */
export function warm(fades: readonly (readonly [Material, AbstractMesh])[]): void {
  const done = new Set<string>();
  for (const [m, mesh] of fades) {
    mesh.computeWorldMatrix(true); // its scale's state settles here, and picks a shader
    const key = `${m.uniqueId} ${shaderKind(mesh)}`;
    if (done.has(key)) {
      continue;
    }
    done.add(key);
    const engine = mesh.getEngine();
    const passes = (m.needDepthPrePass ? [true, false] : [true]).map((color) => ({
      color, // the pre-pass draws without colour, which its shader's defines follow
      trial: new SubMesh(0, 0, 0, 0, 0, mesh, undefined, false, false), // its own, so the mesh's drawn effect stays
    }));
    const check = () => {
      if (mesh.isDisposed()) {
        return;
      }
      const ready = passes.map(({ color, trial }) => {
        engine.setColorWrite(color);
        const built = m.isReadyForSubMesh(mesh, trial);
        engine.setColorWrite(true);
        return built;
      });
      if (!ready.every(Boolean)) {
        setTimeout(check, WARM_MS);
      }
    };
    check();
  }
}

const WARM_MS = 16;

/** What of a mesh changes its material's shader: its bones, its shadows, its vertex data and its scale's state; a model's weapon and body can share a material and not a shader. */
function shaderKind(mesh: AbstractMesh): string {
  const kinds = mesh instanceof Mesh ? mesh.getVerticesDataKinds().toSorted().join() : '';
  return `${mesh.useBones ? mesh.numBoneInfluencers : 0} ${mesh.receiveShadows} ${kinds} ${mesh.nonUniformScaling}`;
}
