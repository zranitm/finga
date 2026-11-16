// Makes the bodies a map's scene draws: a model through the scene's loader, which every body of it shares.
import type { Scene } from '@babylonjs/core/scene';

import { Body } from './body';
import type { Figure, Lighting } from './body';
import type { ModelLook } from './looks';
import type { Models } from './models';

export class Figures {
  constructor(
    private readonly scene: Scene,
    private readonly lighting: Lighting,
    private readonly models: Models,
  ) {}

  /** A body for look, once its model's file is in. */
  async make(look: ModelLook): Promise<Figure> {
    return new Body(this.scene, await this.models.load(look.file), look, this.lighting);
  }
}
