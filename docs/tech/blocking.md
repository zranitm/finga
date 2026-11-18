# Blocking

> **Status: approved · 2026-11-18.** The high-level design for [C21. Blocking](../product/features/combat-and-progression/c21-blocking.md). It builds on [A fuller skill roster](./skill-roster.md), whose dodge a block sits beside, [Skill chains](./skill-chains.md), whose conditions gain one, and the telegraphs of [The 3D world](./3d-world.md) section 7.

## 1. What has to hold

- **The user's words** (2026-11-18): "mob atk wont show the red line, only bosses's does - there are 2 types of line for player to predict - red line: cannot be blocked, only dodge works - yellow line: can be blocked - add atk blocking skill for classes - mob atk can be blocked".
- **Their choices:**
  - a block gives energy and turns its key into a counter, and costs no energy itself;
  - it covers the front half;
  - the Beginner has a block, and each first class its own;
  - elites show no area, like normal mobs.
- **Discovery over cues:** a normal mob's attack is read from its body, not the ground. Only a boss, whose blows matter most, marks them, and says which can be blocked.
- **The server decides,** as for a dodge: whether an attack is blocked, from which side, and what it gives. The client draws the guard and waits for the world's word.
- **Data, not code:** whether an attack can be blocked is a field on the attack, and a block is a field on a skill, so later bosses and later classes add them as data.
- **No new keys:** a block is a skill on the bar, and a counter is a chain follow-up on its key.

## 2. Warning areas

- **`mobs.yaml`:** an attack may say `unblockable: true`. The table's `slam` does, so every field boss's slam is red. A boss's family attack stays blockable, so it's yellow.
- **Validation** refuses an unblockable attack on a mob that isn't a boss. Only a boss's attacks show an area, and an unmarked blow that a guard can't stop would be unfair.
- **The generator** carries `unblockable` with each attack, beside the mob's `boss`.
- **The client** (`game/effects`):
  - **A normal mob or an elite draws no area.** What it draws besides stays:
    - the wind-up clip;
    - a hop or a charge carried on the player's clock;
    - the spit's ball;
    - the impacts where the hit lands.
  - **A boss draws its area as today,** filling on the player's clock, in a colour by the attack:
    - **red** for an unblockable one;
    - **yellow** for the rest.
    - Both are light and glowing, as Blade & Soul's are, with a bright rim in their own colour (as built: lighter warning colours, below).
  - A status still edges a boss's area, just outside its rim (statuses.md).
  - The see-through ghosts of hidden telegraphs (3d-world.md section 7) follow: only bosses' areas have them.
- **`MobAttacked`** is unchanged. The client looks up whether the mob is a boss and whether the attack is blockable in its data.

## 3. Block skills

### Data

