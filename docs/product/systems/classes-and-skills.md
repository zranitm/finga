# Classes and skills

## Classes

- **Everyone starts as a Beginner,** with no class. Character creation asks only for a name (A2).
- **Classes form a tree.** A Beginner can be promoted to one of the first classes: **Warrior**, **Thief**, **Bowman**, or **Magician**. More classes come later as further branches: other first classes, or classes a promoted character can be promoted to again. Four isn't the final set.
- **A class decides how the character fights and which skills they can learn:**

  | Class | Basic attack | Reach | Damage |
  | --- | --- | --- | --- |
  | Beginner | a short melee swing | a 120° arc, 1.9 m | physical |
  | Warrior | melee | a 120° arc, 2.4 m | physical |
  | Thief | fast, short reach | a 120° arc, 1.9 m | physical |
  | Bowman | a long-range shot | 13.1 m | physical |
  | Magician | a ranged spell | 11.3 m | magic |

- **A class also sets its base values:** max HP, attack, accuracy, evasion, defenses, and crit damage, and how any of them grows per level (Progression).
  - Each of the Beginner's is the lowest of the four classes', so no class gives less (the user's call of 2026-11-16).
  - The Warrior's max HP grows fastest of all classes (the user's call of 2026-11-16).
  - The Thief's accuracy, attack, and crit damage grow with its level, since its recommended points go to VIT rather than DEX.
- **A class doesn't decide which stats pay off:** every stat does the same thing for every class (Progression).

## Promotion

- **Where:** a promotion moves a character from their class to a next class in the tree, at that class's trainer, an NPC in a town or a hunting map (NPCs and quests).
- **Conditions:** each promotion lists its conditions, and all must be met:
  - a minimum level;
  - minimum primary stats, from the character's own points without gear;
  - proficiency in a named skill: a minimum number of skill points invested in it;
  - a finished quest.
- **The trainer** shows every condition, met or not, and offers the promotion only when all are met.
- **Choice:** the player may pick any class whose conditions they meet. If several are open, they choose.
- **Starting conditions** for the first classes (proposal, still being decided; like evolutions, they live in game data so they can change without code):

  | Class | Level | Stat | Skill proficiency | Quest |
  | --- | --- | --- | --- | --- |
  | Warrior | 10 | STR 30 | Strike, 10 points | The Warrior's Trial |
  | Thief | 10 | AGI 30 | Strike, 10 points | The Thief's Trial |
  | Bowman | 10 | DEX 30 | Throw, 10 points | The Bowman's Trial |
  | Magician | 10 | INT 30 | Spark, 10 points | The Magician's Trial |

- **A promotion is permanent.**
- **Staying a Beginner is allowed.** A player can stay a Beginner as long as they like: the level, EXP, and Beginner skills keep growing, with no cap.
- **What a promotion changes:**
  - The basic attack and the base values switch to the new class's. Max HP becomes the new class's value for the current level. A promotion never lowers a base value or its growth, as the Beginner's are the lowest.
  - The new class's skills become available to learn.
- **What stays:** Beginner skills, with every point invested, and the stat points, skill points, level, EXP, items, and party.
- **Auto-assign** for stat points works only once a character has a class. A Beginner spends their own points (C6).

## Skills

- **Active or passive:**
  - **Active skills** are used with a key on the skill bar. Each costs energy (Combat → Energy), has a cooldown, and deals physical or magic damage, heals, or gives a short buff.
  - **Passive skills** are always on, such as faster regen.
- **Shapes:** an attack or a skill hits what its shape covers, from where the user stands toward their aim (Combat): an arc in front for melee, a lane for a shot, or a circle round the user for an area skill. Each one's shape and size live in game data, and an evolution can widen or lengthen it, such as Strike's 2.4 m arc growing to 2.9 m as Heavy Strike.
- **Combat rules:** an active skill hits by the same rules as an attack (Combat): accuracy against evasion, and the matching attack against the matching defense. Cooldown speed shortens skill cooldowns (Progression).
- **Actions:** using an active skill is an action, like a basic attack: one at a time, with its own length (Combat → Actions). Movement stays free while using one, and another skill waits for its end.
  - Any skill can cut off a basic attack.
  - A basic attack can cut off only the skills marked cancelable in game data.
