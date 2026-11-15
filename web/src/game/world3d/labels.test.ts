import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { Labels, stackTags } from './labels';
import type { TagBox } from './labels';
import type { Overlay, ScreenPoint } from './overlay';

// A name's bottom 300 px down the screen, 17 px tall.
const tag = (x: number, w = 60, bottom = 300): TagBox => ({ x, bottom, w, h: 17 });
const marker = (x: number): TagBox => ({ x, bottom: 300 - 19, w: 12, h: 26 });
const trainer = tag(500, 100);
// A lifted tag sits 2 px clear of the text under it.
const above = (b: TagBox) => b.bottom - b.h - 2;

describe('stackTags', () => {
  it('tags that cover nothing stay where they are', () => {
    expect(stackTags([trainer], [tag(400), tag(620)])).toEqual([300, 300]);
  });

  it('a player on a trainer has its tag lifted just above the trainer’s', () => {
    expect(stackTags([trainer], [tag(490)])).toEqual([above(trainer)]);
  });

  it('a tag lifted into the trainer’s marker goes on above the marker', () => {
    expect(stackTags([trainer, marker(500)], [tag(490)])).toEqual([above(marker(500))]);
  });

  // A box the tag only reaches once lifted, so a single pass in this order would miss it.
  it('a tag climbs past every box its lift runs into, whatever their order', () => {
    const high: TagBox = { x: 500, bottom: 281, w: 12, h: 26 };
    expect(stackTags([high, trainer], [tag(490)])).toEqual([above(high)]);
  });

  it('players on one spot stack, the earlier tag lower', () => {
    const [first, second, third] = stackTags([trainer], [tag(490), tag(510), tag(480)]);
    expect(first).toBe(above(trainer));
    expect(second).toBe(above({ ...tag(510), bottom: first ?? 0 }));
    expect(third).toBe(above({ ...tag(480), bottom: second ?? 0 }));
  });

  it('tags only touching at their edges both stay', () => {
    expect(stackTags([], [tag(400), tag(460)])).toEqual([300, 300]);
  });

  it('a tag a little lower than another still lifts above it, never down onto its owner', () => {
    const higher = tag(400, 60, 292);
    expect(stackTags([higher], [tag(410)])).toEqual([above(higher)]);
  });

  it('a tag already clear above a box stays', () => {
    expect(stackTags([trainer], [tag(500, 60, 250)])).toEqual([250]);
  });
});

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
