import type { Data, PartyView } from '@/protocol/messages';

import { defineStore } from './store';

export interface Invite {
  readonly inviteId: string;
  readonly from: string;
  readonly fromName: string;
  readonly at: number; // Unix ms; expires 30 s later
}

/** Where a party member stands, as the server last said. */
export interface Spot {
  readonly x: number;
  readonly z: number;
}

export interface PartyState {
  readonly party: PartyView | null;
  readonly invites: readonly Invite[];
  /** By character id: the members on the player's map whom the frames don't bring, where they stood at the server's latest word, about once a second. */
  readonly spots: Readonly<Record<string, Spot>>;
  /** A Welcome clears the party; social sends the current one in a PartyState right after. */
  applyWelcome(): void;
  applyPartyState(d: Data<'partyState'>): void;
  applyPartyInvited(d: Data<'partyInvited'>): void;
  applyPartySpots(d: Data<'partySpots'>): void;
  /** A map's snapshot drops the last map's spots. */
  clearSpots(): void;
  dismissInvite(inviteId: string): void;
  clear(): void;
}

export const party = defineStore<PartyState>((set, get) => ({
  party: null,
  invites: [],
  spots: {},
  applyWelcome: () => {
    set({ party: null, spots: {} });
  },
  applyPartyState: (d) => {
    set({ party: d.party ?? null });
  },
  applyPartyInvited: ({ inviteId, from, fromName }) => {
    const invites = get().invites.filter((i) => i.inviteId !== inviteId);
    set({ invites: [...invites, { inviteId, from, fromName, at: Date.now() }] });
  },
  applyPartySpots: ({ spots }) => {
    set({ spots: Object.fromEntries(spots.map((s) => [s.characterId, { x: s.x, z: s.z }])) });
  },
  clearSpots: () => {
    set({ spots: {} });
  },
  dismissInvite: (inviteId) => {
    set({ invites: get().invites.filter((i) => i.inviteId !== inviteId) });
  },
  clear: () => {
    set({ party: null, invites: [], spots: {} });
  },
}));

export function isMember(p: PartyView | null, id: string): boolean {
  return p?.members.some((m) => m.characterId === id) ?? false;
}
