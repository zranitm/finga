import { create } from '@bufbuild/protobuf';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ERR, errorText } from '@/protocol/errors';
import { PouchKind, PouchViewSchema, ResponseSchema } from '@/protocol/messages';

import { picksInReach, pouches } from './pouches';
import type { TakeRequest } from './pouches';

/** Pouch id holding one of each template, gone in goneMs. */
const pouch = (id: number, templates: string[], goneMs = 60000) =>
  create(PouchViewSchema, {
    id,
    kind: PouchKind.POUCH,
    x: id,
    items: templates.map((template, i) => ({ item: i + 1, template, quantity: 1 })),
    goneInMs: BigInt(goneMs),
  });

/** A take whose request answers each pick with codes, in order, or fails with err. */
function answering(codes: number[] | Error): { request: TakeRequest; asked: number[][][] } {
  const asked: number[][][] = [];
  const request: TakeRequest = (picks) => {
    asked.push(picks.map((p) => [p.pouch, p.item]));
    if (codes instanceof Error) {
      return Promise.reject(codes);
    }
    return Promise.resolve(
      create(ResponseSchema, {
        ok: true,
        picks: picks.map((pick, i) => ({ pick, code: codes[i] ?? 0 })),
      }),
    );
  };
  return { request, asked };
}

const items = () => pouches.getState().pouches.map((p) => [p.id, p.items.map((it) => it.template)]);

describe('pouches', () => {
  beforeEach(() => {
    pouches.reset();
    vi.spyOn(performance, 'now').mockReturnValue(1000);
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('a Snapshot replaces them, by id; a Delta changes and adds some and takes others away, each gone at its own time', () => {
    const s = pouches.getState();
    s.applySnapshot([pouch(7, ['slime_gel']), pouch(2, ['red_potion'], 30000)]);
    expect(items()).toEqual([
      [2, ['red_potion']],
      [7, ['slime_gel']],
    ]);
    expect(pouches.getState().pouches.map((p) => p.goneAt)).toEqual([31000, 61000]);
    s.applyDelta([pouch(7, ['slime_gel', 'bat_wing']), pouch(9, ['iron_sword'])], [2]);
    expect(items()).toEqual([
      [7, ['slime_gel', 'bat_wing']],
      [9, ['iron_sword']],
    ]);
    s.applySnapshot([pouch(1, ['red_potion'])]);
    expect(items()).toEqual([[1, ['red_potion']]]);
  });

  it('an empty Delta changes nothing, nor does the same reach again, so nothing watching them redraws', () => {
    const s = pouches.getState();
    s.applySnapshot([pouch(7, ['slime_gel'])]);
    s.setInReach([7]);
    const before = pouches.getState();
    s.applyDelta([], []);
    s.setInReach([7]);
    expect(pouches.getState()).toBe(before);
  });

  it('Take all takes every item of the pouches in reach, and none of those out of it', () => {
    const s = pouches.getState();
    s.applySnapshot([
      pouch(1, ['slime_gel', 'red_potion']),
      pouch(2, ['bat_wing']),
      pouch(3, ['iron_sword']),
    ]);
    s.setInReach([1, 3]);
    expect(picksInReach(pouches.getState())).toEqual([
      { pouch: 1, item: 1 },
      { pouch: 1, item: 2 },
      { pouch: 3, item: 1 },
    ]);
  });

  it('a pick asked for is taken until its item leaves its pouch, and is never asked for twice meanwhile', async () => {
    const s = pouches.getState();
    s.applySnapshot([pouch(1, ['slime_gel', 'red_potion'])]);
    const { request, asked } = answering([0]);
    const first = s.take([{ pouch: 1, item: 1 }], request);
    expect([...pouches.getState().taking]).toEqual(['1/1']);
    await s.take(
      [
        { pouch: 1, item: 1 },
        { pouch: 1, item: 2 },
      ],
      request,
    );
    await first;
    expect(asked).toEqual([[[1, 1]], [[1, 2]]]);
    expect([...pouches.getState().taking]).toEqual(['1/1', '1/2']); // granted: they leave with the next Delta
    s.applyDelta([pouch(1, ['slime_gel'])], []); // item 2 left its pouch
    expect([...pouches.getState().taking]).toEqual(['1/1']);
    s.applyDelta([], [1]);
    expect([...pouches.getState().taking]).toEqual([]);
  });

  it('a refused pick shows why until it is asked for again; a refused take refuses each of its picks', async () => {
    const s = pouches.getState();
    s.applySnapshot([pouch(1, ['slime_gel', 'red_potion'])]);
    await s.take(
      [
        { pouch: 1, item: 1 },
        { pouch: 1, item: 2 },
      ],
      answering([ERR.ETC_TAB_FULL, 0]).request,
    );
    expect([...pouches.getState().refused]).toEqual([['1/1', errorText(ERR.ETC_TAB_FULL)]]);
    expect([...pouches.getState().taking]).toEqual(['1/2']);
    const again = s.take(
      [{ pouch: 1, item: 1 }],
      answering(new Error('No answer from the server.')).request,
    );
    expect([...pouches.getState().refused]).toEqual([]);
    await again;
    expect([...pouches.getState().refused]).toEqual([['1/1', 'No answer from the server.']]);
    expect([...pouches.getState().taking]).toEqual(['1/2']);
  });

  it('a pick whose item left before its answer came keeps no refusal', async () => {
    const s = pouches.getState();
    s.applySnapshot([pouch(1, ['slime_gel'])]);
    let answer: (codes: number[]) => void = () => undefined;
    const taking = s.take([{ pouch: 1, item: 1 }], (picks) => {
      return new Promise((resolve) => {
        answer = (codes) =>
          resolve(
            create(ResponseSchema, {
              ok: true,
              picks: picks.map((pick, i) => ({ pick, code: codes[i] ?? 0 })),
            }),
          );
      });
    });
    s.applyDelta([], [1]); // its life ended
    answer([ERR.POUCH_GONE]);
    await taking;
    expect([pouches.getState().refused.size, pouches.getState().taking.size]).toEqual([0, 0]);
  });

  it('clear forgets them all', async () => {
    const s = pouches.getState();
    s.applySnapshot([pouch(1, ['slime_gel'])]);
    s.setInReach([1]);
    await s.take([{ pouch: 1, item: 1 }], answering([ERR.POUCH_OUT_OF_REACH]).request);
    s.clear();
    expect(pouches.getState()).toMatchObject({ pouches: [], inReach: [] });
    expect([pouches.getState().taking.size, pouches.getState().refused.size]).toEqual([0, 0]);
  });
});
