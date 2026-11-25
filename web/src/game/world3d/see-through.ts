// What the place hides of a telegraph, drawn again faintly through it (3d-world.md section 10): whatever is drawn nearest marks the stencil but the place, which clears it, and a ghost draws only behind what's nearest where it's clear.
import '@babylonjs/core/Engines/AbstractEngine/abstractEngine.stencil';
import { Constants } from '@babylonjs/core/Engines/constants';
import type { Material } from '@babylonjs/core/Materials/material';
import { Mesh } from '@babylonjs/core/Meshes/mesh';
import type { Scene } from '@babylonjs/core/scene';

/** How strong a ghost is beside what it's a ghost of. */
export const GHOST = 0.5;
/** Where a cave's round shadows, a red area and then the lock ring draw among their group's blended meshes: before every effect, at its default index or a small one, so none marks the stencil or the depth over them first; an area lies over a shadow. */
export const SHADOW_ORDER = -3;
export const AREA_ORDER = -2;
export const RING_ORDER = -1;
const MARK = 1; // what a mesh with no stencil of its own writes, as the lock's outline leaves it for the meshes it doesn't outline
const CLEAR = 0;

/** Has every mesh with no stencil of its own mark it where it's drawn nearest, every frame; the lock's outline turns it on only while it shows. */
export function markOthers(scene: Scene): void {
  const engine = scene.getEngine();
  scene.onBeforeRenderObservable.add(() => {
    engine.setStencilBuffer(true);
    engine.setStencilFunction(Constants.ALWAYS);
    engine.setStencilFunctionReference(MARK);
    engine.setStencilOperationPass(Constants.REPLACE);
  });
}

/** Makes m the place's: it clears the mark where it's drawn nearest. */
export function clearsMark(m: Material): void {
  m.stencil.enabled = true;
  m.stencil.func = Constants.ALWAYS;
  m.stencil.funcRef = CLEAR;
  m.stencil.opStencilDepthPass = Constants.REPLACE;
}

/** Makes m a decal's: drawn only where the place is nearest, never over a body, and leaving the stencil as the ground under it left it. */
export function onPlace(m: Material): void {
  m.stencil.enabled = true;
  m.stencil.func = Constants.EQUAL;
  m.stencil.funcRef = CLEAR;
  m.stencil.opStencilDepthPass = Constants.KEEP;
}

/** Makes m a ghost's: drawn only where something is nearer, and that is the place. */
export function behindPlace(m: Material): void {
  m.depthFunction = Constants.GREATER;
  m.disableDepthWrite = true;
  m.stencil.enabled = true;
  m.stencil.func = Constants.EQUAL;
  m.stencil.funcRef = CLEAR;
  m.stencil.opStencilDepthPass = Constants.KEEP;
}

/** A ghost of mesh in material: a child sharing its geometry and its place in the draw, so it moves, turns, reshapes and goes with it; mesh's visibility isn't its own. */
export function ghostOf(mesh: Mesh, material: Material): Mesh {
  const g = new Mesh(`${mesh.name} ghost`, mesh.getScene());
  mesh.geometry?.applyToMesh(g);
  g.parent = mesh;
  g.material = material;
  g.renderingGroupId = mesh.renderingGroupId;
  g.alphaIndex = mesh.alphaIndex;
  g.isPickable = false;
  g.alwaysSelectAsActiveMesh = true; // its bounds aren't refreshed as mesh reshapes
  return g;
}
