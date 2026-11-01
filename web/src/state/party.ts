import type { Data, PartyView } from '@/protocol/messages';

import { defineStore } from './store';

export interface Invite {
  readonly inviteId: string;
  readonly from: string;
  readonly fromName: string;
  readonly at: number; // Unix ms; expires 30 s later
}

export interface PartyState {
  readonly party: PartyView | null;
  readonly invites: readonly Invite[];
  applyWelcome(d: Data<'welcome'>): void;
  applyPartyState(d: Data<'partyState'>): void;
  applyPartyInvited(d: Data<'partyInvited'>, at?: number): void;
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
    set({ party: d.party ?? null });
  },
  applyPartyInvited: ({ inviteId, from, fromName }, at = Date.now()) => {
    const invites = get().invites.filter((i) => i.inviteId !== inviteId);
    set({ invites: [...invites, { inviteId, from, fromName, at }] });
  },
  dismissInvite: (inviteId) => {
    set({ invites: get().invites.filter((i) => i.inviteId !== inviteId) });
  },
  clear: () => {
    set({ party: null, invites: [] });
  },
}));

export function isMember(p: PartyView | null, id: string): boolean {
  return p?.members.some((m) => m.characterId === id) ?? false;
}
