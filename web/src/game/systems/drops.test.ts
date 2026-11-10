import { create } from '@bufbuild/protobuf';
import { describe, expect, it } from 'vitest';

import { DropViewSchema } from '@/protocol/messages';

import { DropTracker, departure } from './drops';

const gel = (id: number, x: number, ownerId = 'p-1') =>
  create(DropViewSchema, { id, template: 'slime_gel', quantity: 1, x, y: 608, ownerId });

describe('DropTracker', () => {
  it('a Snapshot sets the drops and reports what appeared and what went; clear forgets them', () => {
    const t = new DropTracker();
    expect(t.applySnapshot([gel(1, 100), gel(2, 200)])).toEqual({
      added: [gel(1, 100), gel(2, 200)],
      removed: [],
      collected: [],
    });
    expect(t.applySnapshot([gel(2, 200), gel(3, 300)])).toEqual({
      added: [gel(3, 300)],
      removed: [gel(1, 100)],
      collected: [],
    });
    t.clear();
    expect(t.applySnapshot([gel(3, 300)])).toEqual({
      added: [gel(3, 300)],
      removed: [],
      collected: [],
    });
  });

  it('a Delta adds, removes and collects, each drop it knows once', () => {
    const t = new DropTracker();
    t.applySnapshot([gel(1, 100), gel(2, 200, 'p-2')]);
    expect(t.applyDelta([gel(3, 300), gel(1, 100)], [1, 99], [2, 98])).toEqual({
      added: [gel(3, 300)],
      removed: [gel(1, 100)],
      collected: [gel(2, 200, 'p-2')],
    });
    expect(t.applyDelta([], [1], [2])).toEqual({ added: [], removed: [], collected: [] });
    expect(t.applySnapshot([])).toEqual({ added: [], removed: [gel(3, 300)], collected: [] });
  });

  it('a drop that appears and goes in one Delta is reported both ways', () => {
    const t = new DropTracker();
    expect(t.applyDelta([gel(4, 400)], [], [4])).toEqual({
      added: [gel(4, 400)],
      removed: [],
      collected: [gel(4, 400)],
    });
  });
});

describe('departure', () => {
  const owner = { x: 416, y: 580 };
  it.each([
    {
      name: 'a collected drop whose owner is on the map flies to them',
      collected: true,
      at: owner,
      want: { kind: 'fly', to: owner },
    },
    {
      name: 'a collected drop whose owner is elsewhere fades',
      collected: true,
      at: undefined,
      want: { kind: 'fade' },
    },
    {
      name: 'a drop gone without an owner taking it fades where it lies',
      collected: false,
      at: owner,
      want: { kind: 'fade' },
    },
  ])('$name', ({ collected, at, want }) => {
    expect(departure(collected, at)).toEqual(want);
  });
});
