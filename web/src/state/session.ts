// The account and whether it is logged in. The token itself stays in
// net/token.ts (C04); this store only drives the screen flow.
import { defineStore } from './store';

export interface Account {
  readonly id: string;
  readonly username: string;
}

export interface SessionState {
  readonly account: Account | null;
  readonly loggedIn: boolean;
  logIn(account: Account): void;
  logOut(): void;
}

export const session = defineStore<SessionState>((set) => ({
  account: null,
  loggedIn: false,
  logIn: (account) => {
    set({ account, loggedIn: true });
  },
  logOut: () => {
    set({ account: null, loggedIn: false });
  },
}));
