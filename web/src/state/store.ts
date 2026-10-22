// How every store is built (page 7): a Zustand vanilla store that Phaser
// code reads with getState and subscribe, plus a React hook over it with a
// selector, so each component re-renders only when its slice changes. A
// store action is the only way state changes; actions take protocol
// payloads where there is one.
import { useStore } from 'zustand/react';
import { createStore } from 'zustand/vanilla';
import type { StoreApi } from 'zustand/vanilla';

export interface Store<S> {
  readonly api: StoreApi<S>;
  readonly getState: () => S;
  readonly subscribe: StoreApi<S>['subscribe'];
  /** Back to the initial state; tests call it between cases. */
  readonly reset: () => void;
  /** React: read a slice. */
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
