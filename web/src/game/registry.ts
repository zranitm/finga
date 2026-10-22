// Keys of what the game shares between its scenes and the app through the
// Phaser registry and game events: the socket, and the assets-ready flag.
export const REGISTRY = {
  socket: 'socket',
  assetsReady: 'assetsReady',
} as const;

export const EVENTS = {
  assetsReady: 'assets-ready',
} as const;
