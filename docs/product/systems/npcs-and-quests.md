# NPCs and quests

## NPCs

- **What they are:** characters placed on maps, in towns and in hunting maps. They don't fight and can't be attacked, and everyone sees the same NPCs in the same places.
- **What they offer:** each NPC has a name and a look, and one or more services:
  - promotion, as a class trainer (C8);
  - quests, to give and to take back;
  - later, a shop (F2) or the marketplace (F3).
- **Talking:**
  - Stand within reach and press Talk: Z in the Classic preset, E in WASD (proposal).
  - A dialog opens with what the NPC offers.
  - Walking out of reach, dying, or changing maps closes it, and Talk does nothing while dead.
- **Markers:** an NPC shows a marker above its head when it has something for this player: a quest to give, a quest ready to hand in, or a promotion whose conditions are all met (proposal).
- **NPCs stand still** (proposal).
- **First placement** (proposal):

  | NPC | Where | Offers |
  | --- | --- | --- |
  | Warrior trainer | town | promotion; The Warrior's Trial |
  | Magician trainer | town | promotion; The Magician's Trial |
  | Bowman trainer | forest | promotion; The Bowman's Trial |
  | Thief trainer | cave | promotion; The Thief's Trial |
  | Town guide | town | starter quests for Beginners |

## Task quests

There is no story: a quest is a task with a reward, and narrative quests stay a non-goal.

- **Objectives:** one or more per quest:
  - defeat a number of a mob type;
  - collect a number of an item;
  - talk to an NPC;
  - reach a level.
- **Accepting:** from the NPC who gives it, when the character meets its conditions (level, class, quests finished before). A quest can be active only once at a time.
- **Progress:**
  - It is counted as it happens, shown in the quest log, and announced in the combat channel ("Green Slime 12/30").
  - It is kept across maps, deaths, and logouts.
  - Kills count for every character who gets EXP from the kill: the killer, and party members on the map who share it (proposal).
  - Items count while they're in the inventory, and handing in the quest takes them (proposal).
- **Handing in:**
  - At the NPC the quest names, usually the one who gave it, once every objective is done.
  - The rewards arrive at once: EXP, items, currency once F1 exists, and anything the quest unlocks, such as a promotion condition or a next quest.
  - If there is no room for an item reward, the hand-in is refused, with nothing taken, until there is (the user's call of 2026-11-12):
    - The player is told how many slots to free in each tab: "Free 2 slots in your Equip tab and 1 in your Use tab to take this reward."
    - The count lets the quest's items leave first, but one frees a slot only if its whole stack goes. A reward that stacks fills the player's stacks of it first.
    - Drops waiting for room in a tab the reward needs count too, as each takes a slot freed there before the reward can (Items → Inventory). One in any other tab doesn't.
  - A trainer's weapon at a promotion follows the same rule, and the trainer says it before asking to confirm: "Free 1 slot in your Equip tab to take your weapon."
- **One-time or repeatable:** each quest says which. Promotion trials are one-time.
- **Abandoning:** allowed at any time from the quest log. Progress is lost, and the quest can be taken again.
- **Limit:** at most 20 active quests (proposal).
- **Finished quests are remembered,** since promotions and later quests can require them.
- **Integrity:** items handed in and item rewards follow the item rules. They are never lost or duplicated, and each item's history names the quest (D5).
- **Trials,** one per first class (proposal):

  | Quest | Given by | Task |
  | --- | --- | --- |
  | The Warrior's Trial | Warrior trainer | defeat 20 Wild Boars |
  | The Thief's Trial | Thief trainer | bring 10 Bat Wings |
  | The Bowman's Trial | Bowman trainer | defeat 30 Orange Mushrooms |
  | The Magician's Trial | Magician trainer | bring 15 Slime Gel and talk to the town guide |

## Later

- Repeatable daily quests.
- Shops at NPCs (F2), and the marketplace NPC (F3).