- **Not in towns:** damaging skills can't be used there, as with attacks.
- **Skill points:**
  - Each level-up gives 5 skill points, separate from stat points. Points not spent are kept.
  - Points go into any skill the character's class offers, or that an earlier class offered. Beginner skills are always available.
  - A skill may take its first point only from a character level its game data gives (C17). Below it, the skill window shows that level over the skill's icon, with no add button, and the server refuses the points.
  - Points are staged and applied as stat points are, with Apply and Undo (proposal).
  - Each point makes the skill stronger: more damage, a stronger effect, or a longer duration, by a set amount per point.
- **Evolution:** at set totals of points, a skill evolves into a new skill, with a new name and a stronger or different effect. It keeps every point.
  - **The totals:** the Beginner's first four skills evolve at 10 points, and their actives again at 25. Every other skill evolves once, at 10 (C17). Slash Blast, Double Stab, Power Shot and Fire Arrow gain a status as they do (C19).
  - **Kept in game data:** these live in game data, not in code, so they can change at any time without a code change.
- **Traits:** every active skill that takes points has two traits, its default and an extra one, and uses one at a time (C20).
  - The default is the skill as it is. The extra changes how it behaves, such as a status it inflicts, its shape, or who a buff reaches, and pays for it, often with a longer cooldown.
  - The extra opens once the skill reaches its last evolution, and changes it there. A skill with no evolution opens it with its first point.
  - The player picks it in the skill window, free and at once, but not in combat: until 5 seconds after their last hit, miss, dodge, or damage taken.
  - For now the last evolution is all an extra needs. Later, extra traits also come from items or quests.
  - Break-free, block and passive skills have none.
- **Proficiency** in a skill is the number of points invested in it. Promotions can require it.
- **No cap:** Beginner skills take points without limit. Class skills have no cap either (proposal); their evolution totals give them their shape.
- **Block skills** (C21): every class has one, which comes with it and takes no points: Guard for the Beginner, Bastion, Parry, Deflect, and Ward for the Warrior, Thief, Bowman, and Magician. It guards the front for a moment, and a block turns its key into the class's counter.
- **Skill bar:** eight keys, Q W E R A S D F by default (B6), each holding an active skill (Screens and controls). A key holds a chain's root or a skill in no chain; while one of the root's follow-ups can go, the key offers it instead (C18).
- **Skill resets:** none for now (proposal).

## The Beginner

- **Starting stats, the same for everyone:** STR 7, DEX 7, INT 7, AGI 7, VIT 7 (proposal). These replace the per-class starting stats.
- **Starting gear:** a Wooden Club, a Beginner weapon, and 5 Red Potions (proposal).
- **Beginner weapons fit every class,** so a newly promoted character keeps fighting until they find their class's weapon (proposal).
- **Beginner skills.** The first four each lean toward a class, so early points can prepare a promotion. Lunge and Roll Back (C17) carry over to every class:

  | Skill | Kind | What it does | Evolves into |
  | --- | --- | --- | --- |
  | Strike | active, physical | a strong melee hit | Heavy Strike at 10 points, Crushing Blow at 25 |
  | Throw | active, physical | throws a stone at range | Sling at 10, Volley at 25 |
  | Spark | active, magic | a short-range spell | Bolt at 10, Arc at 25 |
  | Second Wind | passive | faster HP regen | Deep Breath at 10 |
  | Lunge | active, physical, from level 3 | lunges up to 4 m at the locked mob, then swings | Long Lunge at 10 |
  | Roll Back | active, from level 5 | rolls 4 m back, dodging for 0.4 s | Tumble at 10 |

## First classes

- Each first class has its own skill set, learned after promotion, with its own evolutions: its first two skills, then the five of [C17. A fuller skill roster](../features/combat-and-progression/c17-skill-roster.md), from level 10 to 20.
- Each first class also has its own weapons, its auto-assign split (Progression), and its trainer and trial quest (NPCs and quests).

## Later

- Second promotions: classes a first class can be promoted to.
- More first classes.
