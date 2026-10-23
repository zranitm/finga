# Overview

> 👋 **Start here.** The product docs describe what players experience and the rules of the game. How it gets built belongs to the technical design, later. Everything here is a draft; proposals are marked, and open questions live in Product decisions.

## Pitch

A browser-based 2D side-scrolling MMORPG-lite, inspired by MapleStory. Pick one of four classes (Warrior, Thief, Bowman, Magician), hunt mobs, level up, team up in parties of up to 4, and trade on a player-driven marketplace. There is no storyline: progression and the economy carry the game.

## Design pillars (proposal)

1. **Grinding feels good.** Every kill moves you forward: EXP, drops, or currency.2
2. **The economy is fair.** Prices come from players, and nothing is ever duplicated or lost by mistake.
3. **Better together (optional).** Hunting in a party is faster and more rewarding than hunting alone.

## Target players

- Players who enjoy playing games on browser, instead of downloading 100GBs games.
- Players who enjoy classic side-scrolling MMOs and steady progression.
- People who play in short drop-in sessions, solo or with friends.
- Players who like trading and playing the market.

## Product goals

- A new player can sign up, create a character, and defeat their first mob within a few minutes.
- Party play is noticeably more rewarding than solo play.
- The marketplace is active and trusted: players can sell what they loot and find what they need.
- Players never lose items or currency to bugs.

## Non-goals

- Storyline, narrative quests, cutscenes.
- Original or polished art.
- Real-money purchases or any monetization.
- Mobile or console play.
- Player-versus-player combat in the MVP.
- Any MapleStory or Nexon assets, names, or content.

## Core loops

- **Moment to moment (seconds):** move, jump, attack, pick up loot.
- **Session (30–60 minutes):** hunt a map, fill the inventory, return to town, sell and restock, upgrade gear.
- **Long term (weeks):** reach higher levels and new maps, collect better gear, build wealth on the marketplace, take on party dungeons.

## MVP scope

The first public version includes: accounts and characters in all four classes, a small connected world (one town and a few hunting maps), movement, mobs and combat, EXP and levels, drops and inventory, map chat, and parties of up to 4. Currency and the marketplace come right after the MVP (P1). The exact content size is OD-12. The full list is the Features database filtered to P0. Delivery happens in two stages, described in Roadmap under Tech.

## Doc map

1. **Overview** (this page): why the game exists and what's in the MVP.
2. [World](./world.md): towns, hunting maps, channels, and how they connect.
3. [Systems](./systems.md): the game rules, one page each.
   - [Progression](./systems/progression.md) · [Combat](./systems/combat.md) · [Items](./systems/items.md) · [Economy](./systems/economy.md) · [Social](./systems/social.md) · [Gacha](./systems/gacha.md)
4. [Features](./features/README.md): what players can do, with acceptance criteria and priority.
5. [Screens and controls](./screens-and-controls.md): the screen list and key bindings, input for the design phase.
6. [Product decisions](./decisions/README.md): open questions, defaults, and outcomes.

The technical design lives in the Tech section, starting with [High-level design](../tech/high-level-design.md).
