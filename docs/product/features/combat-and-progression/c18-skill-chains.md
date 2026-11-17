# C18. Skill chains

- **Feature:** C18. Skill chains
- **Group:** C. Combat and progression
- **Priority:** P1
- **Status:** Agreed

**Story:** As a player, I don't need a key for every skill. A key turns into the next skill when the moment calls for it, as in Blade & Soul. So my six keys carry a whole kit, and fights flow from one skill into the next.

## Acceptance

- **A chain** is a root skill and its follow-ups, sharing the root's key. While a follow-up's condition holds, the key shows and uses the follow-up instead. When the condition ends, or the follow-up is used, the key goes back.
- **Conditions:**
  - **Right after a skill:** for a short time after the chain's root lands, a combo. Double Stab, then Flurry, for example.
  - **Target controlled:** while the locked mob is stunned, frozen, pinned, or knocked down, a finisher. Fire Arrow becomes Meteor on a frozen mob, for example.
  - **Target far:** while the locked mob is out of the root's reach, the approach. Strike becomes Lunge, for example.
  - **Right after a dodge:** for a short time after the player dodges an attack, a counter.
  - **While the player is held:** while they're stunned, frozen, asleep, knocked down, or pinned, their escape key becomes their class's break-free skill (C19).
- **The chains are set by the game,** for each class, and not by players. A follow-up comes up only once it has a point, and only when it's off cooldown. Otherwise the key stays its root.
- **When two follow-ups could take over,** the one the class lists first wins.
- **The skill bar:**
  - Players drag a chain's root onto any of the six keys, as now. Its follow-ups ride along.
  - A follow-up can't be put on a key of its own.
  - A key whose follow-up is live shows that skill's icon with a glowing frame, so the switch is seen.
  - Each skill keeps its own cooldown and energy, wherever it shows.
- **The skill window** lists each class's chains: the root, then each follow-up with its condition, such as "on Strike's key, while the target is far".
- **The first chains:**

  | Class | Root | Follow-ups, first listed wins |
  | --- | --- | --- |
  | Beginner | Strike | Lunge, while the target is far |
  | Beginner | Roll Back | Break Free, while the player is held; Lunge, right after a dodge |
  | Warrior | Strike | Ground Slam, while the target is stunned; Charge, while it's far |
  | Warrior | Roll Back | Unyielding, while the player is held; Lunge, right after a dodge |
  | Warrior | War Cry | Iron Body, right after War Cry |
  | Thief | Double Stab | Flurry, right after Double Stab; Shadow Step, while the target is far |
  | Thief | Smoke Bomb | Vanish, while the player is held; Shadow Step, right after a dodge |
  | Thief | Poison Blade | Haste, right after Poison Blade |
  | Bowman | Power Shot | Arrow Rain, while the target is controlled |
  | Bowman | Disengage | Recoil, while the player is held; Power Shot, right after a dodge |
  | Bowman | Focus | Eagle Eye, right after Focus |
  | Magician | Fire Arrow | Meteor, while the target is controlled |
  | Magician | Frost Nova | Meteor, while the target is frozen |
  | Magician | Blink | Arcane Release, while the player is held; Fire Arrow, right after a dodge |
  | Magician | Heal | Barrier, right after Heal |

  - Skills in no chain, such as Slash Blast, Taunt, Pinning Shot, Throw, and Spark, stand on their own key, as today.
  - The Beginner's Roll Back was to give Spark, but Spark is the Magician's promotion skill, and a follow-up can't sit on a key of its own. So Lunge took its place, keeping every promotion skill on a key.
  - A follow-up may sit in more than one chain, and keeps one cooldown.
- **Fairness:** the server checks a follow-up's condition when it's used, with a little slack for lag. A modified client can't use a finisher on a mob that isn't controlled.

## Not in this feature

- Players building their own chains.
- Fixed skill bars per class: the keys stay the player's to arrange.
- Follow-ups made only for chains: the first chains reuse C17's skills.

## Rules

Systems → Classes and skills.
