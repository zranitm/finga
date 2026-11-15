// The world in the page: a canvas in #game, the render loop, and with ?fps in the address a readout of the frame time.
import { Engine } from '@babylonjs/core/Engines/engine';

import type { GameSocket } from '@/net/socket';

import type { Arrival } from '../systems/arrival';
import type { WorldEntry } from '../view';

import { World } from './world';

const READOUT_MS = 500;

/** Opens the world for entry in parent, from the map arriving holds; the returned close takes it all down. */
export async function run(
  parent: HTMLElement,
  entry: WorldEntry,
  arriving: Arrival,
  socket: GameSocket,
): Promise<() => void> {
  const canvas = document.createElement('canvas');
  canvas.style.cssText = 'display:block;width:100%;height:100%;outline:none';
  parent.appendChild(canvas);
  const engine = new Engine(canvas, true, { stencil: true }, true);
  let world: World;
  try {
    world = await World.open(
      {
        engine,
        socket,
        parent,
        player: { characterId: entry.characterId, cls: entry.cls, name: entry.name },
        models: (file) => `${import.meta.env.BASE_URL}models/${file}`,
        now: () => performance.now(),
      },
      arriving,
    );
  } catch (err) {
    arriving.take();
    engine.dispose();
    canvas.remove();
    throw err;
  }
  const resize = new ResizeObserver(() => engine.resize());
  resize.observe(parent);
  const readout = new URLSearchParams(location.search).has('fps') ? frameReadout(parent) : null;
  engine.runRenderLoop(() => {
    const t0 = performance.now();
    world.frame(engine.getDeltaTime());
    readout?.(performance.now() - t0);
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

/** A readout of the ms each frame took and the frame rate, bottom left out of the HUD's way; returns what each frame reports to. */
function frameReadout(parent: HTMLElement): (busyMs: number) => void {
  const el = document.createElement('div');
  el.dataset.readout = '';
  el.style.cssText =
    'position:absolute;left:8px;bottom:8px;padding:4px 8px;font:12px monospace;color:#fff;background:rgba(0,0,0,.55);border-radius:4px;pointer-events:none';
  parent.appendChild(el);
  let frames = 0;
  let busyMs = 0;
  let since = performance.now();
  return (ms) => {
    frames++;
    busyMs += ms;
    const now = performance.now();
    if (now - since >= READOUT_MS) {
      el.textContent = `${(busyMs / frames).toFixed(1)} ms/frame · ${Math.round((frames * 1000) / (now - since))} fps`;
      frames = 0;
      busyMs = 0;
      since = now;
    }
  };
}
