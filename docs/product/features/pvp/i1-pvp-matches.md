# I1. PvP matches

- **Feature:** I1. PvP matches
- **Group:** I. PvP
- **Priority:** P1
- **Status:** Agreed

**Story:** As a promoted player, I queue for a PvP match from anywhere and keep hunting while I wait. Within seconds I fight real players near my rating, or bots when no one is around. Everyone fights with the same stats, so my skills, traits and play decide. When it's over I'm back where I was.

## Acceptance

- **Modes:** 1v1 and 3v3 in the arena (I2), and 15v15 on the battlefield (I3). Guild war later puts one guild on each side of the 15v15 mode.
- **Who can queue:** a character with a class (Warrior, Thief, Bowman or Magician). Beginners can't: their kit is a starter's.
- **Queueing:**
  - From the Arena window (V), anywhere in the world, alive or not, in combat or not. One mode at a time.
  - While queued, the player plays on: hunting, changing maps, talking to NPCs. A small queue tag by the minimap shows the mode and the time waited, with a button to leave the queue.
  - A party leader queues their party together for 3v3 (two or three members) or 15v15 (up to four). Every member must be promoted, online and not in a match. The party fights on the same side.
- **Matchmaking:**
  - The queue puts players close in rating together (I4), and widens what counts as close the longer they wait.
  - **Bots:** anyone still waiting after 15 seconds is matched with bots: as opponents, and as teammates in 3v3 and 15v15. A bot's strength follows the rating of the players it meets.
  - So today, with few players, nearly every match has bots. As players arrive, they meet each other first, with no change.
- **Match found:**
  - Each player gets a "Match found" card for 10 seconds, saying the mode and how many bots are in it, with Accept and Decline.
  - When everyone accepts, they're taken into the match at once.
  - Declining, or letting the time run out, leaves the queue. Anyone who accepted goes back to the front of the queue, keeping their wait.
  - A dead player can't accept until they're back on their feet.
- **Equal stats, own build:**
  - In a match every fighter is level 30, with their class's set stats and set gear. Their own level, stat points and gear don't count.
  - Every skill of their class counts as fully learned, at its last evolution, with every trait open.
  - **The arena build:** each character has a second skill bar and a second set of trait choices for matches, set in the skill window's Arena tab (K). It starts as a copy of the world bar and traits. In a match the skill bar shows the arena build.
  - The build can be changed before a match starts and between rounds. A change applies from the next round.
- **Fighting other players:**
  - Attacks and skills hit enemies as they hit mobs, by their shapes, and hit no one on the player's own side.
  - The lock (C15) takes enemy players, with Tab switching between them. The target window shows the player's name, class and HP.
  - Dodges pass through enemies' attacks and skills, and blocks stop them from the front (C21). Nothing a player does is unblockable. Players' attacks draw no warning areas: the swing is the warning.
  - Statuses work on players as on mobs (C19), and break-free skills get out of them. Taunt and losing sight work on mobs only.
  - A player's hit has no hit guard after it, so combos land in full. A skill's push moves the target; a plain hit doesn't knock them back.
  - Buffs and heals reach the player's teammates, in place of their party.
  - Chains (C18) read an enemy as they read a mob: a stunned enemy offers Ground Slam on Strike's key, and a dodged or blocked enemy attack opens the dodge's and block's follow-ups.
- **In a match:**
  - No potions or other items, no equipping, no spending stat or skill points, and no portals or teleports. The world's trait choices can't change either; the arena build can, between rounds. The window says why.
  - Health doesn't regenerate.
  - Going down costs no EXP and no items, and drops nothing. No EXP, drops or quest progress come from a match.
  - Chat, whispers and the party window work as usual. Party members outside see "In a match" on the player's frame.
- **Players and bots look apart:**
  - Enemies' names are red and teammates' green.
  - A bot carries a BOT tag over its head and on its frames. Bots have their own names and play by the same rules as players.
- **Leaving:**
  - A Leave match button forfeits. Logging out does too.
  - **A lost connection holds the player's place for 20 seconds.** A closed tab, a reload or a crash counts as one, since the game can't tell them apart.
    - Their character stays in the match, standing still, with everything on it running on. It can be hit and downed, and a new round brings it back up as anyone.
    - Everyone sees "Reconnecting" on their frame and over their name, and their side is told "<name> lost connection".
    - Coming back within the 20 seconds puts the player straight back into the match, on their character as it stands, with nothing reset. Their side is told "<name> is back".
    - Past the 20 seconds, it's a forfeit.
  - In 1v1 the opponent wins at once. In 3v3 and 15v15 a bot takes the leaver's place, and the match goes on.
  - A forfeit counts as a loss for the leaver. A match that ends while a player's place is held counts by its result for them, as for anyone there.
- **Afterwards:** a result screen shows the outcome, then the player is taken back to where they queued from, with the HP, energy, buffs and cooldowns they had. Cooldowns kept running while they were away.
- **After a relog,** or a lost connection not back within 20 seconds, the player is back where they queued from, and the match is a loss for them.

## Not in this feature

- Guilds and guild war.
- Fighting in the open world, duels on request, and spectating.
- Rewards beyond rating and rank: no items, EXP or currency.
- Joining a match already under way. Coming back to one's own held place after a lost connection isn't that.
- Choosing a map.

## Rules

Systems → Combat. The modes are I2 and I3; rating and ranks are I4.
