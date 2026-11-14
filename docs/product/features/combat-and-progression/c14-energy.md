# C14. Energy

- **Feature:** C14. Energy
- **Group:** C. Combat and progression
- **Priority:** P1
- **Status:** Draft

**Story:** As a player, I build energy by landing basic attacks and spend it on skills. A fight becomes a rhythm of attacks and skills chained with cancels, not a pool I refill with potions. Grinding and that combat are the game's main experience.

## Acceptance

- **Energy replaces MP.**
  - Every character has energy from 0 to 10, shown as 10 pips under the HP bar.
  - Nothing raises the 10.
- **Basic attacks fill it:**
  - Each basic attack that reaches a mob gives 2 energy, whether it hits or MISSes, however many mobs it reaches.
  - A swing that reaches nothing gives none, so a player can't charge up before a fight.
  - Skills give none.
- **Skills spend it:**

  | Energy | Skills |
  | --- | --- |
  | 2 | Strike, Throw, Double Stab, Power Shot: the quick single-target skills; and the Magician's Spark and Fire Arrow |
  | 3 | Slash Blast, Heal, and the buffs Iron Body, Haste, and Focus |

  - A skill's cost doesn't grow with its points or change when it evolves.
  - A skill without enough energy is refused with a short notice, as one without enough MP was, and nothing is spent.
  - Cooldowns stay as they are, to tune after a playtest.
  - A skill used alone still comes as often as its cooldown allows. Energy paces a mix of skills: a 2-energy skill can follow every attack, and a 3-energy one two attacks in three.
- **Keeping it:**
  - Energy stays between fights, across maps, and over logging out, so a player can open the next pack with a skill.
  - It drops to 0 on death. A new character starts at 0.
  - Level-ups and promotions don't change it.
- **No MP left:**
  - INT no longer gives max MP or MP regen. It keeps magic attack, magic defense, and cooldown speed.
  - Classes have no base MP.
  - The Runed Wand, Gold Pendant, and Starlight Pendant lose their max MP and MP regen. Their other stats stay as they are.
  - Second Wind and Deep Breath give HP regen only.
  - Blue Potions and Mana Elixirs are gone, and so is the MP potion key. Mobs and field bosses no longer drop them. The three quests that gave Blue Potions give as many Red Potions.
- **No attack speed:**
  - The attack speed stat is gone. A basic attack always takes its class's time (Systems → Combat → Pace).
  - The Thief's, Bowman's, and Magician's attacks are re-paced, so each fights as fast as it did with its attack speed. The Thief's keeps a 150 ms cancel window.
  - AGI keeps evasion and move speed.
  - Hunter's Gloves, Plate Gauntlets, Ranger's Gloves, and the Whisper Edge lose their attack speed. Their other stats stay as they are.
  - Haste gives move speed in place of attack speed, as much as it gave before, with its evasion as before.
- **Fairness:** the server counts every gain and every cost. The player's own screen shows a spend the moment they press, and a gain as soon as the server counts it. A skill that needs the energy of the attack it cuts waits a moment for that count instead of being refused.

## Not in this feature

- Energy from taking hits, or from skills.
- Gear or skills that raise max energy or the energy a hit gives.
- A new use for the freed MP potion key.
- Retuning cooldowns or skill powers, which follows a playtest.

## Rules

Systems → Combat → Energy, Systems → Classes and skills, and Systems → Progression
