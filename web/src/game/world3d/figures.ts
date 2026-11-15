// Makes the bodies a map's scene draws: a model through the scene's loader, which every body of it shares; a mob with no model gets its stand-in.
import type { Scene } from '@babylonjs/core/scene';

import { Body } from './body';
import type { Figure, Lighting } from './body';
import type { Look, ModelLook } from './looks';
import type { Models } from './models';
import { StandIn } from './standin';

export class Figures {
  constructor(
    private readonly scene: Scene,
    private readonly lighting: Lighting,
    private readonly models: Models,
  ) {}

  /** A body for look: its model once the file is in, or its stand-in at once. */
  async make(look: Look | ModelLook): Promise<Figure> {
    if ('standIn' in look) {
      return new StandIn(this.scene, look.standIn, this.lighting);
    }
    const model = 'model' in look ? look.model : look;
    return new Body(this.scene, await this.models.load(model.file), model, this.lighting);
  }
}