- **A new field, `block: <time>`,** on an active skill: from the action's start, for this long, the user blocks blockable mob attacks that come from the front.
- **A new effect, `block`:** its block, and optionally a `move` (Deflect's leap back, once the guard is down). No shape, power or damage.
- **`actions.yaml`:**
  - `block: {arc: 180}`, the degrees round the user a block covers, centred on its aim;
  - `per_block: 2` in `energy`, what the first attack a use blocks gives.
- **Validation:**
  - `block` only on a `block` skill, and every `block` skill has one;
  - the time is above 0 and at most the skill's length;
  - a block skill is granted, has one stage, has no shape, costs no energy, and isn't `cancelable`. The energy rule becomes "only a break-free or a block skill costs none", and the shape check skips a block skill as it skips a move skill.
  - `arc` is above 0 and at most 360, and `per_block` is 0 to `max`.
- **The five skills,** each granted with its class in `classes.yaml`, as the break-free skills are:

  | Skill | Class | Level | `block` | Length | Cooldown | Also |
  | --- | --- | --- | --- | --- | --- | --- |
  | Guard | Beginner | 1 | 600ms | 600ms | 6s | |
  | Bastion | Warrior | 10 | 600ms | 600ms | 2s | |
  | Parry | Thief | 10 | 400ms | 400ms | 4s | |
  | Deflect | Bowman | 10 | 600ms | 900ms | 6s | `move: {kind: leap, distance: 2, time: 300ms, toward: back, delay: 600ms}` |
  | Ward | Magician | 10 | 800ms | 800ms | 7s | |

  - Each has a game-icons.net icon (C16), picked in the build, with its row in `LICENSES.md`.
  - Being granted, a block is its own class's alone (`Class.Learnable`). A Warrior has Bastion, not Guard.
  - Granted skills have no traits ([Skill traits](./skill-traits.md) section 2), so blocks have none.

### In the world

- **When a block starts,** in `cast`, the player keeps:
  - the block's window, from the action's start tick to its end, beside the dodge's;
  - its aim, as a way in x and z, as the world turns any skill's aim into one;
  - whether this use has blocked yet.
- **In `mobHits`,** after the dodge check, for each hit on a player:
  1. The attack is blockable: its data doesn't say `unblockable`.
  2. The player's block window holds this tick.
  3. It comes from the front:
     - The hit's `DX, DZ` is the way it knocks the player, so it comes from the other way.
     - It's from the front when the aim's dot product with (−DX, −DZ) is at least cos(arc ÷ 2), worked out once at load. That's 0 for 180°: no trig per hit.
  4. Then it's blocked:
     - a `Damage` with `Block: true`, as a dodge's has `Dodge`;
     - no damage, knockback, status, or hit guard;
     - the first blocked attack in a use gives `per_block` energy, up to `max`;
     - the player's last block input is noted for chains, as `dodgedSeq` is.
- **What still holds:**
  - **A dodge comes first:** a hit landing in both passes through as a dodge.
  - A rush stops at a blocking player and a shot ends on them, as they do at a guarded one.
  - A hard status ends the block with the action, as it ends any action (statuses.md).
  - Death ends it.
- **Bots** don't block.

### Counters

- **A new chain condition,** `blocked: <time>`: within `time` of the player's last block.
  - It's counted as `dodged` is (skill-chains.md section 4): from the input acked by the frame that carried the player's own BLOCK, with 150 ms of slack after.
  - Validation holds it to 0.2 to 5 s, as `dodged`.
- **The first counters,** in `classes.yaml`, each 1.5 s:

  | Class | Root | Follow-up |
  | --- | --- | --- |
  | Beginner | Guard | Strike |
  | Warrior | Bastion | Ground Slam |
  | Thief | Parry | Flurry |
  | Bowman | Deflect | Power Shot |
  | Magician | Ward | Frost Nova |

  - Strike, Power Shot and Frost Nova root chains of their own, so they still stand on their own keys. Ground Slam and Flurry are already follow-ups.
  - A block is a root, so it stands on a key.
- **The client** keeps the last block as it keeps the last dodge: the input the newest frame acked when the player's own BLOCK `Damage` arrived.

### Promotion

- **On `Promoted`,** a skill-bar key holding Guard takes the class's block, saved as any change to the bar is. The Beginner's chains give way to the class's, as today.

### In PvP matches

- A block stops enemy players' attacks and skills from the front as it stops mobs'. Nothing a player does is unblockable ([PvP](./pvp.md) section 5).

## 4. Protocol

| Message | Direction | Change |
| --- | --- | --- |
| `Damage` | server | Gains `block`: a mob's attack the player blocked, with no damage, knockback or guard |

- No new codes: a block is used as any skill is, with its refusals.

## 5. Client

- **The guard's look:**
  - an upper-body guard pose, on the upper-body mask that skills already use (3d-world.md);
  - a half ring in front of the player along the aim, in the class's colour, for the block's time.
  - Deflect's leap is predicted as Roll Back's is.
- **BLOCK** shows over the player where DODGE would, from `Damage.block`, with a spark on the ring's edge toward the blow. It waits for the world, as every result does.
- **The counter's key** glows as other follow-ups do (C18).
- **The skill window** shows the block's tile as granted, as break-free tiles are.
- **The warning areas,** section 2.

## 6. Balance

- **Bosses keep their threat:** their slam, the heaviest blow, can't be blocked. Their potion budget (field-bosses.md) is set for a player who never dodges or blocks, so blocking only helps.
- **Energy:** at most 2 per block use. Even Bastion's 2 s cooldown keeps it below what attacking gives, 2 for each basic attack that lands.
- **Numbers** are first guesses, in data, for the live pass to tune.

## 7. Testing

- **Data:**
  - `unblockable` refused on a mob that isn't a boss;
  - each block skill rule in section 3, and the energy rule's exception;
  - `blocked` validated as `dodged` is;
  - the generator carrying `unblockable`.
- **World:**
  - a bite, a hop, a charge and a spit blocked from the front, at the window's first and last tick;
  - one from behind landing;
  - a boss's slam landing through a block, and dodged by Roll Back;
  - no damage, knockback, status or guard on a block;
  - energy once per use, however many it blocks;
  - a dodge taking a hit that both would;
  - a stun ending a block;
  - `blocked`'s check at and just past its slack.
- **Client:**
  - no area for a normal mob or an elite, and its wind-up, carry, ball and impact still drawn;
  - yellow and red areas for a boss's two attacks, with the slam's status edge;
  - the guard's ring along the aim, and BLOCK;
  - the counter's key after a BLOCK;
  - a promotion swapping Guard's key for the class's block.
- **End to end:**
  - a Beginner's Guard blocks a slime's hop, BLOCK shows, energy rises by 2, and the key offers Strike;
  - a slime hopping from behind hits through it;
  - all judged by the world's ticks, not the wall clock.
- **Live:** each class blocks each mob family from the front and is hit from behind, and a field boss's yellow blow is blocked while its red slam is dodged.

## 8. Build

1. **Warning areas:** `unblockable` in data and the generator, and the client's areas: none for normal mobs and elites, yellow and red for bosses. Small, and it can go first.
2. **Blocks, server side:**
   - the `block` field and effect, `actions.yaml`'s numbers, and validation;
   - the five skills and their counters;
   - the world's block and energy;
   - `Damage.block` and `blocked`.
3. **Blocks, client side:** the guard's look, BLOCK, the counter's key, the promotion swap, and the tests.

### As built: PR 1, the warning areas

- **Data:** `unblockable` is on the table's `slam`, so every boss's slam carries it. A mob's use may say otherwise. The loader refuses it on a mob that isn't a boss; a test on the shipped data holds every boss's slam unblockable and no other attack.
- **The client:** `PlayOptions.area` is `'red'`, `'yellow'` or unset. `world3d/combat.ts` sets it for a boss alone, from the attack's `unblockable`. `effects/mob-looks.ts` lays the tint, the fill, their ghosts and the status edge only with it. The wind-up clip, a hop's or charge's carry, the spit's ball and its ghost, and the impacts are drawn as before.
- **The yellow,** picked in a headless render on each ground a boss stands on: grass, stone, sand, snow and brick. Both colours were later made lighter (as built: lighter warning colours, below).
  - The tint was `0xffd640`. Multiplying took the ground's blue down, so grass turned olive, stone ochre, sand gold and brick burnt orange, each apart from the red beside it.
  - The fill was `0xffd21f` at 0.75, above the red's 0.6, so it read yellow over brick's red and grass's green.

### As built: PR 2, blocks on the server

- **Data:**
  - `block` is a stage field, and `block` an active effect.
  - `actions.yaml` gives `block: {arc: 180}` and `per_block: 2` in `energy`. The loader keeps `Block.Front`, cos(arc ÷ 2), so a hit costs one dot product.
  - Validation is section 3's, with one addition: a block skill must be granted (a granted skill already has one stage).
- **The five blocks** are as section 3's table gives them, each listed last in its class's `skills`, its counter's chain last in its class's `chains`.
  - Icons: Guard `lorc/checked-shield`, Bastion `lorc/bordered-shield`, Parry `lorc/sword-clash`, Deflect `lorc/shield-reflect`, Ward `lorc/magic-palm`.
- **The world:**
  - `mobs.Landing` carries the attack's `Unblockable`.
  - `cast` keeps the block's window from the action's start, its facing from the aim at the press, and whether this use has given its energy.
  - `mobHits` checks the block after the dodge. A blocked hit is `Damage{Block: true}`. It gives `per_block` once a use, and notes the input for `blocked`, as a dodge does.
  - **The arc's edge** takes a slack of 1e-6: an aim arrives as a float32, so a yaw of exactly 90° comes 4e-8 rad past the edge.
  - A death or a hard status ends the block, where they end the dodge.
  - A blocked `Damage` names the player, so it keeps them in combat (C20), as a dodge's does.
- **Bots** use only their class's main damage skill, so they never block, with no change.
- **The client, in this PR:**
  - The generator carries `block_ms`, `blocked_ms`, `per_block` and the arc.
  - The chains panel and tooltips say "after a block" and "right after a block".
  - Until PR 3, the client doesn't keep the last block, so no key offers a counter. A block draws the generic look.
- **Promotion's swap of Guard** is PR 3's: the skill bar is the client's, saved by its own request, and the server doesn't touch it on promotion.

### As built: PR 3, blocks on the client

- **The counter:** `ChainMemory` keeps the input the newest frame acked when the player's own BLOCK arrives, as it keeps a dodge's, and `holds` judges `blocked` from it as `dodged` is. A new map or a rejoin forgets it.
- **BLOCK** shows where DODGE would, in a miss's grey.
- **The clang,** `hit.block`, plays on the guard's ring toward the blow's mob: a short steel burst, not the class's colour, so it reads as a hit stopped.
- **The guard's look** is the block skills' own when no look is named:
  - a flat half ring round the front at the waist, along the aim at the press, from the action's start for the block's time, coming up in 80 ms and fading over the last 150;
  - **1.05 m** across, picked in a headless render at the game's camera: at 0.8 m its front hid behind the players' heads, and at 1.05 m the whole half shows round them;
  - each class's own colour (the live pass's fixes, below);
  - Deflect's leap draws its take-off dust after it. At 2 m in 0.3 s, it's too slow for speed lines.
