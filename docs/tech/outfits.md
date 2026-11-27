# Outfits

> **Status: approved · 2026-11-19.** The high-level design for [D8. Outfits and wardrobe](../product/features/items-and-inventory/d8-outfits.md) and [D9. Getting outfits](../product/features/items-and-inventory/d9-getting-outfits.md).

## 1. What has to hold

- **The user's words** (2026-11-19): "lets add outfit system like that of bns", then "also we'll add wing fashion".
- **Their choices:**
  - Five slots: outfit, headwear, face, weapon look, and wings, added after the rest.
  - Wings are looks only, flap by themselves, hide the outfit's cape, and come from prestige play: the top PvP ranks, the hardest bosses and late feats.
  - Pieces come from field bosses, PvP ranks, quests and feats. An outfit vendor comes once gold and NPC shops exist. Gacha or a paid shop stays open for later, and nothing paid is built.
  - One wardrobe per account. A piece from a drop is an item, tradeable until it's added.
  - The art is KayKit, as today, with colourways made for the game. The user compared Quaternius's modular outfits, which look closer to Blade & Soul, and kept KayKit, since Quaternius needs a new rig and new combat clips.
  - Face pieces are modelled for the game, since KayKit has none.
  - KayKit's Skeletons pack gives rare outfits from the undead bosses.
- **Looks, never power.** Gear never shows, and a piece never changes stats, hits, reach or a body's size.
- **The client computes, the server validates.**
  - The client draws looks, previews any piece, and writes "how to get" from the game data.
  - The server checks every piece worn is collected, in its slot, and of the class's weapon kind. It makes every grant.
- **Names:** the player sees "wardrobe". In code, a *piece* is one entry of it, and a *look* is the five picks a character wears. The world's `Wardrobe` port already reads worn gear at a join, and it grows to read the look too.

## 2. Today

- **Looks:**
  - `classes.yaml` gives each class `look: {model, tint}`.
  - The client's `world3d/model-looks.ts` maps a model to its file, the meshes it hides, the weapon its swing trail follows, and the part its tint colours.
  - `Body` clones one file per figure, with its own skeleton. Others are drawn from `PlayerView.class` alone, once, when they first come into view.
- **The art:** KayKit Adventurers 1.0, five files.
  - Each file is a 41-joint rig with the same joint names, order, parents and rest pose, and the same 76 clips.
  - Each body is six skinned meshes (`*_ArmLeft`, `ArmRight`, `Body`, `Head`, `LegLeft`, `LegRight`).
  - Gear meshes are rigid on bones:

    | File | On `head` | On `chest` | On `handslot.r` | On `handslot.l` |
    | --- | --- | --- | --- | --- |
    | Barbarian | `Barbarian_Hat` | `Barbarian_Cape` | `1H_Axe`, `2H_Axe`, `Mug` | `1H_Axe_Offhand`, `Barbarian_Round_Shield` |
    | Knight | `Knight_Helmet` | `Knight_Cape` | `1H_Sword`, `2H_Sword` | `1H_Sword_Offhand`, four shields |
    | Mage | `Mage_Hat` | `Mage_Cape` | `1H_Wand`, `2H_Staff` | `Spellbook`, `Spellbook_open` |
    | Rogue, Rogue_Hooded | none (the hood is in `Rogue_Head_Hooded`) | `Rogue_Cape` | `Knife`, `1H_Crossbow`, `2H_Crossbow`, `Throwable` | `Knife_Offhand` |

  - Each file has one material on one 1024 px atlas. `npm run models` drops the gear no look shows and the clips no look plays.
- **Items:**
  - Every item is a character's.
  - Causes are `drop`, `player`, `starter`, `quest` and `promotion`. Nothing is bound or held per account.
  - Boss drops are each eligible player's own roll (`bossFell`). Quest rewards are `{exp, items}`.
  - A quest hand-in takes items and records itself (`quest_hand_ins`) in one item-module transaction.
- **Missing:** no currency, shop, marketplace or trade, and no achievements. PvP ranks give nothing.

## 3. Data: `server/data/outfits.yaml`

```yaml
pieces:
  - {id: knight_crimson, slot: outfit,   name: Crimson Plate,  stars: 4, look: knight_crimson,
     from: [{boss: king_slime, chance: 0.02}]}
  - {id: skeleton_warrior, slot: outfit, name: Bone Warrior,   stars: 5, look: skeleton_warrior,
     from: [{boss: crypt_warden, chance: 0.01}]}
  - {id: helm_gold,      slot: headwear, name: Gold Visor,     stars: 4, look: knight_helmet_gold,
     from: [{rank: gold}]}
  - {id: round_glasses,  slot: face,     name: Round Glasses,  stars: 2, look: round_glasses,
     from: [{feat: level_10}]}
  - {id: bone_staff,     slot: weapon,   kind: staff, name: Bone Staff, stars: 4, look: skeleton_staff,
     from: [{boss: shade_lord, chance: 0.02}, {quest: magicians_trial}]}
  - {id: crystal_wings,  slot: wings,    name: Crystal Wings,  stars: 6, look: wings_crystal,
     from: [{rank: master}]}
feats:
  - {id: level_10,      name: Reach level 10,           when: {level: 10}}
  - {id: first_boss,    name: Beat a field boss,        when: {boss: any}}
  - {id: promoted,      name: Take a second class,      when: {promoted: true}}
  - {id: first_pvp_win, name: Win a PvP match,          when: {pvp_wins: 1}}
```

