// The shaders an engine has built, which Babylon keeps by source and defines in a private map: the warm-ups' tests read it to see a first use build none.
import type { AbstractEngine } from '@babylonjs/core/Engines/abstractEngine';

/** The keys of every shader engine holds built. */
export function builtShaders(engine: AbstractEngine): string[] {
  return Object.keys(
    (engine as unknown as { _compiledEffects: Record<string, unknown> })._compiledEffects,
  );
}
