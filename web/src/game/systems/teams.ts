// Sides in a match (pvp.md section 5): a player on the other side is an enemy, one on the player's own a teammate; outside a match, where every team is 0, no one is either.

export type Relation = 'foe' | 'friend' | 'none';

/** How a player on team theirs stands to one on team mine. */
export function relationOf(mine: number, theirs: number): Relation {
  if (mine === 0 || theirs === 0) {
    return 'none';
  }
  return mine === theirs ? 'friend' : 'foe';
}