- **A piece:**
  - `id`, `slot` (`outfit`, `headwear`, `face`, `weapon` or `wings`), `name`, `stars` as items have, and `look`.
  - `look` is a key in the client's look table (section 6).
  - A weapon piece names its `kind`.
- **Sources (`from`):**
  - `{boss, chance}`: the loader adds the piece's item to that boss's drop list, rolled on each eligible player's own roll as today.
  - `{rank}`: a rank id from `pvp.yaml`, other than `bronze`.
  - `{quest}`: the quest's hand-in grants the piece. As built (4b-1), the loader adds the piece to that quest's rewards. A quest source names no boss or chance itself, though the piece may have a boss source of its own as well (one a boss drops and a quest gives, as `bone_staff` above); a piece names a quest once.
  - `{feat}`: a feat below. As built (4b-2), a feat source names no boss, chance, rank or quest itself, and gives its piece outright with no item.
  - `vendor` and `gacha` come with their features.
- **Feats:** `when` is one condition the server sees as it happens:
  - `level: N`, at a level-up to N;
  - `boss: any` or a boss id, at a boss's rewards;
  - `promoted: true`, at a promotion;
  - `pvp_wins: N`, at a match's end, counting the character's wins over every mode;
  - `quest: <id>`, at a hand-in.
  - **As built (4b-2):** the loader reads `feats:` from `outfits.yaml`. Each has an id of its own, a name, and exactly one condition: a level from 2 to the cap, `any` or a field boss, `promoted: true`, or a quest. Every feat must give some piece. As built (4b-3), `pvp_wins` joins them, counting from 1.
- **Items for pieces:**
  - Every piece with a `boss` source gets an item template the loader makes: id `piece.<id>`, tab `etc`, stack 100, the piece's stars, and a description naming the piece.
  - Vendor and gacha sources later use the same items.
- **Classes:** `classes.yaml` gains `weapon: <kind>`:
  - `one_handed` for Beginner and Warrior;
  - `knife` for Thief;
  - `crossbow` for Bowman;
  - `staff` for Magician.
- **Loader rules:**
  - Ids are unique, and no piece's item id clashes with `items.yaml`.
  - Slots are known. Only weapon pieces name a kind, and it's one a class has.
  - Every piece has a source this build has. A boss source names a `boss: true` mob, with a chance in (0, 1].
  - Every rank, quest and feat named exists, and every feat gives some piece.
  - A rank gives at most one piece.
- **`gen-gamedata`** carries the pieces, without chances, and the feats' names, for the wardrobe's lists and "how to get". As built, it also writes the item of each piece a boss drops as the server's loader makes it (`piece.<id>`, Etc, the piece's name and stars), so the inventory names a dropped piece.

## 4. Server: the wardrobe in the item module

- **Why the item module:** adding a piece takes an item and makes a wardrobe entry. That's one transaction, as a quest hand-in already is there. Pieces are possessions, as items are.
- **Migration `00013_item_outfit.sql`:**
  - `account_pieces(account_id, piece_id, cause_type, cause_ref, item_id NULL, created_at, updated_at, PRIMARY KEY (account_id, piece_id))`: the wardrobe. `cause_type` is `item`, `rank`, `quest` or `feat`. `item_id` is set exactly when it's `item`, and `cause_ref` exactly when it isn't.
  - `item_id` names the stack added from. It isn't unique: a stack outlives one add.
  - `character_looks(character_id PRIMARY KEY, outfit, headwear, face, weapon, wings, hide_headwear, revision)`. A null pick is the class's own, and a null `wings` is none. No row means the class's own look.
- **Item events:**
  - A new kind, `collected`: quantity −1, taken from the stack, cause `player`.
  - `templateTotals` (check 6) counts it and `used`. It hadn't counted `used`, which a randomized test found once potions were used in it.
  - A new invariant (check 13): every `collected` event has the `account_pieces` row naming its item, and every such row has its event.
- **Adding** (`AddToWardrobe{item}`, through the server straight to the item module, as discarding goes):
  1. The item must be the actor's, in the inventory, and a piece's item. Otherwise `ITEM_NOT_FOUND` or `NOT_A_PIECE`.
  2. A piece already collected is `PIECE_COLLECTED`, and nothing changes.
  3. Otherwise, in one transaction: take one from the stack (`collected`) and insert the row.
  4. It answers with the inventory change. The client knows the piece from its item. Idempotent by key, as item actions are.
