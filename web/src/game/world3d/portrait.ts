// Characters drawn in 3D into windows' own canvases (outfits.md section 7): one small scene on the game's engine, kept with its models while the world is open, each open portrait's figure drawn into a corner of the game's canvas before the world each frame and copied out.
import { TargetCamera } from '@babylonjs/core/Cameras/targetCamera';
import type { AbstractEngine } from '@babylonjs/core/Engines/abstractEngine';
import { DirectionalLight } from '@babylonjs/core/Lights/directionalLight';
import { Color4 } from '@babylonjs/core/Maths/math.color';
import { Vector3 } from '@babylonjs/core/Maths/math.vector';
import { Scene } from '@babylonjs/core/scene';

import type { Picks, Portrait } from '@/state/wardrobe';

import { STILL } from './body';
import type { Figure } from './body';
import { Figures } from './figures';
import { playerLook } from './looks';
import { Models } from './models';
import type { ModelSource } from './models';
import { Palette } from './toon';

const EYE = new Vector3(0, 1.15, 3.6); // in front of the figure, a little above its chest
const AIM = new Vector3(0, 0.9, 0);
const SUN = new Vector3(-0.4, -0.8, -0.6); // from over the viewer's shoulder

/** The portraits on the game's engine. Made before the world, so the world's scene stays the engine's last; a window opened again finds its models loaded. */
export class Portraits {
  private readonly scene: Scene;
  private readonly camera: TargetCamera;
  private readonly figures: Figures;
  private readonly open = new Set<PortraitView>();

  constructor(
    private readonly engine: AbstractEngine,
    models: ModelSource,
  ) {
    this.scene = new Scene(engine);
    this.scene.clearColor = new Color4(0, 0, 0, 0); // the window's own background shows round the figure
    this.camera = new TargetCamera('portrait', EYE.clone(), this.scene);
    this.camera.setTarget(AIM);
    new DirectionalLight('sun', SUN.normalizeToNew(), this.scene);
    this.figures = new Figures(
      this.scene,
      { palette: new Palette(this.scene), shadows: null },
      new Models(this.scene, models),
    );
  }

  /** A portrait drawn into canvas from the next frame till it's closed. */
  portrait(canvas: HTMLCanvasElement): Portrait {
    const view = new PortraitView(this.figures, canvas, () => this.open.delete(view));
    this.open.add(view);
    return view;
  }

  /** Draws each open portrait with a figure at now, dtMs after the last frame, before the world draws over the game's canvas; the others' figures are hidden while it's drawn. */
  draw(now: number, dtMs: number): void {
    const source = this.engine.getRenderingCanvas();
    const full = [this.engine.getRenderWidth(), this.engine.getRenderHeight()] as const;
    const scale = this.engine.getHardwareScalingLevel();
    for (const view of this.open) {
      const figure = view.figure;
      const to = view.canvas.getContext('2d');
      const w = Math.min(full[0], Math.round(view.canvas.clientWidth / scale));
      const h = Math.min(full[1], Math.round(view.canvas.clientHeight / scale));
      if (!figure || !source || !to || w <= 0 || h <= 0) {
        continue; // not in yet, or not laid out
      }
      for (const other of this.open) {
        other.figure?.root.setEnabled(other === view);
      }
      figure.update(now, dtMs, STILL, true);
      const viewport = this.camera.viewport;
      [viewport.x, viewport.y, viewport.width, viewport.height] = [0, 0, w / full[0], h / full[1]];
      this.scene.render();
      this.engine.flushFramebuffer();
      if (view.canvas.width !== w || view.canvas.height !== h) {
        view.canvas.width = w;
        view.canvas.height = h;
      }
      to.clearRect(0, 0, w, h);
      to.drawImage(source, 0, full[1] - h, w, h, 0, 0, w, h);
    }
  }

  /** As the world closes: the scene, its models and any figure still open go. */
  dispose(): void {
    this.open.clear();
    this.scene.dispose();
  }
}

class PortraitView implements Portrait {
  figure: Figure | null = null;
  private asked = 0; // each show's number: a figure made for an older one is let go
  private yaw = 0;
  private closed = false;

  constructor(
    private readonly figures: Figures,
    readonly canvas: HTMLCanvasElement,
    private readonly gone: () => void,
  ) {}

  show(cls: string, look: Picks): void {
    const asked = ++this.asked;
    this.figures.make(playerLook(cls, look)).then(
      (figure) => {
        if (this.closed || asked !== this.asked) {
          figure.dispose();
          return;
        }
        this.figure?.dispose(); // the last stays till the next is in
        this.figure = figure;
        figure.root.rotation.y = this.yaw;
      },
      (err: unknown) => {
        console.info(`finga: a portrait's figure didn't load: ${String(err)}`);
      },
    );
  }

  turn(radians: number): void {
    this.yaw += radians;
    if (this.figure) {
      this.figure.root.rotation.y = this.yaw;
    }
  }

  /** Its figure goes, and one still loading goes as it comes in; the models stay for the next. */
  close(): void {
    this.closed = true;
    this.figure?.dispose();
    this.figure = null;
    this.gone();
  }
}
