// The scene's materials (3d-world.md section 11): one toon NodeMaterial per base colour or texture, its light posterized into flat bands, an inverted hull for the ink line, and an unlit glow; all take the scene's fog.
import { AddBlock } from '@babylonjs/core/Materials/Node/Blocks/addBlock';
import { ClampBlock } from '@babylonjs/core/Materials/Node/Blocks/clampBlock';
import { FogBlock } from '@babylonjs/core/Materials/Node/Blocks/Dual/fogBlock';
import { LightBlock } from '@babylonjs/core/Materials/Node/Blocks/Dual/lightBlock';
import { TextureBlock } from '@babylonjs/core/Materials/Node/Blocks/Dual/textureBlock';
import { FragmentOutputBlock } from '@babylonjs/core/Materials/Node/Blocks/Fragment/fragmentOutputBlock';
import { InputBlock } from '@babylonjs/core/Materials/Node/Blocks/Input/inputBlock';
import { LerpBlock } from '@babylonjs/core/Materials/Node/Blocks/lerpBlock';
import { MultiplyBlock } from '@babylonjs/core/Materials/Node/Blocks/multiplyBlock';
import { NormalizeBlock } from '@babylonjs/core/Materials/Node/Blocks/normalizeBlock';
import { PosterizeBlock } from '@babylonjs/core/Materials/Node/Blocks/posterizeBlock';
import { ScaleBlock } from '@babylonjs/core/Materials/Node/Blocks/scaleBlock';
import { TransformBlock } from '@babylonjs/core/Materials/Node/Blocks/transformBlock';
import { BonesBlock } from '@babylonjs/core/Materials/Node/Blocks/Vertex/bonesBlock';
import { VertexOutputBlock } from '@babylonjs/core/Materials/Node/Blocks/Vertex/vertexOutputBlock';
import { NodeMaterialBlockConnectionPointTypes } from '@babylonjs/core/Materials/Node/Enums/nodeMaterialBlockConnectionPointTypes';
import { NodeMaterialSystemValues } from '@babylonjs/core/Materials/Node/Enums/nodeMaterialSystemValues';
import { NodeMaterial } from '@babylonjs/core/Materials/Node/nodeMaterial';
import type { NodeMaterialConnectionPoint } from '@babylonjs/core/Materials/Node/nodeMaterialBlockConnectionPoint';
import type { Texture } from '@babylonjs/core/Materials/Textures/texture';
import { Color3 } from '@babylonjs/core/Maths/math.color';
import type { AbstractMesh } from '@babylonjs/core/Meshes/abstractMesh';
import type { Scene } from '@babylonjs/core/scene';

/** The light bands a surface steps through: shade, mid and lit; in shade it keeps this share of its colour, a cool grey. */
const BANDS = 3;
const SHADE = new Color3(0.5, 0.5, 0.58).toLinearSpace();
/** The ink line's width in metres, pushed out after skinning, and its colour. */
const OUTLINE_M = 0.03;
const INK = new Color3(0.07, 0.06, 0.09);

export interface ToonBase {
  readonly texture: Texture | null;
  readonly color: Color3; // linear, as glTF writes it
}

/** The vertex half both materials share: a skinned position and normal in the world, out to clip space. */
class Skinned {
  readonly worldPos = new TransformBlock('worldPos');
  readonly worldNormal = new TransformBlock('worldNormal');
  readonly vertex = new VertexOutputBlock('vertexOutput');

