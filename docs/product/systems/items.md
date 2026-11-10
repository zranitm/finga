# Items

## Item types

These are also the inventory tabs.

- **Equipment:** weapons, armor, and accessories. Each piece is one of a kind and is worn in a slot to raise stats. It has stars and rolled stats, and may need a minimum level, minimum stats, or a class.
- **Use:** consumables such as potions. Stackable.
- **Etc:** materials and mob drops with no direct use, sold to shops or other players. Stackable.

## Equipment slots

Ten slots (proposal):

- **Weapon:** one, for the character's class.
- **Armor:** Hat, Top, Bottom, Gloves, Shoes.
- **Accessories:** Necklace, Earrings, and two Rings.

Every item type names its slot. A ring fits either ring slot.

## Requirements

- A minimum level and minimum primary stats, for example 25 STR (OD-19: decided, stat requirements too).
- Weapons also name a class, since the class decides the attack's shape. Armor and accessories use stat requirements instead of a class (proposal).
- An item the character doesn't meet can be kept, traded, and compared, but not worn.

## Rarity: stars

- Every item has stars, from 1★ to 7★ (OD-14: decided, rarity tiers by stars).
- Stars belong to the item type: every Iron Sword has the same stars. Only the stats roll (below).
- More stars means rarer and stronger: higher stat ranges and lower drop chances.
- Each star count has its own colour, used for the item's name and the frame of its icon wherever it shows (proposal: 1★ grey, 2★ green, 3★ blue, 4★ purple, 5★ orange, 6★ red, 7★ gold).
- Use and Etc items have stars too, as a rarity label only (proposal).

## Equipment stats

- An item type lists which stats it gives and a range for each, for example Physical attack 16–20.
- Equipment can give any of the five primary stats and any of the sixteen derived stats (Progression).
- Each copy rolls every one of its stats within its range, once, when it drops. The roll never changes after that, so two copies of the same item can differ.
- Starter items have the lowest value of each range, so every new character starts the same (proposal).
- The stats of all worn items add up and apply right away, in combat and in the character window alike. They add on top of what points give, and a stat's limit is the ceiling for the total (Progression).

## Drops

- Each mob type has a drop list: which items and currency it can drop, and the chance of each.
- Drops land on the ground and are collected automatically; there is no pickup key. After a short delay (1 second to start, tunable in game data) a drop flies to its owner and goes into their inventory, wherever they are on the map.
- Every drop belongs to one player from the moment it falls (OD-05: decided, replacing the old default of "the killer or their party first, then anyone"):
  - hunting alone, the killer;
  - in a party, the members on the map take turns (Social → Party).
- Nobody can take a drop that isn't theirs. A drop that can't be collected vanishes after 60 seconds.

## Inventory

- Each tab has a fixed number of slots. Stacks have a maximum size.
- With a full inventory, a drop waits on the ground for its owner and is collected within a few seconds of a slot freeing up. The owner is told what is waiting. A drop still waiting after 60 seconds vanishes.
- Items can be discarded. A worn item must be unequipped first. Items can no longer be dropped on the ground for someone else, since nobody could pick them up; giving items is trade (F3, F4).

## Integrity

- Every piece of equipment can be told apart from an identical-looking one, and its rolled stats are part of it.
- Items are never duplicated or lost by mistake, even if the game disconnects mid-action, equipping and unequipping included.
- Every item's history can be traced: where it came from, what it rolled, and who has held it.

## Content (proposal)

- At least one item for every armor and accessory slot in each of the three level bands (1–10, 10–20, 20–30), and a weapon for each class in each band.
- The early bands drop mostly 1★ to 3★ items; 6★ and 7★ are very rare finds.