- **The pose** is KayKit's `Block` clip, its guard up at 0.28 of the clip and held to its end, so the body puts that frame on the action's start. Every class's look has it, and `npm run models` keeps it, about 31 KB more a hero model. It plays on the upper body alone while walking, as skills do.
- **The card** says "Blocks blows from the front for 0.6s, but not red-marked ones", Deflect's leap, and that the first blow stopped gives 2 energy. A point adds nothing to a granted skill.
- **The promotion swap:** on `Promoted`, a key holding a block that isn't the new class's own takes the class's block, moved there if it stood on another key, and saved as any change to the bar is. A bar without one is left alone.

### As built: fixes from the live pass

- **Deflect blocks, then leaps.** Leaping from the press, its 2 m cleared a slime's hop: no BLOCK, so no energy or counter. With a body behind, the leap stopped and it blocked. It either dodged by distance without its rewards, or didn't leap.
  - `move` gains `delay`: its start is held that long past the press's next input. Deflect's is 600 ms, its guard's time, and its length grows to 900 ms, so the 300 ms leap runs inside its action and nothing starts under it.
  - Validation holds a delay to 0 or more, and the delay plus the move's time to the skill's length.
  - The world's move mark and the client's predicted mark both add the delay's ticks to the press's input. The shared sim's leap is unchanged, so no vector moves. Both count a zero delay as none, since ticks round any time up to at least one.
  - The trail waits for the delay, and a leap's dust rises where the body is when it takes off, since a player may walk during the guard.
  - The card reads "after 0.6s, leaps 2 m back".
  - **A stun in the guard drops the leap.** A status that stops acting clears the waiting mark as it lands, and a held body starts no move. The live pass couldn't show it, since slimes stood behind in every try, so a world test pins it: King Slime's slam lands 8 inputs into the guard, and no leap comes at 600 ms or after the stun. A client test pins the same, its one correction the stun's own frame.