  constructor() {
    const bones = new BonesBlock('bones');
    this.attribute('matricesIndices').output.connectTo(bones.matricesIndices);
    this.attribute('matricesWeights').output.connectTo(bones.matricesWeights);
    this.system('world', NodeMaterialSystemValues.World).output.connectTo(bones.world);
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

/** Finishes m: rgb, a linear colour, fogged by its distance from the camera, out in gamma space at alpha. */
function built(m: NodeMaterial, s: Skinned, rgb: NodeMaterialConnectionPoint, alpha = 1) {
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
  const fragment = new FragmentOutputBlock('fragmentOutput');
  fragment.convertToGammaSpace = true;
  fog.output.connectTo(fragment.rgb);
  if (alpha < 1) {
    const a = new InputBlock('alpha');
    a.value = alpha;
    a.output.connectTo(fragment.a);
  }
  m.addOutputNode(s.vertex);
  m.addOutputNode(fragment);
  m.build();
  return m;
}

function toonMaterial(name: string, scene: Scene, base: ToonBase, alpha: number): NodeMaterial {
  const s = new Skinned();
  s.place(s.worldPos.output);

  const light = new LightBlock('light'); // every light on the mesh: the sun alone, here
  const normal = new NormalizeBlock('normal'); // the world matrix carries the model's scale
  s.worldNormal.output.connectTo(normal.input);
  s.worldPos.output.connectTo(light.worldPosition);
  normal.output.connectTo(light.worldNormal);
  s.system('cameraPosition', NodeMaterialSystemValues.CameraPosition).output.connectTo(
    light.cameraPosition,
  );

  // Posterize floors the light into 0, ⅓ and ⅔; stretched to 0, ½ and 1, the top band is the colour itself.
  const steps = new InputBlock('bands');
  steps.value = BANDS;
  const floored = new PosterizeBlock('floored');
  light.diffuseOutput.connectTo(floored.value);
  steps.output.connectTo(floored.steps);
  const stretch = new InputBlock('stretch');
  stretch.value = BANDS / (BANDS - 1);
  const stretched = new ScaleBlock('stretched');
  floored.output.connectTo(stretched.input);
  stretch.output.connectTo(stretched.factor);
  const banded = new ClampBlock('banded');
  stretched.output.connectTo(banded.value);
  const shade = new InputBlock('shade');
  shade.value = SHADE;
  const lit = new InputBlock('lit');
  lit.value = Color3.White();
  const tone = new LerpBlock('tone');
  shade.output.connectTo(tone.left);
  lit.output.connectTo(tone.right);
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
  } else {
    lit.output.connectTo(albedo.left);
  }
  color.output.connectTo(albedo.right);

  const shaded = new MultiplyBlock('shaded');
  albedo.output.connectTo(shaded.left);
  tone.output.connectTo(shaded.right);
  return built(new NodeMaterial(name, scene, { emitComments: false }), s, shaded.output, alpha);
}

/** The ink: back faces only, pushed out along their normals by OUTLINE_M in the world, so every model's line is the same width. */
function inkMaterial(scene: Scene): NodeMaterial {
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
  const m = new NodeMaterial('ink', scene, { emitComments: false });
  m.cullBackFaces = false;
  return built(m, s, ink.output);
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

/** A scene's materials: toon ones by texture and colour, shared by every mesh drawn with them, and the ink. */
export class Palette {
  private readonly made = new Map<string, NodeMaterial>();
  private ink: NodeMaterial | null = null;

  constructor(private readonly scene: Scene) {}

  /** The toon material for base, drawn at alpha: 1, or less for a solid faded out of the camera's way. */
  toon(base: ToonBase, alpha = 1): NodeMaterial {
    const color = base.color.clampToRef(0, 1, new Color3());
    const key = `${base.texture?.uniqueId ?? '-'} ${color.toHexString()}${alpha < 1 ? ` ${alpha}` : ''}`;
    let m = this.made.get(key);
    if (!m) {
      m = toonMaterial(`toon ${key}`, this.scene, { texture: base.texture, color }, alpha);
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

  /** An ink line round mesh: a clone on it, sharing its geometry and skeleton, drawn in the ink. */
  outline(mesh: AbstractMesh): void {
    const hull = mesh.clone(`${mesh.name} ink`, mesh, true);
    if (!hull) {
      return;
    }
    hull.position.setAll(0);
    hull.rotationQuaternion = null;
    hull.rotation.setAll(0);
    hull.scaling.setAll(1);
    hull.isPickable = false;
    this.ink ??= inkMaterial(this.scene);
    hull.material = this.ink;
  }
}

/** An sRGB 0xrrggbb as a linear colour. */
export function linearOf(hex: number): Color3 {
  return Color3.FromInts((hex >> 16) & 0xff, (hex >> 8) & 0xff, hex & 0xff).toLinearSpace();
}
