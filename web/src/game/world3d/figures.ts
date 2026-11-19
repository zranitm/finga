// Makes the bodies a map's scene draws: a model through the scene's loader, which every body of it shares, and the parts files its outfit pieces come from.
import type { Texture } from '@babylonjs/core/Materials/Textures/texture';
import type { Scene } from '@babylonjs/core/scene';

import { Body } from './body';
import type { Figure, Lighting } from './body';
import type { ModelLook } from './looks';
import type { Models } from './models';
import { colourwayFile, partsFile } from './outfit-looks';
import type { PieceLook } from './outfit-looks';
import type { Worn } from './wear';

export class Figures {
  constructor(
    private readonly scene: Scene,
    private readonly lighting: Lighting,
    private readonly models: Models,
  ) {}

  /** A body for look, once its model's file and its pieces' parts files are in. */
  async make(look: ModelLook): Promise<Figure> {
    const [container, ...worn] = await Promise.all([
      this.models.load(look.file),
      ...(look.wear ?? []).map((piece) => this.wearable(piece)),
    ]);
    return new Body(this.scene, container, look, this.lighting, worn);
  }

  private async wearable(piece: PieceLook): Promise<Worn> {
    const parts = await this.models.load(partsFile(piece.file));
    const atlas = piece.colourway
      ? this.models.atlas(colourwayFile(piece.colourway), parts.textures[0] as Texture)
      : null;
    return { piece, parts, atlas };
  }
}
