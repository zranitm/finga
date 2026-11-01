// Vanilla store for Phaser (getState/subscribe) plus a selector hook for React.
import { useStore } from 'zustand/react';
import { createStore } from 'zustand/vanilla';
import type { StoreApi } from 'zustand/vanilla';

export interface Store<S> {
  readonly api: StoreApi<S>;
  readonly getState: () => S;
  readonly subscribe: StoreApi<S>['subscribe'];
  readonly reset: () => void;
  readonly use: <T>(selector: (s: S) => T) => T;
}

export function defineStore<S extends object>(
  init: (set: StoreApi<S>['setState'], get: StoreApi<S>['getState']) => S,
): Store<S> {
  const api = createStore<S>((set, get) => init(set, get));
  const initial = api.getState();
  return {
    api,
    getState: api.getState,
    subscribe: api.subscribe,
    reset: () => {
      api.setState(initial, true);
    },
    use: (selector) => useStore(api, selector),
  };
}
