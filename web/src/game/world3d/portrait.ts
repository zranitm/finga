// A character drawn in 3D into a window's own canvas (outfits.md section 7): a small scene of its own on the game's engine, drawn into a corner of the game's canvas before the world each frame and copied out, only while the window is open.
import { TargetCamera } from '@babylonjs/core/Cameras/targetCamera';
import type { AbstractEngine } from '@babylonjs/core/Engines/abstractEngine';
import { DirectionalLight } from '@babylonjs/core/Lights/directionalLight';
import { Color4 } from '@babylonjs/core/Maths/math.color';
import { Vector3 } from '@babylonjs/core/Maths/math.vector';
import { Viewport } from '@babylonjs/core/Maths/math.viewport';
import { Scene } from '@babylonjs/core/scene';

import type { Picks } from '@/state/wardrobe';

import type { Portrait } from '../view';

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

/** The portraits open on the game's engine, each drawn by draw while open. */
export class Portraits {
  private readonly open = new Set<PortraitView>();

  constructor(
    private readonly engine: AbstractEngine,
    private readonly models: ModelSource,
  ) {}

  /** A portrait drawn into canvas from the next frame till it's closed. */
  portrait(canvas: HTMLCanvasElement): Portrait {
    const view = new PortraitView(this.engine, this.models, canvas, () => this.open.delete(view));
    this.open.add(view);
    return view;
  }

  /** Draws each open portrait at now, dtMs after the last frame, before the world draws over the game's canvas. */
  draw(now: number, dtMs: number): void {
    for (const view of this.open) {
      view.draw(now, dtMs);
    }
  }

  dispose(): void {
    for (const view of [...this.open]) {
      view.close();
    }
  }
}

class PortraitView implements Portrait {
  private readonly scene: Scene;
  private readonly camera: TargetCamera;
  private readonly figures: Figures;
  private figure: Figure | null = null;
  private asked = 0; // each show's number: a figure made for an older one is let go
  private yaw = 0;
  private closed = false;

  constructor(
    private readonly engine: AbstractEngine,
    models: ModelSource,
    private readonly canvas: HTMLCanvasElement,
    private readonly gone: () => void,
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

  close(): void {
    if (this.closed) {
      return;
    }
    this.closed = true;
    this.figure?.dispose();
    this.figure = null;
    this.scene.dispose();
    this.gone();
  }

  /** Poses the figure, draws the scene into the game canvas's bottom-left corner at the canvas's own size, and copies it there. */
  draw(now: number, dtMs: number): void {
    const figure = this.figure;
    if (!figure) {
      return;
    }
    figure.update(now, dtMs, STILL, true);
    const full = [this.engine.getRenderWidth(), this.engine.getRenderHeight()] as const;
    const scale = this.engine.getHardwareScalingLevel();
    const w = Math.min(full[0], Math.round(this.canvas.clientWidth / scale));
    const h = Math.min(full[1], Math.round(this.canvas.clientHeight / scale));
    if (w <= 0 || h <= 0) {
      return; // not laid out yet
    }
    this.camera.viewport = new Viewport(0, 0, w / full[0], h / full[1]);
    this.scene.render();
    const source = this.engine.getRenderingCanvas();
    const to = this.canvas.getContext('2d');
    if (!source || !to) {
      return;
    }
    this.engine.flushFramebuffer();
    if (this.canvas.width !== w || this.canvas.height !== h) {
      this.canvas.width = w;
      this.canvas.height = h;
    }
    to.clearRect(0, 0, w, h);
    to.drawImage(source, 0, full[1] - h, w, h, 0, 0, w, h);
  }
}
