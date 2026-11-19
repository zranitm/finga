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
  - `{quest}`: the quest's hand-in grants the piece.
  - `{feat}`: a feat below.
  - `vendor` and `gacha` come with their features.
- **Feats:** `when` is one condition the server sees as it happens:
  - `level: N`, at a level-up to N;
  - `boss: any` or a boss id, at a boss's rewards;
  - `promoted: true`, at a promotion;
  - `pvp_wins: N`, at a match's end, counting the character's wins over every mode;
  - `quest: <id>`, at a hand-in.
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
- **`gen-gamedata`** carries the pieces, without chances, and the feats' names, for the wardrobe's lists and "how to get".

## 4. Server: the wardrobe in the item module

- **Why the item module:** adding a piece takes an item and makes a wardrobe entry. That's one transaction, as a quest hand-in already is there. Pieces are possessions, as items are.
- **Migration `00013_item_outfit.sql`:**
  - `account_pieces(account_id, piece_id, cause_type, cause_ref, item_id NULL, added_at, PRIMARY KEY (account_id, piece_id))`: the wardrobe. `cause_type` is `item`, `rank`, `quest` or `feat`. `item_id` is set exactly when it's `item`, and `cause_ref` exactly when it isn't.
  - `item_id` names the stack added from. It isn't unique: a stack outlives one add.
  - `character_looks(character_id PRIMARY KEY, outfit, headwear, face, weapon, wings, hide_headwear, revision)`. A null pick is the class's own, and a null `wings` is none. No row means the class's own look.
- **Item events:**
  - A new kind, `collected`: quantity −1, taken from the stack, cause `player`.
  - `templateTotals` (check 6) counts it and `used`. It hadn't counted `used`, which a randomized test found once potions were used in it.
  - A new invariant (check 13): every `collected` event has the `account_pieces` row naming its item, and every such row has its event.
- **Adding** (`AddToWardrobe{item}`, through the gateway straight to the item module, as discarding goes):
  1. The item must be the actor's, in the inventory, and a piece's item. Otherwise `ITEM_NOT_FOUND` or `NOT_A_PIECE`.
  2. A piece already collected is `PIECE_COLLECTED`, and nothing changes.
  3. Otherwise, in one transaction: take one from the stack (`collected`) and insert the row.
  4. It answers with the inventory change. The client knows the piece from its item. Idempotent by key, as item actions are.
- **Grants without items** (step 4, with their callers): `GrantPieces(actor, pieces, cause)` inserts with `ON CONFLICT DO NOTHING`, so a second grant of a piece is a no-op. It returns what was new.
  - **Ranks:** at a match's end, the pvp module grants the piece of each rank the new shown rating reaches. The account's row is the "first time": falling and climbing again finds it there.
  - **Quests:** a hand-in's rewards gain `pieces`, granted in the hand-in's transaction.
  - **Feats:**
    - The world checks level, boss, promotion and quest feats where those happen, against the catalogue's feats.
    - The pvp module checks `pvp_wins` at a match's end.
    - Each grants as above.
- **Notices** (step 4): a grant or an add that made a row pushes `PiecesAdded{pieces}` to the account's live session. Until then the add's answer is the only word.
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
- **Bots:**
  - PvP bots wear a random look, seeded by their id: each slot is the class's own half the time, otherwise a boss piece of that slot, and a weapon look of the class's kind. Feat pieces join them with feats.
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
  - Each pair is two halves, each on a hinge node at the back of `chest`.
  - The client turns each hinge every frame from the figure's state, so no clips are needed:

    | State | Beat | Swing | Open |
    | --- | --- | --- | --- |
    | Standing | 2.4 s | ±6° | 20° |
    | Walking or running | 1.2 s | ±10° | 25° |
    | Sprinting | 0.6 s | ±16° | 30° |
    | In the air | 1.6 s | ±12° | 45° |
    | Attacking, using a skill, dead | — | folded | 5° |

  - It eases between states over 150 ms. A remote figure uses the gait and action it already draws.
  - A pair marked `glow` in the look table draws in the toon material's lit band only, with a soft rim, so prestige wings read from afar.
  - The afterimage copies their pose, and the wardrobe's figure flutters.
  - **The cape** (`*_Cape` on `chest`) hides while wings are worn, and shows again when they're off.
- **Others:** a remote figure is redrawn when its view's class or look changes. Until a new part loads, the figure keeps what it showed.
- **Cost:**
  - A figure draws the same number of meshes as today: six to nine skinned parts, gear, and each one's ink. Wings add two meshes and two hinge turns a frame, in place of the cape's one mesh.
  - Colourways add materials, not draw calls, since each figure already draws on its own.
  - Measured with the regions soak's crowded town (large-regions.md section 13).

## 7. Client: the wardrobe and the character window

- **The wardrobe,** `features/wardrobe/Wardrobe.tsx`, on `wardrobe` (`KeyO`, key-bindings.md):
  - **Left:** the character in 3D, wearing the picks, turned by a drag. It's a second scene on the game's engine, drawn into the window's canvas with `engine.registerView`, only while the window is open.
  - **Right:** tabs Outfit, Headwear, Face, Weapon and Wings. Each is a grid of every piece in that slot: collected ones lit, the rest greyed.
  - **A card** on hover: the name, stars, slot, kind for a weapon look, and "How to get", written from its sources:
    - "Dropped by King Slime";
    - "Reach Gold in 1v1 or 3v3";
    - "Feat: Reach level 10";
    - "Quest: The Magician's Trial".
  - **Picking** tries a piece on, collected or not. **Wear** sends `SetLook` with the collected picks. **Reset** goes back to what's worn.
  - Each slot has the class's own look first, headwear has Hidden, and wings have None.
  - A count, "Collected 12 of 45", sits under the grid.
  - A refusal shows the server's message.
- **The character window:** its empty figure becomes the same 3D view of the character, as worn.
- **The inventory:**
  - A piece's item card gains Preview, which opens the wardrobe on it, and Add to wardrobe.
  - Add to wardrobe asks first: "Add <name> to your wardrobe? It can't be traded after." For a piece already collected it's greyed, with "Already in your wardrobe".
- **Notices:** each new piece shows "New in your wardrobe: <name>".

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
3. **The wardrobe window,** the character window's figure, the inventory card actions, notices and the `wardrobe` key.
4. **Sources:** ranks from the pvp module, quest pieces and feats, with `GrantPieces` and `PiecesAdded`.
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
- **A new module for outfits:** adding would cross two modules' transactions. The item module already holds possessions and the worn-gear read.
