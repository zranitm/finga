// The stage in the page: a canvas in #game, the player's keys, the render loop, and a readout of the frame time.
import { Engine } from '@babylonjs/core/Engines/engine';

import { isTyping } from '@/input/typing';
import { settings } from '@/state/settings';

import type { WorldEntry } from '../scenes/world';
import { bindKeyboard } from '../systems/keyboard';

import { Stage } from './stage';

const READOUT_MS = 500;

/** Opens the stage for entry in parent; the returned close takes it all down. */
export async function run(parent: HTMLElement, entry: WorldEntry): Promise<() => void> {
  const canvas = document.createElement('canvas');
  canvas.style.cssText = 'display:block;width:100%;height:100%;outline:none';
  parent.appendChild(canvas);
  const engine = new Engine(canvas, true, { stencil: true }, true);
  const stage = await Stage.open(
    engine,
    { cls: entry.cls, tickMs: 1000 / entry.tickRateHz },
    (file) => `${import.meta.env.BASE_URL}models/${file}`,
    () => performance.now(),
  );

  const offKeys = bindKeyboard({
    target: stage,
    preset: () => settings.getState().preset,
    typing: isTyping,
    onAction: (action) => stage.onAction(action),
  });
  const shift = (e: KeyboardEvent) => stage.run(e.shiftKey);
  window.addEventListener('keydown', shift);
  window.addEventListener('keyup', shift);
  const resize = new ResizeObserver(() => engine.resize());
  resize.observe(parent);

  const readout = document.createElement('div');
  readout.style.cssText =
    'position:absolute;top:8px;right:8px;padding:4px 8px;font:12px monospace;color:#fff;background:rgba(0,0,0,.55);border-radius:4px;pointer-events:none';
  parent.appendChild(readout);
  let frames = 0;
  let busyMs = 0;
  let since = performance.now();
  engine.runRenderLoop(() => {
    const t0 = performance.now();
    stage.step(engine.getDeltaTime());
    stage.scene.render();
    const t1 = performance.now();
    frames++;
    busyMs += t1 - t0;
    if (t1 - since >= READOUT_MS) {
      readout.textContent = `${(busyMs / frames).toFixed(1)} ms/frame · ${Math.round((frames * 1000) / (t1 - since))} fps`;
      frames = 0;
      busyMs = 0;
      since = t1;
    }
  });

  return () => {
    engine.stopRenderLoop();
    offKeys();
    window.removeEventListener('keydown', shift);
    window.removeEventListener('keyup', shift);
    resize.disconnect();
    stage.dispose();
    engine.dispose();
    readout.remove();
    canvas.remove();
  };
}
