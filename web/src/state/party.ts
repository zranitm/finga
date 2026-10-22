// The party as the server last described it, and the invites waiting for
// an answer. Leader and member order follow the payload.
import type { Data, PartyView } from '@/protocol/messages';

import { defineStore } from './store';

export interface Invite {
  readonly invite_id: string;
  readonly from: string;
  readonly from_name: string;
  readonly at: number; // Unix milliseconds, when it arrived; it expires 30 s later
}

export interface PartyState {
  readonly party: PartyView | null;
  readonly invites: readonly Invite[];
  applyWelcome(d: Data<'Welcome'>): void;
  applyPartyState(d: Data<'PartyState'>): void;
  applyPartyInvited(d: Data<'PartyInvited'>, at?: number): void;
  /** Answered or expired: the invite leaves the list. */
  dismissInvite(inviteId: string): void;
  clear(): void;
}

export const party = defineStore<PartyState>((set, get) => ({
  party: null,
  invites: [],
  applyWelcome: (d) => {
    set({ party: d.party ?? null });
  },
  applyPartyState: (d) => {
    set({ party: d.party });
  },
  applyPartyInvited: (d, at = Date.now()) => {
    const invites = get().invites.filter((i) => i.invite_id !== d.invite_id);
    set({ invites: [...invites, { ...d, at }] });
  },
  dismissInvite: (inviteId) => {
    set({ invites: get().invites.filter((i) => i.invite_id !== inviteId) });
  },
  clear: () => {
    set({ party: null, invites: [] });
  },
}));

/** True when id is in the party. */
export function isMember(p: PartyView | null, id: string): boolean {
  return p?.members.some((m) => m.character_id === id) ?? false;
}