- **Grants without items** (step 4, with their callers): `GrantPieces(actor, pieces, cause)` inserts with `ON CONFLICT DO NOTHING`, so a second grant of a piece is a no-op. It returns what was new.
  - **Ranks:** at a match's end, the pvp module grants the piece of each rank the new shown rating reaches. The account's row is the "first time": falling and climbing again finds it there.
  - **As built (4a-2):**
    - The item module's `GrantPieces(account, grants)` takes each grant as a piece, its cause and its rank, in one transaction. It refuses the whole call if a grant names a source its piece doesn't have. A grant writes no item event or notice, so checks 6 and 13 count none.
    - The pvp module grants through an `Items` port once the match is stored, after `MatchEnded`, in a rated mode, for a placed player alone, since the result shows no rank while placing. It asks for every rank piece the shown rating reaches, so a grant lost to a failure comes with the next rated match. A match's grants share one minute's retries, so a wardrobe that's down holds the outcome a minute however many players wait.
    - A placed player who left before the outcome is rated as a loss and gets the pieces of the rank that rating shows, like anyone else: the pieces follow the shown rating, not how the match went.
    - A piece no boss drops has no item, and `gen-gamedata` writes none for it either.
  - **Quests:** a hand-in's rewards gain `pieces`, granted in the hand-in's transaction.
  - **As built (4b-1):**
    - The world sends a quest's pieces with its hand-in. A quest that gives a piece and nothing else still goes through the item module.
    - A piece with a boss source and a quest source both is a dropped item and the quest's grant: the boss drops `piece.<id>`, and the hand-in grants the piece outright.
    - The item module refuses the hand-in if a piece doesn't name that quest as a source. Otherwise it grants each piece to the account in the hand-in's transaction (cause `quest`, `cause_ref` the quest), as `GrantPieces` does. A hand-in refused for its items grants none.
    - The stored reply keeps the pieces new to the account, so a repeat, or the join that finishes a hand-in the world missed, tells of them again. Another character of the account handing the quest in finds them there and hears of none.
    - The world pushes `PiecesAdded` (cause `quest`) once the quest finishes.
  - **Feats:**
    - The world checks level, boss, promotion and quest feats where those happen, against the catalogue's feats.
    - The pvp module checks `pvp_wins` at a match's end.
    - Each grants as above.
  - **As built (4b-2), the world's feats:**
    - **When it asks:** at a level-up it asks for every level feat at or below the new level. At a field boss's rewards, it asks each player whose group dealt the drop share for the `any` feats and that boss's, as their drop roll is. At a promotion it asks once the class has changed. When a quest finishes (live, or at the join that finishes a hand-in the world missed), it asks for that quest's feats.
    - **How:** a worker of the item-call pool grants the pieces through the item module's `GrantPieces` (cause `feat`, `cause_ref` the feat), retrying until the item-call budget runs out.
    - **At each join:** once the world's join (a login or a reconnect, not a map change) has landed, the map hands the feat worker an ask for every feat the character's save shows met: a level reached, a promotion taken (a class with a parent), a quest finished. The join never waits on the item module; the pieces come a moment later, as a live feat's do.
    - **A lost grant:** a refused grant, one given up, or one the pool was too busy to take is logged. The next join grants it, and so does the next time the feat is met. A boss feat has no record a join can see, so one lost comes with the next boss beaten.
    - **A feat added later** reaches every character who already meets it at their next join.
    - **The notice:** the player hears `PiecesAdded` (cause `feat`) for the pieces new to the account. One gone from the map hears nothing and finds them in the wardrobe.
    - **Bots** do no feat.
  - **As built (4b-3), the PvP win feats:**
    - **When pvp asks:** at a match's end, rated or not, for each winner who didn't leave. A loss, a draw, or a win the character left asks for nothing.
    - **What it counts:** the character's wins over every mode, read from the store's records once this match is stored. It asks for the pieces of each `pvp_wins` feat that count reaches.
    - **How:** pvp grants through its `Items.GrantFeatPieces` port, which calls the item module's `GrantPieces` (cause `feat`, `cause_ref` the feat), retrying within the match's one grant budget, shared with the ranks' pieces. The winner hears `PiecesAdded` (cause `feat`) after the match's outcome, for the pieces new to the account.
    - **A lost grant:** a failed or given-up grant is logged. Every win asks again, so it comes with the next win. A join doesn't ask for win feats, since the world doesn't see pvp's records.
- **Notices** (step 4): a grant or an add that made a row pushes `PiecesAdded{pieces}` to the account's live session. Until then the add's answer is the only word.
  - **Who pushes it:** the caller of the item module, on the stream it already has to the player; the item module keeps no stream of its own. A rank's at a match's end goes through pvp's deliveries (as built, 4a-2); a quest's (as built, 4b-1) and a feat's at a level, a boss, a promotion or a quest (as built, 4b-2) go through the world's; the PvP win feat's at a match's end goes through pvp's (as built, 4b-3).
  - **As built (4a-1):** only the add. The server delivers it to the adding session once the add succeeds; a refused add pushes none. A replayed add (the same idempotency key) pushes it again, and the client shows a piece's notice once each time the player enters the world.