- **Each class's ring has its own colour.** It had been the class's attack's damage colour, so four of five were the same orange. A small table beside `blockOf`, `blockColors`, colours the ring and the block's tile:

  | Class | Colour |
  | --- | --- |
  | Beginner | silver `#d8dde6` |
  | Warrior | blue `#4f86ff` |
  | Thief | violet `#a66bff` |
  | Bowman | green `#46d160` |
  | Magician | pink `#ff6ad5` |

  - The hues are spread round the wheel, away from the warning areas' yellow and red.
  - They were checked in a headless render on grass, snow, brick and sand, at the game's camera and closer. All five read apart; the Bowman's green is the faintest on grass, held out by the ring's dark edge.
  - **The Beginner's was orange `#ff9a3c`** until the second live pass: 18° of hue from the boss warning's yellow edge, where the others are 43° or more from any warning. Silver has no hue near a warning's.
    - It reads plainly on grass, brick and sand.
    - On snow it's the faintest of the five, held by its dark edge as the green is on grass. A darker silver, `#a9b2c0`, drew the same, since the ring's glow lifts both.

### As built: lighter warning colours

- **The user's words** (2026-11-18): "actually the colors of yellow and red line should be lighter, like the ones in blade and soul".
- **Why:** a boss's area tinted the ground by multiplying, which can only darken it. On dark ground both colours turned muddy: on grass the red read dark brown and the yellow olive, and on the Fallen Keep's brick the Shade Lord's yellow lane read dark orange.
- **The look** (`effects/mob-looks.ts`):
  - **The area and the fill growing over it each glow.** A `'glow'` decal (premultiplied alpha) covers the ground at its alpha and adds a further `GLOW`, 0.25, of its colour as light (`effects/fx.ts`). So it lightens the ground as it colours it.
    - Adding light alone left red and yellow both pale lime on grass and both peach on brick.
    - Laying the colour over alone mixed it with a ground of the opposite hue toward brown: khaki on grass and on stone.

    | | Area, at 0.4 | Fill, at 0.75 | Rim |
    | --- | --- | --- | --- |
    | Red | coral `#ff8a7a` | coral `#ff7563` | `#ff3a2a` |
    | Yellow | lemon `#ffe870` | gold `#ffd43a` | `#ffc21a` |

  - **Every boss area has a rim,** a 6.5 cm line in its own colour round its edge. It holds the area's shape on snow and sand, where a light area fades.
  - **A status's edge moves 12 cm outside the rim.** Every slam is red and stuns, and the stun's yellow on its rim would have made it read as yellow.
  - **The rim and the edge have ghosts,** as the area and the fill do (3d-world.md section 7).
  - **Where the place opens,** a glow fades its colour with its alpha, since the blend takes the colour as written. The multiply tint and its branch in the decal material are gone.
