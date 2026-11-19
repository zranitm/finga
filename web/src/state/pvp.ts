// PvP (pvp.md sections 11 and 13): the queue the player waits in, a match offer, the match under way and its outcome, as the server says them; each wait and time counts locally from when its message came, in performance.now() milliseconds.
import { MatchPhase } from '@/protocol/messages';
import type { Data, MatchOutcome } from '@/protocol/messages';

import { defineStore } from './store';

export interface Queued {
  readonly mode: string;
  /** When the ticket began waiting. */
  readonly since: number;
}

export interface Offer {
  readonly id: string;
  readonly mode: string;
  /** How many of the match's places bots fill. */
  readonly bots: number;
  /** When it lapses. */
  readonly until: number;
}

export interface Fighter {
  readonly id: string;
  readonly name: string;
  readonly cls: string;
  readonly bot: boolean;
}

export interface Match {
  readonly id: string;
  readonly mode: string;
  readonly phase: MatchPhase;
  /** The round under way or next, from 1. */
  readonly round: number;
  /** Rounds won by side 1 and side 2. */
  readonly wins: readonly number[];
  /** When the phase ends. */
  readonly endsAt: number;
  /** Side 1's fighters, then side 2's. */
  readonly sides: readonly (readonly Fighter[])[];
}

export interface RatingChange {
  readonly before: number;
  readonly after: number;
  readonly rankBefore: string;
  readonly rankAfter: string;
  /** Placement matches still to play, 0 once placed. */
  readonly placementLeft: number;
}

export interface Outcome {
  readonly mode: string;
  readonly result: MatchOutcome;
  /** Rounds won by the player's side, then the other's. */
  readonly rounds: readonly [number, number];
  readonly rating: RatingChange | null;
}

export interface PvpState {
  readonly queued: Queued | null;
  readonly offer: Offer | null;
  readonly match: Match | null;
  /** The last match's outcome, shown till the player closes it. */
  readonly outcome: Outcome | null;
  applyQueued(d: Data<'pvpQueued'>): void;
  applyUnqueued(): void;
  applyOffer(d: Data<'pvpOffer'>): void;
  /** An offer ended without a match: the player stays queued if the world put them back. */
  applyOfferGone(d: Data<'pvpOfferGone'>): void;
  /** The match's state: in it, the player has left the queue and its offer. */
  applyMatchState(d: Data<'matchState'>): void;
  applyMatchEnded(d: Data<'matchEnded'>, me: string): void;
  /** The player was moved out of the match's map. */
  leftMatch(): void;
  dismissOutcome(): void;
  clear(): void;
}

/** Which side, 1 or 2, the player fights on in match, 0 for neither. */
export function sideOf(match: Match | null, me: string): number {
  const i = match?.sides.findIndex((s) => s.some((f) => f.id === me)) ?? -1;
  return i + 1;
}

export const pvp = defineStore<PvpState>((set, get) => ({
  queued: null,
  offer: null,
  match: null,
  outcome: null,
  applyQueued: (d) => {
    set({ queued: { mode: d.mode, since: performance.now() - d.sinceMs } });
  },
  applyUnqueued: () => {
    set({ queued: null, offer: null });
  },
  applyOffer: (d) => {
    set({
      offer: {
        id: d.offer,
        mode: d.mode,
        bots: d.bots,
        until: performance.now() + d.untilMs,
      },
    });
  },
  applyOfferGone: (d) => {
    if (get().offer?.id !== d.offer) {
      return;
    }
    set({ offer: null, ...(!d.requeued && { queued: null }) });
  },
  applyMatchState: (d) => {
    const was = get().match;
    set({
      queued: null,
      offer: null,
      match: {
        id: d.match,
        mode: d.mode,
        phase: d.phase,
        round: d.round,
        wins: d.wins,
        endsAt: performance.now() + d.endsInMs,
        sides: d.sides.map((s) =>
          s.fighters.map((f) => ({ id: f.id, name: f.name, cls: f.class, bot: f.bot })),
        ),
      },
      ...(was?.id !== d.match && { outcome: null }),
    });
  },
  applyMatchEnded: (d, me) => {
    const m = get().match;
    const side = sideOf(m, me);
    const [one = 0, two = 0] = d.wins;
    const r = d.rating;
    set({
      outcome: {
        mode: m?.mode ?? '',
        result: d.result,
        rounds: side === 2 ? [two, one] : [one, two],
        rating: r
          ? {
              before: r.before,
              after: r.after,
              rankBefore: r.rankBefore,
              rankAfter: r.rankAfter,
              placementLeft: r.placementLeft,
            }
          : null,
      },
    });
  },
  leftMatch: () => {
    set({ match: null });
  },
  dismissOutcome: () => {
    set({ outcome: null });
  },
  clear: () => {
    set({ queued: null, offer: null, match: null, outcome: null });
  },
}));

/** Whether the match holds everyone still: before the first round, and in the count into each. */
export function walled(phase: MatchPhase | undefined): boolean {
  return phase === MatchPhase.PREP || phase === MatchPhase.COUNTDOWN;
}
