// The world in the page: a canvas in #game, the render loop, and with ?fps in the address a readout of the frame time and the prediction's resets.
import { Engine } from '@babylonjs/core/Engines/engine';

import type { GameSocket } from '@/net/socket';

import type { Arrival } from '../systems/arrival';
import type { WorldEntry } from '../view';

import { workerChunks } from './chunk-workers';
import { World } from './world';

const READOUT_MS = 500;
const CHUNK_WORKERS = 2; // a region's chunks build two at a time, beside the page's thread

/** Opens the world for entry in parent, from the map arriving holds; the returned close takes it all down, built or not. */
export function run(
  parent: HTMLElement,
  entry: WorldEntry,
  arriving: Arrival,
  socket: GameSocket,
): () => void {
  const canvas = document.createElement('canvas');
  canvas.style.cssText = 'display:block;width:100%;height:100%;outline:none';
  parent.appendChild(canvas);
  const engine = new Engine(canvas, true, { stencil: true }, true);
  const world = World.open(
    {
      engine,
      socket,
      parent,
      player: { characterId: entry.characterId, cls: entry.cls, name: entry.name },
      models: (file) => `${import.meta.env.BASE_URL}models/${file}`,
      chunks: workerChunks(CHUNK_WORKERS),
      now: () => performance.now(),
      reload: () => location.reload(),
    },
    arriving,
  );
  const resize = new ResizeObserver(() => engine.resize());
  resize.observe(parent);
  const readout = new URLSearchParams(location.search).has('fps') ? frameReadout(parent) : null;
  engine.runRenderLoop(() => {
    const t0 = performance.now();
    world.frame(engine.getDeltaTime());
    readout?.(performance.now() - t0, world.resets);
  });

  return () => {
    engine.stopRenderLoop();
    resize.disconnect();
    world.dispose();
    engine.dispose();
    canvas.remove();
    parent.querySelector('[data-readout]')?.remove();
  };
}

/** A readout of the ms each frame took, the frame rate and the prediction's resets, bottom right clear of the HUD and chat; returns what each frame reports to. */
function frameReadout(parent: HTMLElement): (busyMs: number, resets: number) => void {
  const el = document.createElement('div');
  el.dataset.readout = '';
  el.style.cssText =
    'position:absolute;right:8px;bottom:8px;padding:4px 8px;font:12px monospace;color:#fff;background:rgba(0,0,0,.55);border-radius:4px;pointer-events:none';
  parent.appendChild(el);
  let frames = 0;
  let busyMs = 0;
  let since = performance.now();
  return (ms, resets) => {
    frames++;
    busyMs += ms;
    const now = performance.now();
    if (now - since >= READOUT_MS) {
      el.textContent = `${(busyMs / frames).toFixed(1)} ms/frame · ${Math.round((frames * 1000) / (now - since))} fps · ${resets} resets`;
      frames = 0;
      busyMs = 0;
      since = now;
    }
  };
}