- **The look:**
  - `SetLook{look}` goes through the world, as `Equip` does. The world refuses a dead player (`CHARACTER_DEAD`) or one in combat (C20's 5 s clock, `IN_COMBAT`, as a trait change is).
  - The item module then checks each pick, wings too, against the player's live class:
    - a known piece of its slot (`UNKNOWN_PIECE`, `WRONG_SLOT`);
    - collected by the account (`PIECE_NOT_COLLECTED`);
    - a weapon look of the class's kind (`WRONG_WEAPON_KIND`).
  - It stores the look with the next revision. The world applies the look and sends it in the player's view.
  - **A join** reads the look with the worn gear, through the `Wardrobe` port.
- **Promotion:**
  - A weapon look the new class's kind doesn't take is cleared in the trainer gift's item transaction, which returns the look. The world wears it before `Promoted`, in the same tick as the new class.
  - A crash after that commit but before the checkpoint saves the new class leaves the old class without its weapon look. It's the same window the gift already has.
  - A look and a promotion never overlap. Each is checked against the class the other may change, so whichever is in flight refuses the other as unavailable.
- **Bots:**
  - PvP bots wear a random look, seeded by their id: each slot is the class's own half the time, otherwise a piece of that slot every source of which is a boss or a feat (feat pieces joined in 4b-2), and a weapon look of the class's kind.
  - They never wear rank pieces, so a rank piece always means a player earned it.
  - cmd/bot's players wear what their accounts own.
- **`GET /api/wardrobe`**: the account's pieces, oldest first, each with its cause, the item for an item, and when it was added. None is `[]`.

## 5. Protocol

| Message | Way | What |
| --- | --- | --- |
| `Look{outfit, headwear, face, weapon, wings, hide_headwear}` | — | Piece ids; empty is the class's own, or no wings |
| `PlayerView.look = 33` | server | In every view of the player |
| `SetLook{idempotency_key, look}` | client | Wear these picks |
| `AddToWardrobe{idempotency_key, item_id}` | client | Add an item's piece; answered with the inventory change |
| `PiecesAdded{pieces: [{id, cause}]}` | server | New wardrobe entries, from step 4 |
| `GET /api/wardrobe` | client | The account's pieces |

**Failures,** in the item area after `BAD_ITEM_BATCH`:

| Code | Name | HTTP | When |
| --- | --- | --- | --- |
| 220027 | `NOT_A_PIECE` | 409 | Adding an item that holds no piece |
| 220028 | `PIECE_COLLECTED` | 409 | Adding a piece the wardrobe has |
| 220029 | `UNKNOWN_PIECE` | 400 | A pick that isn't a piece |
| 220030 | `WRONG_SLOT` | 400 | A piece in another slot's place |
| 220031 | `PIECE_NOT_COLLECTED` | 409 | Wearing a piece the account hasn't collected |
| 220032 | `WRONG_WEAPON_KIND` | 409 | A weapon look the class can't hold |

Changing looks in combat is `IN_COMBAT` (330037), the code a trait change in combat answers.

## 6. Client: drawing a look

- **Parts on the class's rig:**
  - A figure still clones its class's file, for the rig and the clips.
  - The outfit's skinned meshes come from the body's parts file and are bound to that skeleton. The rigs share joint order, so the bind is a skeleton swap, and `Body.stand()` throws if a file's joints differ.
  - The class's own body meshes hide.
  - Headwear and face pieces parent to `head`, and a weapon look to `handslot.r`, in place of the class's weapon.
  - Wings parent to `chest`, and the outfit's cape hides while they're worn (below).
  - The swing trail follows the weapon shown. The afterimage copies the look.
- **Files:** `npm run models` writes:
  - beside today's class files, a parts file per source file: meshes only, no clips, with every mesh a piece uses;
  - the Skeletons pack's files;
  - `Face.glb`, the face pieces, and `Wings.glb`, the wings.
  - A parts file loads the first time a piece needs it, once.
- **Colourways:**
  - A colourway is a recoloured copy of a file's atlas. `npm run models` writes it from a table in `world3d/outfit-looks.ts`: the atlas swatches it changes and their new colours. It's shrunk to 128 px, as the forest's atlas is.
  - Skin and face swatches never change.
  - The toon palette keys materials by texture, so each colourway is one material shared by everyone wearing it.
- **The look table,** `world3d/outfit-looks.ts`, beside `model-looks.ts`:
  - Each piece's `look` key names its file, meshes, colourway and attach bone.
  - Where a piece made for one head sits wrong on another, it gives a `fit` for that body: an offset and a scale, tuned in the preview.
  - A test checks every piece in the generated data has a look whose meshes exist.
- **Wings:**
  - Each pair is two halves, each on a hinge node at the back of `chest`. Each half is modelled spread flat across the back from its root, where its hinge goes.
  - The client turns each hinge every frame from the figure's state, so no clips are needed. Open is degrees from folded straight back, 0, to spread flat, 90; the swing beats either side of it:

    | State | Beat | Swing | Open |
    | --- | --- | --- | --- |
    | Standing | 2.4 s | ±6° | 55° |
    | Walking or running | 1.2 s | ±10° | 60° |
    | Sprinting | 0.6 s | ±16° | 45°, swept back |
    | In the air | 1.6 s | ±12° | 80° |
    | Acting, or not up (below) | — | folded | 10° |

    - These are a start: the camera sits behind and above, where the first angles, 20° to 45°, read as thin slivers. The final values are tuned with the user on step 5's preview sheet, at close and far zoom.
    - As built, wings fold whenever the body plays a clip over its legs: an attack, a skill or a block, a flinch, a knockdown and rising, and death. So they never hide an action's pose, and don't cut into the ground under a body that's down.
  - It eases between states over 150 ms. A remote figure uses the gait and action it already draws.
  - A pair marked `glow` in the look table draws in the toon material's lit band only, with a soft rim, so prestige wings read from afar.
  - The afterimage copies their pose, and the wardrobe's figure flutters.
  - **The cape** (`*_Cape` on `chest`) hides while wings are worn, and shows again when they're off.
- **Others:** a remote figure is redrawn when its view's class or look changes. Until a new part loads, the figure keeps what it showed.
- **Cost:**
  - A figure draws the same number of meshes as today: six to nine skinned parts, gear, and each one's ink. Wings add two meshes and two hinge turns a frame, in place of the cape's one mesh.
  - Colourways add materials, not draw calls, since each figure already draws on its own.
  - Measured with the regions soak's crowded town (large-regions.md section 13).
- **As built (step 2):**
  - `npm run gen` writes each piece's id, slot and look key to `gamedata/outfits.ts`. A test checks each has a look of its slot. A piece id the client's data lacks, as an older client meets, is drawn as the class's own for that slot (3d-world.md section 11).
  - A body is named by its class and each slot's piece, so a promotion or a look change redraws it, and a look wearing nothing keeps the class's own figure. The player is dressed by their own first frame, others by theirs. The old figure stays until the new one is in.
  - Dressing comes after `stand()`, so no piece or bare head changes the body's size. A bare head hides headwear only. The hooded Rogue's hood is part of its head, so it stays under a bare head; worn headwear swaps the head (below).
  - Rigged class files and parts files keep raw positions. Quantizing folds each file's grid into its skins' inverse bind matrices, so files' rigs differed; a test holds one rig across them.
  - A skin swatch is the face's atlas cell and any cell sharing its colours, such as the Mage's hands.
  - A crowd of 40 dressed players drew within noise of 40 in their class's own looks.
- **As built after the outfits playtest:**
  - **The hooded head under headwear:**
    - **The problem:** the Bowman's hood is part of its head, `Rogue_Head_Hooded`, so every hat sat over it. The green hood filled the Knight's Helm's face opening and wrapped the face under the Bear Hood.
    - **Now:** under a worn piece of headwear, the head swaps for the bare `Rogue_Head`, skinned on the same rig and drawn in the same atlas. The hood is headwear in all but name, so only one thing sits on the head. A bare head, or none worn, keeps the hood.
    - **The look table:** the model gives the swap as `hood: { head, file, bare }` in `model-looks.ts`. `classLook` wears the bare head as a piece from that file's parts, and `dress()` hides the hooded head.
    - **The parts file:** `npm run models` keeps the bare head in the Rogue parts file, which grew from 154.6 KB to 191.3 KB (76.0 to 94.1 KB gzipped).
  - **The Knight's Helm on a Rogue head:** the earring is part of the skinned `Rogue_Head`, so it can't hide on its own.
    - **Now:** the helm's `fit` scales it 1.08 on both Rogue files, so the earring sits inside its cheek guard.
    - **Why 1.08:** in close-up stills, 1.05 and 1.06 still showed its tip, and 1.07 a glint. Both are rigid to the head, so it holds in every pose.
    - **A test** checks every fit names a body a class is drawn as, so a misnamed file can't leave a piece unfitted.
  - **The Horned Hat is the Bear Hood:** it's KayKit's `Barbarian_Hat`, a bear's head. Only the name changed; its id stays `horned_hat`, so saved wardrobes keep it.
- **As built (step 5, the Skeletons pack):**
  - **The rig:** all four bodies have the Adventurers' 41 joints, same names, order and parents, and a test holds their inverse bind matrices to six decimals, as for every parts file.
  - **A skeleton comes with its own head:** its body piece includes its skull (`Head`, or the Mage's `Skull`), jaw and eyes, and is marked `ownHead` in the look table. Like the Rogue's hooded head, it's one thing on the head chosen by the look table: each class model's `own.head` names its own head's meshes, `dress()` hides them for an `ownHead` piece, and a hooded head is not swapped under it. A hat sits on the skull. It's dressed after `stand()`, so the size never changes; without the piece the head shows again.
  - **The files:** a pack's file that holds pieces and that no look draws whole, as its bodies, is written by `npm run models` as its parts file alone. The four weapons are packed into one glb, `Skeleton_Weapons.glb`, with no skin: the rig's joints and the four on `handslot.r`, hung as the Adventurers' weapon of their kind. The rig test reads a file like that by its joints.
  - **Open for the preview sheet:** the pieces' `fit` on each head, and the eyes, which the pack draws in an unlit yellow `Glow` material and the toon material draws as a plain one.

## 7. Client: the wardrobe and the character window

- **The wardrobe,** `features/wardrobe/Wardrobe.tsx`, on `wardrobe` (`KeyO`, key-bindings.md):
  - **Left:** the character in 3D, wearing the picks, turned by a drag. As built (`world3d/portrait.ts`), it's drawn in a small scene of its own on the game's engine, only while the window is open:
    - each frame, before the world, it's drawn into the game canvas's bottom-left corner at the window canvas's size, then copied into that canvas with `drawImage`;
    - the scene and its models stay while the world is open, so a window opened again loads nothing; closing a window disposes only its figure;
    - the scene is made before the world's, which stays the engine's last.
  - **Right:** tabs Outfit, Headwear, Face, Weapon and Wings. Each is a grid of every piece in that slot: collected ones lit, the rest greyed. The Weapon tab lists only looks of the class's weapon kind, the only ones the server takes.
  - **A card** on hover: the name, stars, slot, kind for a weapon look, and "How to get", written from its sources:
    - "Dropped by King Slime";
    - "Reach Gold in 1v1 or 3v3";
    - "Feat: Reach level 10";
    - "Quest: The Magician's Trial".
  - **Picking** tries a piece on, collected or not. **Wear** sends `SetLook` with the collected picks. **Reset** goes back to what's worn.
  - Each slot has the class's own look first, headwear has Hidden, and wings have None.
  - A count, "Collected 12 of 45", sits under the grid.
  - A refusal shows the server's message.
- **The character window:** its empty figure becomes the same 3D view of the character, as worn. As built, both windows draw it with `features/shared/FigureView`.
- **The inventory:**
  - A piece's item card gains Preview, which opens the wardrobe on it, and Add to wardrobe.
  - Add to wardrobe asks first: "Add <name> to your wardrobe? It can't be traded after." For a piece already collected it's greyed, with "Already in your wardrobe".
  - **As built:**
    - the account's pieces load as a character enters the world, and again as the wardrobe opens, so the card knows a piece already collected;
    - Preview opens the wardrobe at the piece's tab, tried on over what's worn; it's greyed for a weapon look the class can't carry, which can still be added for another character;
    - once the server answers an add, the wardrobe holds the piece and its notice shows.
- **Notices:** each new piece shows "New in your wardrobe: <name>". As built (4a-1), `PiecesAdded` shows it and marks the piece collected, once a piece; the inventory's add only asks.

## 8. Art

- **KayKit Adventurers 1.0:** already in the repo. Every body, hat, cape and weapon in section 2's table can be a piece.
- **KayKit Skeletons 1.0** (Kay Lousberg, CC0, https://github.com/KayKit-Game-Assets/KayKit-Character-Pack-Skeletons-1.0):
  - Four bodies: Warrior, Mage, Rogue and Minion.
  - Headwear: the Warrior's helmet, the Mage's hat and the Rogue's hood.
  - Weapons: a blade, an axe, a staff and a crossbow. Its shields stay out.
  - Vetted: its rig has the adventurers' 41 joints with the same names, order and parents, its licence file is CC0, and it holds no scripts.
  - Recorded in `web/public/models/LICENSES.md` as the other packs are.
- **Face pieces:**
  - Modelled in Blender for the game, low-poly and rigid on `head`, coloured from KayKit's atlas swatches: an eye patch, round glasses, a cloth mask and goggles.
  - Recorded in LICENSES.md as made for the game, CC0.
- **Wings:**
  - Modelled in Blender for the game, about six pairs: feathered, dark feathered, bat, butterfly, dragon and crystal.
  - Each half is low-poly, hinged at its root, and coloured from KayKit's atlas swatches. Pairs can have colourways.
  - No CC0 wings were found. The paid packs' royalty-free licences don't clearly allow sending the raw files to every browser, as a web game must.
  - Recorded in LICENSES.md as made for the game, CC0.
  - **As built (step 5, part B):** five pairs, made in code, not in Blender, by `web/scripts/wings.ts` (`npm run wings`, which `npm run models` runs first), so anyone can rebuild them and a test holds the committed file to the script's bytes.
    - **The pairs:** feathered (three layered rows of feathers, 2.0 m across), bat (three finger bones with a scalloped membrane, 2.2 m), butterfly (two lobes a side with a rim and spots, 1.8 m), dragon (bat-like with a thick leading bone and claws, 2.6 m) and crystal (nine faceted shards fanned from the root, 2.2 m). Each half is flat across the back but for its thickness (a crystal's shards are four-sided points), a few hundred triangles at most.
    - **Two-sided:** each flat half carries a face to either side, the second wound the other way and lit toward the front, in the same swatches, and the material culls: the toon ramp shades a face seen from its back black, as the first sheet showed from the side. A crystal's shards are closed solids and keep one face each.
    - **The file:** `models-src/wings/Wings.glb`, one atlas of flat swatches on KayKit's 8 × 4 grid (cell 0,0, the face's in KayKit's, is never drawn from) and ten meshes named `<Kind>_Wing_L` and `_R`, each on a node at its half's root, which is the hinge: (±0.04, 0.15, -0.12) in the chest's axes. `npm run models` writes only its parts file, `wings/parts/Wings.glb`, with the positions kept, so the hinge stays where the script put it.
    - **The looks** (`outfit-looks.ts`, looks only, no outfits.yaml entry until the user picks): `wings_feather`, `wings_raven` (the feathered pair in the `wings_raven` colourway: its feather, tip and shade swatches), `wings_bat`, `wings_butterfly`, `wings_dragon` and `wings_crystal`, the last two `glow`. The flap angles stay data in `wear.ts`'s `FLAPS`, to be tuned on the preview sheet.
- **As built (step 5, part A, the face pieces):** four pieces made in code by `web/scripts/face.ts` (`npm run face`, which `npm run models` runs after the wings), from the same `web/scripts/lib/mesh.ts` shapes, so a test holds the committed `models-src/face/Face.glb` to the script's bytes.
  - **The pieces:** round glasses (two 8-sided rims, a bridge, an arm to each ear), an eye patch (a disc over the wearer's left eye with a strap across the forehead and one to the left ear), a cloth mask (a sheet from the nose bridge to the chin, wrapped to the cheeks, an ear loop each side) and goggles (two thick round frames with a lens each, a bridge and a strap round the head). Red Mask and Frost Goggles are colourways of the mask and the goggles. The goggles' lenses are opaque, so the eyes are hidden behind them.
  - **The file:** one atlas, each role in a swatch of its own so a colourway recolours one piece alone, and four meshes on nodes at the scene's root with their vertices in the head joint's axes (+x the wearer's left, +z forward), no skin. `dress` mounts every piece made in code the same way, on a node on the joint its slot names: wings on a hinge at the chest, a face piece on a still node at the head. `outfit-looks.ts` fits the hooded head's brim a little further out.
  - **Checked on all five heads** (Barbarian, Knight, Mage, Rogue and the hooded Rogue): a test lays each piece on each head's face in the head joint's axes and holds it in front of the face, never in it, and within a hand's width of it.
  - **The looks** (looks only): `round_glasses`, `eye_patch`, `cloth_mask`, `red_mask`, `diver_goggles` and `frost_goggles`.
    - **Looks only:** a test wears each pair on a warrior and a thief and checks the figure's size and its own meshes' bounds don't move; the server never sees a look, so no body size or reach changes.
    - **Shared helpers:** shapes, flat swatch UVs and the one-primitive mesh builder are in `web/scripts/lib/mesh.ts`, which the face pieces use too.
- **Weapon-look candidates,** kept or dropped in the preview:
  - one-handed: `1H_Axe`, `1H_Sword`, `2H_Sword`, `2H_Axe`, `Mug`, the skeleton blade and axe;
  - knife: `Knife` and its colourways;
  - crossbow: `2H_Crossbow`, `1H_Crossbow`, the skeleton crossbow;
  - staff: `2H_Staff`, `1H_Wand`, the skeleton staff.
- **The catalogue,** about 50 pieces, is designed with the user from a preview sheet before its PR:
  - every body in each proposed colourway, and every headwear, face piece, weapon look and pair of wings on two classes, the wings standing, sprinting and in a jump;
  - drawn in the game's renderer at close and far zoom;
  - with the proposed source of each piece.
- **Left out:**
  - KayKit Adventurers 2.0, which is on itch.io only and whose rig is unchecked;
  - its paid Extra tier;
  - Quaternius's modular outfits.

## 9. Testing

- **Loader:** each rule of section 3, and the derived boss drops and items.
- **Item module:**
  - adding takes exactly one item and makes the row, all or nothing, idempotent by key;
  - a collected piece refused with nothing changed;
  - grants are no-ops the second time;
  - the new invariant and check 6 hold over randomized adds and grants;
  - a failed commit leaves the item and no row.
- **Looks:**
  - every refusal in section 5;
  - promotion clearing a weapon look that no longer fits;
  - the look surviving a rejoin.
- **Sources:**
  - a boss's personal roll can give a piece's item;
  - a rank's piece arrives once, through a fall and a climb;
  - a quest's piece arrives with its hand-in;
  - each feat kind fires once per account, across its characters.
- **Wire:** `PlayerView.look` in full views and on change. Another client redraws a changed look. Bots never wear rank pieces.
- **Client:**
  - every piece's look loads and binds to every class's rig;
  - wings' state from each gait and action, folded through an action, and the cape hidden only while wings are worn;
  - colourways change no skin swatch;
  - the preview window draws, and closing it stops its view;
  - the inventory card's actions.
- **End to end:** a boss drop added, worn, and seen by another player.
- **Live** (the player harness): try on and wear pieces on each class, check headwear and face fits, watch another player change, and check a crowded town's frame time.

## 10. Build

1. **Server and data:**
   - `outfits.yaml` and its loader, with a starter catalogue of a few pieces and `boss` sources alone;
   - classes' weapon kinds;
   - the migration, adding, the look and its checks, promotion;
   - `PlayerView.look`, `/api/wardrobe`, the failures and the invariant.
2. **The client's looks:** parts files, binding to the class's rig, colourways, attach and fit, wings' hinges and motion, the cape hidden under wings, others redrawn, and the swing trail.
3. **The wardrobe window,** the character window's figure, the inventory card actions, notices and the `wardrobe` key. Built in two parts: 3a the window and its key, 3b the character window, the inventory card and an add's notice.
4. **Sources:** ranks from the pvp module, quest pieces and feats, with `GrantPieces` and `PiecesAdded`. Built in five parts: 4a-1 `PiecesAdded`, which an add pushes; 4a-2 `GrantPieces` and the ranks' pieces, with stand-ins for silver, gold and platinum (the Knight's helm and plate in new colourways) till step 5; 4b-1 quests' pieces, with stand-ins for the four class trials (each class's own weapon in bronze) till step 5; 4b-2 the feats the world sees (Reach level 10, Beat a field boss, Take a second class), with stand-ins (Wanderer's Hat, Crimson Helm, Veteran's Plate) till step 5; 4b-3 the PvP win feat (Win a PvP match), with a stand-in (Victor's Horns, the Barbarian's hat in red fur with gold horns) till step 5.
5. **The catalogue:**
   - the Skeletons pack added, and the face pieces and wings modelled;
   - colourways and sources designed with the user from the preview sheet;
   - then the full `outfits.yaml` and look table.

Steps 1 and 2 can run side by side. Step 5's design comes before its build.

## 11. Decisions

The user's choices are in section 1. These calls were made in planning and logged for the user to push back on:

1. **Gear never shows,** as in Blade & Soul. The outfit is the look, and the ten equipment slots stay stats only.
2. **Each slot can stay on the class's own look,** and headwear can be hidden. A new character needs no pieces to look like its class.
3. **Weapon looks fit by kind:** one-handed, knife, crossbow or staff. A promotion clears a look the new kind doesn't take.
4. **Changing works anywhere, at once,** alive and out of combat, as in Blade & Soul's wardrobe.
5. **Anything can be tried on,** collected or not. Only collected pieces are worn.
6. **Ranks, quests and feats grant to the wardrobe directly,** never as items, since they mark what that account did. Drops, and later the vendor and gacha, give items.
7. **A rank's piece comes the first time the rank is reached,** in 1v1 or 3v3. PvP stays equal: looks only.
8. **Feats are per account and have no window.** The wardrobe's "how to get" names them. Their conditions are ones the server already sees, so they need no counters.
9. **A boss piece is a 1–2% personal roll,** tuned with the catalogue.
10. **PvP bots wear random boss and feat pieces,** never rank pieces.
11. **An item for a piece already collected can't be added.** It stays an item for trading later.
12. **The wardrobe lives in the item module,** so adding is one transaction.
13. **The character window shows the character in 3D,** using the wardrobe's view.
14. **The wardrobe is on O,** a free key.
15. **Wings fold during an attack or skill,** so they never hide the action, and droop folded on a dead body.
16. **Wings flap in code,** from the gait and action each figure already draws, so they need no clips and cost two hinge turns a frame.
17. **Prestige wings glow,** drawn in the lit band only with a soft rim, so they read from afar.
18. **Wings sources:** Diamond and Master ranks, very rare drops from the bosses of levels 22 to 30, and late feats such as level 30. No early source gives wings.
## 12. Alternatives considered

- **Quaternius's modular outfits** (CC0, November 2025): 12 outfits from 62 parts, three colourways each, with grown-up proportions nearer Blade & Soul. The free edition has two outfits. The full packs cost about $50, need a new base character and rig, and their free clips don't clearly cover spellcasting or crossbows. The user kept KayKit.
- **Gear that shows,** as in MapleStory: every item would need a model, and the look would follow stats rather than taste.
- **A wardrobe per character:** pieces would have to be won again on each character, and B&S's wardrobe is per account.
- **Every piece as a tradeable item:** rank and feat pieces would stop marking who earned them.
- **Wings that slow a fall or glide:** offered, and the user chose looks only. It would have changed the shared movement rules, and made wings power in PvP.
- **A bought wings pack** (CGTrader, about $19 for 15 rigged pairs): not CC0, and its licence doesn't clearly allow sending the files to browsers.
- **An achievements window:** feats only grant pieces today, so the wardrobe shows them where they matter.
- **`engine.registerView` for the wardrobe's figure:** it skips the main scene's own draw on frames it draws views, and resizes the master canvas to each view's. Here the master canvas is the game's visible one.
- **A new module for outfits:** adding would cross two modules' transactions. The item module already holds possessions and the worn-gear read.
