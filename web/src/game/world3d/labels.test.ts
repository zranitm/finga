import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { Labels } from './labels';
import type { Overlay, ScreenPoint } from './overlay';

describe('Labels', () => {
  // Every world point lands at (x, 300 - y) on the screen; jsdom lays nothing out, so a text is 7 px a letter and 17 tall.
  const overlay = {
    layer: document.createElement('div'),
    project: (x: number, y: number, _z: number, out: ScreenPoint) => {
      out.x = x;
      out.y = 300 - y;
      return true;
    },
  } as unknown as Overlay;
  const bottoms = (): Record<string, number> =>
    Object.fromEntries(
      [...overlay.layer.querySelectorAll<HTMLElement>('[data-label]')].map((e) => [
        e.dataset.label ?? '',
        Number(/translate\(([-\d.]+)px, ?([-\d.]+)px\)/.exec(e.style.transform)?.[2]),
      ]),
    );

  beforeEach(() => {
    vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockImplementation(function (
      this: HTMLElement,
    ) {
      return (this.textContent?.length ?? 0) * 7;
    });
    vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockReturnValue(17);
  });

  afterEach(() => {
    overlay.layer.replaceChildren();
    vi.restoreAllMocks();
  });

  it('lifts the player’s name clear of an NPC’s, and others’ clear of theirs, the player’s first whenever it came', () => {
    const labels = new Labels(overlay);
    labels.set('p:7', 'player', 'Robin', 102, 0, 0);
    labels.set('npc:guide', 'npc', 'Town Guide', 100, 0, 0);
    labels.set('portal:to_forest', 'portal', 'To Forest', 400, 0, 0);
    labels.set('me', 'self', 'Ayla', 98, 0, 0);
    labels.place();
    expect(bottoms()).toEqual({
      'p:7': 300 - 2 * 19,
      'npc:guide': 300,
      'portal:to_forest': 300,
      me: 300 - 19,
    });
  });

  it('measures a name again when its text changes, and a hidden one lifts nothing', () => {
    const labels = new Labels(overlay);
    labels.set('npc:guide', 'npc', 'G', 100, 0, 0);
    labels.set('me', 'self', 'Ayla', 120, 0, 0);
    labels.place();
    expect(bottoms().me).toBe(300); // a letter's width doesn't reach 20 px across
    labels.set('npc:guide', 'npc', 'Town Guide', 100, 0, 0);
    labels.place();
    expect(bottoms().me).toBe(300 - 19);
    labels.set('npc:guide', 'npc', '', 100, 0, 0);
    labels.place();
    expect(bottoms().me).toBe(300);
  });
});
