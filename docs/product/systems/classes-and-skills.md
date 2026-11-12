# Classes and skills

## Classes

- **Everyone starts as a Beginner,** with no class. Character creation asks only for a name (A2).
- **Classes form a tree.** A Beginner can be promoted to one of the first classes: **Warrior**, **Thief**, **Bowman**, or **Magician**. More classes come later as further branches: other first classes, or classes a promoted character can be promoted to again. Four isn't the final set.
- **A class decides how the character fights and which skills they can learn:**

  | Class | Basic attack | Damage |
  | --- | --- | --- |
  | Beginner | a short melee swing | physical |
  | Warrior | melee | physical |
  | Thief | fast, short reach | physical |
  | Bowman | a long-range shot | physical |
  | Magician | a ranged spell | magic |

- **A class also sets its base values:** max HP and MP and how they grow per level, and base accuracy, evasion, and defenses (Progression). The Beginner's sit in the middle of the four classes' (proposal).
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
  - The basic attack and the base values switch to the new class's. Max HP and MP become the new class's values for the current level.
  - The new class's skills become available to learn.
- **What stays:** Beginner skills, with every point invested, and the stat points, skill points, level, EXP, items, and party.
- **Auto-assign** for stat points works only once a character has a class. A Beginner spends their own points (C6).

## Skills

- **Active or passive:**
  - **Active skills** are used with a key on the skill bar. Each costs MP, has a cooldown, and deals physical or magic damage, heals, or gives a short buff.
  - **Passive skills** are always on, such as faster regen.
- **Combat rules:** an active skill hits by the same rules as an attack (Combat): accuracy against evasion, and the matching attack against the matching defense. Cooldown speed shortens skill cooldowns; attack speed doesn't change skills (Progression).
- **Actions:** using an active skill is an action, like a basic attack: one at a time, with its own length (Combat → Actions).
  - Any skill can cut off a basic attack.
  - A basic attack can cut off only the skills marked cancelable in game data.
- **Not in towns:** damaging skills can't be used there, as with attacks.
- **Skill points:**
  - Each level-up gives 5 skill points, separate from stat points. Points not spent are kept.
  - Points go into any skill the character's class offers, or that an earlier class offered. Beginner skills are always available.
  - Points are staged and applied as stat points are, with Apply and Undo (proposal).
  - Each point makes the skill stronger: more damage, a stronger effect, or a longer duration, by a set amount per point.
- **Evolution:** at set totals of points, a skill evolves into a new skill, with a new name and a stronger or different effect. It keeps every point.
  - **Still to be decided:** the totals, what each skill becomes, and how many evolutions a skill has.
  - **Kept in game data:** these live in game data, not in code, so they can change at any time without a code change.
  - The examples on this page (Strike becomes Heavy Strike, then Crushing Blow) are placeholders.
- **Proficiency** in a skill is the number of points invested in it. Promotions can require it.
- **No cap:** Beginner skills take points without limit. Class skills have no cap either (proposal); their evolution totals give them their shape.
- **Skill bar:** six keys, 1 to 6, each holding an active skill (Screens and controls).
- **Skill resets:** none for now (proposal).

## The Beginner

- **Starting stats, the same for everyone:** STR 7, DEX 7, INT 7, AGI 7, VIT 7 (proposal). These replace the per-class starting stats.
- **Starting gear:** a Wooden Club, a Beginner weapon, and 5 Red Potions (proposal).
- **Beginner weapons fit every class,** so a newly promoted character keeps fighting until they find their class's weapon (proposal).
- **Beginner skills** (proposal; the evolutions are placeholders, see Evolution). Each one leans toward a class, so early points can prepare a promotion:

  | Skill | Kind | What it does | Evolves into |
  | --- | --- | --- | --- |
  | Strike | active, physical | a strong melee hit | Heavy Strike at 10 points, Crushing Blow at 25 |
  | Throw | active, physical | throws a stone at range | Sling at 10, Volley at 25 |
  | Spark | active, magic | a short-range spell | Bolt at 10, Arc at 25 |
  | Second Wind | passive | faster HP and MP regen | Deep Breath at 10 |

## First classes

- Each first class has its own skill set, learned after promotion, with its own evolutions. The sets are content, defined per class with the class's arrival. Warrior and Bowman come first; Thief and Magician are added now too.
- Each first class also has its own weapons, its auto-assign split (Progression), and its trainer and trial quest (NPCs and quests).

## Later

- Second promotions: classes a first class can be promoted to.
- More first classes.
