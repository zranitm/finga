# C20. Skill traits

- **Feature:** C20. Skill traits
- **Group:** C. Combat and progression
- **Priority:** P1
- **Status:** Agreed

**Story:** As a player, I shape each skill to how I fight. Every active skill has two traits, its default and an extra one, and I pick which one it uses. Strike can stun instead of coming back quickly, and War Cry can make me alone twice as strong instead of lifting the party. The skill window shows my skills as a grid of icons, with the details on a card when I hover over one.

## Acceptance

- **Traits:**
  - Every active skill that takes points has two traits: its default, which is the skill as it is today, and one extra.
  - Only one is chosen at a time. A skill starts on its default.
  - The extra trait changes what the skill does: a status it inflicts, its shape, its targets, its move, who a buff reaches, its cooldown. Each one trades something, so neither trait is simply better.
  - Break-free skills and passive skills have no traits.
- **Evolutions stay.** A skill still evolves with its points, and the chosen trait applies at every stage. Evolutions decide how strong a skill is; traits decide how it behaves. A trait may grow at an evolution too, such as Fury lasting longer once War Cry becomes Battle Roar.
- **Choosing:**
  - The player picks a skill's trait in the skill window. It changes at once, with no cost and no Apply.
  - A skill needs at least one point before its trait can change.
  - **Not in combat:** a trait can't change during a fight. A player is in combat until 5 seconds after they last hit, missed, dodged, or took damage from a mob, damage over time included. Towns are never combat.
  - A cooldown or a buff already running keeps going as it was. The new trait counts from the skill's next use.
  - **For now every trait is open to everyone.** Later, extra traits come from items or quests.
- **The first traits:**

  | Class | Skill | Default | Extra |
  | --- | --- | --- | --- |
  | Beginner | Strike | Swift: ready again in 4.5 s | Concussion: stuns for 1 s; cooldown 7 s |
  | Beginner | Throw | Stone: ready again in 4.5 s | Bola: slows for 3 s; cooldown 6 s |
  | Beginner | Spark | Crackle: ready again in 5.6 s | Static: weakens for 4 s; cooldown 7 s |
  | Beginner | Lunge | Quick Lunge: ready again in 6 s | Slip In: dodges through the dash and swing; cooldown 8 s |
  | Beginner | Roll Back | Retreat: rolls away from the locked mob | Dive: rolls the way the player walks |
  | Warrior | Slash Blast | Sweep: 180° in front, up to 3 | Whirlwind: a full circle of 3 m, up to 5; cooldown 9 s |
  | Warrior | Iron Body | Steadfast: the Warrior alone, 30 s | Iron Wall: the party within 6 m too, 15 s |
  | Warrior | Charge | Bull Rush: stuns the first mob it meets | Trample: knocks down up to 3 where it ends |
  | Warrior | Ground Slam | Shockwave: all round the Warrior | Fissure: along a 7 m line ahead |
  | Warrior | War Cry | Rally: the party within 8 m, 30 s | Fury: the Warrior alone, twice the attack, 15 s |
  | Warrior | Taunt | Roar: every mob within 6 m | Call Out: the mobs in a 12 m line ahead, for 8 s |
  | Thief | Double Stab | Twin Stab: ready again in 3.4 s, bleeds once evolved | Puncture: breaks armour for 4 s instead; cooldown 5 s |
  | Thief | Haste | Fleet: faster feet, a little evasion | Blur: no faster feet, three times the evasion |
  | Thief | Shadow Step | Ambush: ready again in 10 s | Sap: puts the mob to sleep for 4 s; cooldown 14 s |
  | Thief | Smoke Bomb | Smoke: mobs lose sight of the Thief | Flash Powder: blinds them for 4 s instead |
  | Thief | Flurry | Five Stabs: five stabs in front | Fan of Knives: three waves all round, up to 4; cooldown 10 s |
  | Thief | Poison Blade | Venom: every hit poisons | Numbing Edge: every hit weakens instead |
  | Bowman | Power Shot | Heavy Shot: ready again in 4.5 s | Piercing Shot: through up to 3 in its lane; cooldown 6 s |
  | Bowman | Focus | Precision: accuracy and crit rate | Expose: accuracy, and every hit breaks armour |
  | Bowman | Disengage | Kick Off: ready again in 12 s | Net: also pins the mob it knocks back for 2 s; cooldown 16 s |
  | Bowman | Pinning Shot | Pin: pins its mob in place | Barbed Shot: bleeds for 5 s instead |
  | Bowman | Arrow Rain | Downpour: ready again in 12 s | Thorn Rain: each wave slows for 2 s; cooldown 15 s |
  | Bowman | Eagle Eye | Lone Hunter: the Bowman alone, 30 s | Spotter: the party within 8 m too, 20 s |
  | Magician | Fire Arrow | Flame: burns once evolved | Ice Arrow: slows for 3 s instead; cooldown 6 s |
  | Magician | Heal | Mend: the caster and up to two others | Greater Heal: one, 70% more; cooldown 6 s |
  | Magician | Blink | Stride: the way the Magician walks | Recede: straight away from the locked mob, a longer dodge |
  | Magician | Frost Nova | Nova: round the Magician | Ice Prison: a 3 m circle round the locked mob, up to 10 m off |
  | Magician | Barrier | Bulwark: a full shield on the Magician | Sanctuary: the party within 6 m too, each at 60% |
  | Magician | Meteor | Impact: knocks down | Firestorm: burns for 3 s instead, which elites and bosses take |

  - Some extras feed chains (C18) and some leave them: Concussion sets up the Warrior's Ground Slam, and Net sets up Arrow Rain. Trample, Firestorm and Ice Arrow give up the hard control their chains wait for.
- **The skill window (K):**
  - **A grid of icons,** one section per class, Beginner first, each skill in the order its level opens it. Under each icon, its name at its stage.
  - **On the icon:**
    - its points, with any staged in green;
    - **+** while there are points to spend;
    - "Lv N" while the skill is locked by level;
    - a chain mark on a follow-up that can't go on a key of its own (C18).
  - **Under the icon,** two small buttons for its two traits, the chosen one lit in the skill's colour. Hovering one shows that trait's card; clicking it chooses it.
  - **Details only on hover:** hovering a skill shows its card beside the pointer, as items' cards show:
    - its name, kind and points;
    - what it does now, and with any staged points;
    - its cost, cooldown and length;
    - what each point adds;
    - its next evolution;
    - its chains;
    - both traits, with the chosen one marked.
  - A trait's card shows what the skill does with that trait, in numbers, so the two can be compared.
  - **In combat,** the trait buttons are dimmed, and the window says traits change once out of combat.
  - Points are still staged with **+** and applied with Apply or Undo. Active skills are still dragged onto the skill bar by their icons.
- **The skill bar:** a key whose skill uses its extra trait shows a small mark in the skill's colour, and its tooltip names the trait.
- **Fairness:** the server keeps each skill's trait, refuses a change in combat, and uses the trait when the skill is used. Other players see the skill as its trait makes it.

## Not in this feature

- Unlocking extra traits through items or quests: every trait is open for now.
- More than two traits for a skill.
- Traits that add or change chain follow-ups.
- Traits for passive skills and break-free skills.
- An icon of its own for each trait.

## Rules

Systems → Classes and skills.
