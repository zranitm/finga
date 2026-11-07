export const REGISTRY = {
  socket: 'socket',
  assetsReady: 'assetsReady',
  /** The game's current visit to the world; a World started for an earlier one builds nothing. */
  visit: 'visit',
} as const;

export const EVENTS = {
  assetsReady: 'assets-ready',
} as const;