- **Checked** in a headless render at the game's camera, both colours mid-fill on grass, stone, sand, snow and brick, and in a Shade Lord fight on brick, before and after.
  - Every area lightens its ground, and the fill reads over the area as it grows.
  - Red and yellow read apart on each ground, in the area, the fill and the rim. The yellow lane reads yellow on brick.
  - The class rings, the Beginner's silver too, read apart from both. Red's hue is 4° to 7° and yellow's 44° to 50°, against the magician's pink at 317° and the bowman's green at 131°.

## 9. Decisions

The user's choices are in section 1. These calls were made in planning and logged for the user to push back on:

1. **Blocks are granted with the class,** as break-free skills are: no points, no evolutions, no traits. A Beginner has Guard from level 1, since normal mobs no longer mark their attacks.
2. **Each class's block has its own numbers:** the Warrior's back in 2 s (the user's call of 2026-11-18), the Thief's short, the Bowman's with a leap back, the Magician's between.
3. **The counters reuse skills:** Strike, Ground Slam, Flurry, Power Shot and Frost Nova, each 1.5 s after a block.
4. **Only a boss's attack can be unblockable,** enforced by validation. The first bosses' slams are, and their family attacks aren't.
5. **A block's energy comes once per use,** at its first blocked attack.
6. **A block faces its aim at the press** and doesn't turn as the player moves. The guard is a commitment, as a dodge is.
7. **A block isn't cut by a basic attack,** so mashing attack never drops the guard.
8. **A blocked attack starts no hit guard,** as a dodged one doesn't, so a block can take a crowd's blows one after another.
9. **Normal mobs' statuses show no warning,** since their areas are gone. Their mark shows when they land.

## 10. Alternatives considered

- **Telegraphs for every mob, coloured by blockability:** the user wanted normal mobs read from their bodies.
- **A block from every side:** the user chose the front half, so position matters in a crowd.
- **A block costing energy:** the user chose a free block.
- **Block skills with points, evolutions and traits:** a block has nothing to grow per point, and points spent on it would be wasted.
- **New counter skills made for blocks:** more content than the first pass needs; the chains reuse skills that already counter well.
- **A perfect-block window** with a bonus: timing on top of a short window is more than this game's no-timing cancels ask of players.
