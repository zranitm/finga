# C10. Field bosses

- **Feature:** C10. Field bosses
- **Group:** C. Combat and progression
- **Priority:** P1
- **Status:** Draft

**Story:** As a player, I hunt on a map until its field boss appears, then fight it with whoever is there, for a share of its EXP and a roll on its drops.

## Acceptance

- **One boss per hunting map:** each is a much bigger, stronger, and tougher version of its map's mob, with its own name. Towns have none.
- **Appearing:**
  - Each map counts the mobs killed on it, by anyone. When the count reaches the map's number, the boss appears at its spot.
  - Players don't see the count.
  - Every player in the game is told: "King Slime has appeared in the Forest!"
- **One at a time:** while a boss is up, kills don't count. After it's beaten or leaves, the count starts again from 0.
- **Leaving:** a boss nobody beats leaves after 10 minutes and drops nothing. Players on the map are told.
- **The fight:**
  - A boss HP bar at the top of the screen shows its name, its HP, and the time it has left, to everyone on the map.
  - It fights like other mobs, with its touch, only much harder.
- **Rewards,** for players alive on the map when it falls:
  - **EXP** is split by the damage each player dealt. A party's damage adds up, and its share is split among its members as for any kill.
  - **Drops are personal:** each player whose damage, or whose party's, reached 1% of its HP gets their own roll on its drop list. Nobody can take another's.
  - A player who is dead, or off the map, when it falls gets nothing, even if they hit it.
- **Fairness:** every player on the map sees the same boss, and the server decides every count, hit, and reward.

## Not in this feature

- Special attacks for bosses (later).
- Bosses in party dungeons (C7).

## Rules

Systems → Combat → Field bosses
