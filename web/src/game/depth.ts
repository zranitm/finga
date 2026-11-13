/** The draw order, back to front; things at one depth draw in the order they were made. */
export const DEPTH = {
  background: -4,
  glow: -3,
  tiles: -2,
  label: -1, // a map's label: over a wall, never over someone
  actor: 0, // mobs, a boss among them, NPCs and drops
  afterimage: 0.5, // a cancel's afterimage: under the player, over the mobs
  player: 1, // over every mob, so one standing inside a boss still shows
  effect: 5, // the looks of attacks, skills and hits
  damage: 10,
  banner: 20,
} as const;
