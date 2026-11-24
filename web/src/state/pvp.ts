// PvP (pvp.md sections 11 and 13): the queue the player waits in, a match offer, the match under way and its outcome, as the server says them; each wait and time counts locally from when its message came, in performance.now() milliseconds.
import { pvp as pvpData } from '@/gamedata/pvp';
import { MatchPhase } from '@/protocol/messages';
import type { Data, MatchOutcome } from '@/protocol/messages';

import { defineStore } from './store';

export interface Queued {
  readonly mode: string;
  /** When the ticket began waiting. */
  readonly since: number;
  /** The ticket's characters: the player alone, or their party. */
  readonly members: readonly string[];
  /** Their match is found but waits for the server to have room for its bots. */
  readonly waitingRoom: boolean;
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
  /** The world tick the state was sent at, and the phase's time left then, as the world counts it in ticks. */
  readonly sent: { readonly tick: number; readonly endsInMs: number };
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
  /** The match the player was moved out of, kept for its MatchEnded, which a forfeit's MapChange comes before. */
  readonly left: Match | null;
  /** The last match's outcome, shown till the player closes it. */
  readonly outcome: Outcome | null;
  applyQueued(d: Data<'pvpQueued'>): void;
  applyUnqueued(): void;
  applyOffer(d: Data<'pvpOffer'>): void;
  /** An offer ended without a match: the player stays queued if the world put them back. */
  applyOfferGone(d: Data<'pvpOfferGone'>): void;
  /** The match's state, sent at the world's tick: in it, the player has left the queue and its offer. */
  applyMatchState(d: Data<'matchState'>, tick: number): void;
  applyMatchEnded(d: Data<'matchEnded'>, me: string): void;
  /** The player was moved out of the match's map; true when they left it under way and it runs on without them, so its end comes as a line, not the card. */
  leftMatch(): boolean;
  dismissOutcome(): void;
  clear(): void;
}

/** Whether a match of mode runs on without a fighter who leaves, a bot in their place, as a team mode's does; a 1v1 ends with them. */
export function runsOn(mode: string): boolean {
  return (pvpData.modes.find((m) => m.id === mode)?.team ?? 1) > 1;
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
  left: null,
  outcome: null,
  applyQueued: (d) => {
    set({
      queued: {
        mode: d.mode,
        since: performance.now() - d.sinceMs,
        members: d.members,
        waitingRoom: d.waitingRoom,
      },
    });
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
  applyMatchState: (d, tick) => {
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
        sent: { tick, endsInMs: d.endsInMs },
        sides: d.sides.map((s) =>
          s.fighters.map((f) => ({ id: f.id, name: f.name, cls: f.class, bot: f.bot })),
        ),
      },
      left: null,
      ...(was?.id !== d.match && { outcome: null }),
    });
  },
  applyMatchEnded: (d, me) => {
    const m = get().match ?? get().left;
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
      left: null,
    });
  },
  leftMatch: () => {
    const m = get().match;
    if (!m) {
      return false;
    }
    const quit = m.phase !== MatchPhase.RESULT && runsOn(m.mode);
    set({ match: null, left: quit ? null : m });
    return quit;
  },
  dismissOutcome: () => {
    set({ outcome: null, left: null });
  },
  clear: () => {
    set({ queued: null, offer: null, match: null, left: null, outcome: null });
  },
}));

/** Whether the match holds everyone still: before the first round, and in the count into each. */
export function walled(phase: MatchPhase | undefined): boolean {
  return phase === MatchPhase.PREP || phase === MatchPhase.COUNTDOWN;
}

/** The world's ticks the count into a round holds everyone still, the last exclusive, as the match's state tells them: the next count in prep and between rounds, which starts as they end, and the rest of one under way; none otherwise. */
export function countdownTicks(
  m: Match | null,
  tickMs: number,
): { readonly from: number; readonly until: number } | null {
  if (!m) {
    return null;
  }
  const ends = m.sent.tick + Math.round(m.sent.endsInMs / tickMs);
  switch (m.phase) {
    case MatchPhase.PREP:
    case MatchPhase.BETWEEN:
      return { from: ends, until: ends + Math.round(pvpData.countdown_ms / tickMs) };
    case MatchPhase.COUNTDOWN:
      return { from: m.sent.tick, until: ends };
    default:
      return null;
  }
}
