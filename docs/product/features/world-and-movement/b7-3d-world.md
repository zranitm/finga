# B7. 3D world

- **Feature:** B7. 3D world
- **Group:** B. World and movement
- **Priority:** P0
- **Status:** Agreed

**Story:** As a player, I fight in a 3D world seen from above at an angle, with a stylized, animated look. Combat reads at a glance and hits feel heavy. Everything I know stays the same: my stats, my skills, and chaining attacks and skills with cancels.

## Acceptance

- **What stays:**
  - the stat system and progression;
  - the combat style: actions, cancels, energy, 1 s skills that leave movement free, hit-stop, and damage numbers;
  - the skill system;
  - items, quests, party, chat, and the HUD and windows.
- **The camera:**
  - It looks down on the character from above and behind at about 50°, as in Lost Ark or Diablo, and follows them in the middle of the screen.
  - It never turns, and the mouse doesn't move it.
  - Trees, walls, rocks, bushes, and anything else taller than half a player that stands between the camera and the player fade out so the player stays visible. They ease out and back in within a fifth of a second and keep their shadows. A building or tree right at the camera is hidden.
  - Where a bank or cliff stands between the camera and the player, it opens round them, so the player, the monsters at their side and their attack markings stay visible. It opens round the monsters fighting them too: the target, those attacking and those nearby. The opening's edge is dithered, and nothing opens on open ground.
- **Controls:**
  - They stay keyboard only, in the same two presets (B6).
  - The four direction keys walk up, down, left, and right on screen. Two together walk diagonally at the same speed.
  - The character faces the way they walk.
  - Jump stays.
  - The talk key also enters a portal the player stands in, since up now walks.
- **Aiming locks onto a mob** (C15, which replaced the first auto-aim):
  - The nearest mob on screen is locked, and attacks and skills go to it. With nothing locked, they go the way the player faces.
  - Tab switches to the next closest, a gold ring and outline mark the lock, and a target window shows its name, level, and HP.
- **Moving:**
  - Players walk anywhere on open ground: over gentle hills, and around trees, rocks, walls, and ruins.
  - They can jump onto low rocks and ledges.
  - There are no ropes or one-way platforms.
  - Mobs still block players.
  - Using a skill doesn't stop movement, and another skill waits for its end (C11).
- **Attacks in 3D:**
  - Melee attacks hit an arc in front of the player.
  - Ranged attacks fly as shots toward the aim.
  - Area skills hit all around, as before.
  - Reach stays what it was, turned into metres at 32 px to a metre: a Bowman's 420 px shot reaches 13.1 m.
- **Mobs:**
  - Mobs wander around their spawn area.
  - They chase a player around obstacles, and walk home if led too far from it.
  - Each attack shows a red area on the ground through its wind-up:
    - a cone for a bite;
    - the landing circle for a hop;
    - a lane for a charge;
    - the path of a spit;
    - a circle for a slam.
  - The area is set when the wind-up starts, so a player who steps out of it in time isn't hit.
- **The look:**
  - Characters, mobs, and places are low-poly 3D, toon-shaded, with dark outlines.
  - Swings leave trails, hits throw sparks, and skills glow.
  - Hit-stop, damage numbers, and the cancel afterimage carry over (C13).
- **Art:**
  - All of it comes from free CC0 packs, so nothing needs to be bought or credited:
    - KayKit characters and their animations for the classes;
    - Quaternius and KayKit monsters;
    - KayKit, Quaternius, and Kenney kits for places.
  - Some mob types are renamed or reskinned to fit the models.
- **Rollout:**
  - The 3D game was built on its own branch. It replaced the 2D game there, and the 2D client was removed.
  - First comes a playable slice: town and forest designed in 3D, every class, and the forest's mobs. The other maps come along as plain placeholders, so quests, levels, and bosses keep working.
  - As soon as the slice is complete and passes its playtest, it replaces the 2D game without waiting for the user, and the other maps are designed in 3D one by one.
  - As built, the slice is complete: town and forest are designed in 3D, and the other 12 maps are flat placeholders.
  - As built since, the other 12 maps are designed in 3D too, one by one, the fallen keep last.
- **Fairness:** the server still moves every player and decides every hit. The player's own screen moves and acts at once, as today.

## Not in this feature

- Turning the camera, zooming, or aiming with the mouse.
- A dash or a dodge. Whether 3D needs one is judged after the slice.
- Climbing, swimming, or gliding.
- Sounds.
- Bought or commissioned art.

## Rules

World, Screens and controls, and Systems → Combat, which describe the 3D game.
