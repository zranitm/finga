import { create } from '@bufbuild/protobuf';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import {
  MatchEndedSchema,
  MatchOutcome,
  MatchPhase,
  MatchStateSchema,
  PvpOfferGoneReason,
  PvpOfferGoneSchema,
  PvpOfferSchema,
  PvpQueuedSchema,
} from '@/protocol/messages';
import { resetStores } from '@/testing/reset-stores';

import { countdownTicks, pvp, sideOf, walled } from './pvp';

const duel = (phase: MatchPhase, wins: number[] = [0, 0], match = 'mt-1') =>
  create(MatchStateSchema, {
    match,
    mode: 'duel',
    phase,
    round: 1,
    wins,
    endsInMs: 3000,
    sides: [
      { fighters: [{ id: 'c-robin', name: 'Robin', class: 'thief' }] },
      { fighters: [{ id: 'c-ayla', name: 'Ayla', class: 'warrior' }] },
    ],
  });

describe('pvp', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    resetStores();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('counts a ticket’s wait from when the queue said it had waited, and an offer’s lapse from its time left', () => {
    vi.advanceTimersByTime(50_000);
    const now = performance.now();
    pvp
      .getState()
      .applyQueued(
        create(PvpQueuedSchema, { mode: 'team', sinceMs: 4000, members: ['c-ayla', 'c-robin'] }),
      );
    expect(pvp.getState().queued).toEqual({
      mode: 'team',
      since: now - 4000,
      members: ['c-ayla', 'c-robin'],
      waitingRoom: false,
    });
    pvp
      .getState()
      .applyQueued(create(PvpQueuedSchema, { mode: 'team', sinceMs: 20_000, waitingRoom: true }));
    expect(pvp.getState().queued?.waitingRoom).toBe(true);
    pvp
      .getState()
      .applyOffer(create(PvpOfferSchema, { offer: 'of-1', mode: 'duel', bots: 1, untilMs: 9500 }));
    expect(pvp.getState().offer).toEqual({
      id: 'of-1',
      mode: 'duel',
      bots: 1,
      until: now + 9500,
    });
  });

  it('an offer that falls through keeps the player queued when the world put them back, else leaves the queue too; another offer’s end changes nothing', () => {
    const offer = create(PvpOfferSchema, { offer: 'of-1', mode: 'duel', untilMs: 10_000 });
    const gone = (o: string, requeued: boolean) =>
      create(PvpOfferGoneSchema, { offer: o, reason: PvpOfferGoneReason.DECLINED, requeued });
    pvp.getState().applyQueued(create(PvpQueuedSchema, { mode: 'duel' }));
    pvp.getState().applyOffer(offer);
    pvp.getState().applyOfferGone(gone('of-0', false));
    expect(pvp.getState().offer?.id).toBe('of-1');
    pvp.getState().applyOfferGone(gone('of-1', true));
    expect([pvp.getState().offer, pvp.getState().queued?.mode]).toEqual([null, 'duel']);
    pvp.getState().applyOffer(offer);
    pvp.getState().applyOfferGone(gone('of-1', false));
    expect([pvp.getState().offer, pvp.getState().queued]).toEqual([null, null]);
  });

  it('a match’s state leaves the queue and its offer, times its phase from now and from the world’s tick it came at, and a new match drops the last one’s outcome', () => {
    pvp.getState().applyQueued(create(PvpQueuedSchema, { mode: 'duel' }));
    pvp.getState().applyOffer(create(PvpOfferSchema, { offer: 'of-1', mode: 'duel' }));
    pvp.getState().applyMatchState(duel(MatchPhase.PREP), 4120);
    const m = pvp.getState().match;
    expect([pvp.getState().queued, pvp.getState().offer]).toEqual([null, null]);
    expect(m).toMatchObject({
      id: 'mt-1',
      mode: 'duel',
      phase: MatchPhase.PREP,
      round: 1,
      endsAt: performance.now() + 3000,
      sent: { tick: 4120, endsInMs: 3000 },
      sides: [
        [{ id: 'c-robin', name: 'Robin', cls: 'thief', bot: false }],
        [{ id: 'c-ayla', name: 'Ayla', cls: 'warrior', bot: false }],
      ],
    });
    expect([
      sideOf(m, 'c-ayla'),
      sideOf(m, 'c-robin'),
      sideOf(m, 'c-kai'),
      sideOf(null, 'c-ayla'),
    ]).toEqual([2, 1, 0, 0]);
    pvp
      .getState()
      .applyMatchEnded(
        create(MatchEndedSchema, { result: MatchOutcome.WIN, wins: [1, 2] }),
        'c-ayla',
      );
    pvp.getState().applyMatchState(duel(MatchPhase.RESULT, [1, 2]), 0);
    expect(pvp.getState().outcome?.result).toBe(MatchOutcome.WIN); // the same match's
    pvp.getState().applyMatchState(duel(MatchPhase.PREP, [0, 0], 'mt-2'), 0);
    expect(pvp.getState().outcome).toBeNull();
  });

  it('an outcome counts rounds from the player’s side and carries the rating’s change; it stays after the match is left till dismissed', () => {
    pvp.getState().applyMatchState(duel(MatchPhase.RESULT, [1, 2]), 0);
    pvp.getState().applyMatchEnded(
      create(MatchEndedSchema, {
        result: MatchOutcome.WIN,
        wins: [1, 2],
        rating: {
          before: 1000,
          after: 1032,
          rankBefore: 'bronze',
          rankAfter: 'bronze',
          placementLeft: 3,
        },
      }),
      'c-ayla',
    );
    expect(pvp.getState().outcome).toEqual({
      mode: 'duel',
      result: MatchOutcome.WIN,
      rounds: [2, 1],
      rating: {
        before: 1000,
        after: 1032,
        rankBefore: 'bronze',
        rankAfter: 'bronze',
        placementLeft: 3,
      },
    });
    pvp.getState().leftMatch();
    expect([pvp.getState().match, pvp.getState().outcome?.rounds]).toEqual([null, [2, 1]]);
    pvp.getState().dismissOutcome();
    expect(pvp.getState().outcome).toBeNull();
    pvp
      .getState()
      .applyMatchEnded(
        create(MatchEndedSchema, { result: MatchOutcome.LOSS, wins: [2, 0] }),
        'c-robin',
      );
    expect(pvp.getState().outcome).toMatchObject({ mode: '', rounds: [2, 0], rating: null });
  });

  it('a forfeit’s outcome, whose MapChange comes before its MatchEnded, keeps the match’s mode and counts rounds from the player’s side, side 2 here', () => {
    pvp.getState().applyMatchState(duel(MatchPhase.FIGHT, [1, 0]), 0);
    pvp.getState().leftMatch(); // the forfeiter's MapChange
    expect(pvp.getState().match).toBeNull();
    pvp
      .getState()
      .applyMatchEnded(
        create(MatchEndedSchema, { result: MatchOutcome.LOSS, wins: [1, 0] }),
        'c-ayla',
      );
    expect(pvp.getState().outcome).toMatchObject({ mode: 'duel', rounds: [0, 1] });
    expect(pvp.getState().left).toBeNull();
    pvp.getState().applyMatchState(duel(MatchPhase.FIGHT, [0, 0], 'mt-2'), 0);
    pvp.getState().leftMatch();
    pvp.getState().dismissOutcome(); // the card closed: no MatchEnded is waited for
    expect(pvp.getState().left).toBeNull();
  });

  it('leaving a team match under way is a quit, its card not waited for; a duel’s forfeit, or a team match left once decided, keeps the match for its card', () => {
    const team = (phase: MatchPhase) => ({ ...duel(phase, [0, 0], 'mt-3'), mode: 'team' });
    pvp.getState().applyMatchState(team(MatchPhase.FIGHT), 0);
    expect(pvp.getState().leftMatch()).toBe(true);
    expect([pvp.getState().match, pvp.getState().left]).toEqual([null, null]);
    expect(pvp.getState().leftMatch()).toBe(false); // already out
    pvp.getState().applyMatchState(team(MatchPhase.RESULT), 0);
    expect(pvp.getState().leftMatch()).toBe(false);
    expect(pvp.getState().left?.id).toBe('mt-3');
    pvp.getState().applyMatchState(duel(MatchPhase.FIGHT, [0, 0], 'mt-4'), 0);
    expect(pvp.getState().leftMatch()).toBe(false);
    expect(pvp.getState().left?.id).toBe('mt-4');
  });

  it('reads the world’s ticks the count into a round holds everyone: the next in prep and between rounds, the rest of one under way, none in a fight or after', () => {
    const at = (phase: MatchPhase, tick: number) => {
      pvp.getState().applyMatchState(duel(phase), tick); // 3 s left: 60 ticks of 50 ms
      return countdownTicks(pvp.getState().match, 50);
    };
    expect(countdownTicks(null, 50)).toBeNull();
    expect(at(MatchPhase.PREP, 1000)).toEqual({ from: 1060, until: 1120 }); // pvp.yaml's 3 s count
    expect(countdownTicks(pvp.getState().match, 100)).toEqual({ from: 1030, until: 1060 }); // at 100 ms ticks
    expect(at(MatchPhase.BETWEEN, 2000)).toEqual({ from: 2060, until: 2120 });
    expect(at(MatchPhase.COUNTDOWN, 2060)).toEqual({ from: 2060, until: 2120 });
    expect(at(MatchPhase.FIGHT, 2120)).toBeNull();
    expect(at(MatchPhase.RESULT, 3000)).toBeNull();
  });

  it('the wall stands and everyone holds through prep and the count into a round only', () => {
    expect(
      [
        MatchPhase.PREP,
        MatchPhase.COUNTDOWN,
        MatchPhase.FIGHT,
        MatchPhase.BETWEEN,
        MatchPhase.RESULT,
        undefined,
      ].map(walled),
    ).toEqual([true, true, false, false, false, false]);
  });
});
