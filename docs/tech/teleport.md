# Teleport

> **Status: approved · 2026-11-17.** The high-level design for [B9. Teleport](../product/features/world-and-movement/b9-teleport.md). The server runs the channel and moves the player with the portals' own hand-off. The client sends the request and draws the bar.

## 1. What has to hold

- **The user's words** (2026-11-17): "allow teleportation in world map".
- **Their choices:**
  - In this phase, any map. Later, visited maps and towns: "supposedly 1, but in this phase i want to teleport to any map".
  - Free, with a 3 s channel that moving, acting, or being hit cancels, so it can't be used to escape a fight.
- **The server decides.** It starts, cancels, and completes the channel. The client's bar only follows it.
- **One way to leave a map.** Arrival goes through `handOff`, as a portal's does. So the checkpoint, the party's map, quests, timers, and the cross-map transfer all behave as they do for a portal.

## 2. The change at a glance

| Part | Today | After |
| --- | --- | --- |
| `server/data/world.yaml` | Chart places | Also `teleport: {channel: 3s}` |
| `client.proto` | — | `Teleport { map_id }` in `ClientMessage`'s `oneof`, a request answered by `Response`; `TeleportCancelled { reason }` in `ServerMessage`'s. Additive |
| World instance | — | A player's teleport channel: start, cancel, complete (section 3) |
| Gateway | — | Maps the two messages, as it maps `UseSkill` |
| Client | — | Teleport here (`map-views.md` section 12), a channel bar, a glow, a cancel notice |

## 3. The world

### Start

`Teleport{map_id}`, stamped with its input seq as `UseSkill` is, starts a channel if:
- the map exists and isn't the player's own;
- the player is alive;
- no channel runs already.

Otherwise it's refused, with a new code each, as public errors:
- `TELEPORT_UNKNOWN_MAP`;
- `TELEPORT_SAME_MAP`;
- `TELEPORT_DEAD`;
- `TELEPORT_BUSY`.

A started channel holds the target map, the press's seq, and its end tick: now plus `channel` in ticks. The `Response` is sent as it starts.

### Cancel

A running channel ends, and the player gets `TeleportCancelled` with its reason, when:

| Reason | When |
| --- | --- |
| `MOVED` | An input with a seq after the press's is stepped with any of left, right, up, down, or jump held |
| `ACTED` | An attack or a skill of theirs starts |
| `HIT` | A mob's hit lands on them. A MISS doesn't cancel |
| `DIED` | They die |

- **Leaving the map** another way, by a portal, drops the channel without a notice. So does a disconnect or a rejoin.
- **Potions, talking to NPCs, and the inventory** don't cancel it, since they aren't actions or movement.

### Complete

- At the end tick, the player is handed off to the target map's player spawn, at its yaw, through `handOff`, as a portal hands them off.
- If an item call of theirs is in flight, the hand-off waits a tick, as `enterPortal` waits.
- The arrival's `MapChange` reaches the client as for a portal.

### Data

- `world.yaml`: `teleport: {channel: 3s}`.
- The loader refuses a channel at or below 0, or over 10 s.

## 4. Client

- **Teleport here** sends `Teleport`. On the `Response`:
  - the map window closes;
  - a channel bar shows at the lower middle of the screen, reading "Teleporting to <map>" and filling over `channel`;
  - a glow plays around the player.
  - A refusal shows its message as a notice, as other refusals do.
- **Moving, jumping, attacking, or a skill key** hides the bar at once, ahead of the server's `TeleportCancelled`, so the screen never shows a channel the server has ended.
- **`TeleportCancelled`** hides the bar if it still shows, and shows a short notice by reason:
  - "Teleport cancelled: you moved";
  - "…you attacked";
  - "…you were hit";
  - DIED shows none, since the death screen already says it.
- **`MapChange`** plays the usual transition, and the bar and glow end with the old map.

## 5. Later: visited maps only

- When the user turns this on, a character's visited maps are saved: a set kept at each landing, beside the checkpoint.
- `Teleport` is then refused for a map that isn't a town and isn't in the set, with `TELEPORT_NOT_VISITED`.
- The World tab shows Teleport here only where it's allowed.
- None of this is built now.

## 6. Testing

- **World:**
  - each refusal;
  - a channel completes at its end tick and lands at the target's spawn and yaw through `handOff`, with checkpoint and party map updated;
  - each cancel reason, and a MISS not cancelling;
  - a potion and an NPC talk not cancelling;
  - a portal taken mid-channel dropping it;
  - an input seq before the press's not cancelling it;
  - an item call delaying the hand-off by a tick.
- **Protocol:** the messages, samples, and contracts; `make breaking`.
- **Client:**
  - the bar on `Response`;
  - hidden at a local movement key;
  - each notice;
  - the window closing;
  - refusals as notices.
- **End to end:** a player teleports from town to the forest and lands at its spawn; a mob's hit on a channelling player cancels the channel with `HIT`.
- **Live:**
  - a teleport from every town to a hunting map and back;
  - a cancel by each cause;
  - the party panel showing the new map.

## 7. Build

One PR, each step one commit, green on `make test`, `make lint`, `make cover`, and `make breaking`:

1. **Data and world:** `teleport.channel`, the channel in the instance, its refusals, cancels, and hand-off.
2. **Protocol and gateway:** `Teleport`, `TeleportCancelled`, and the error codes.
3. **Client:** Teleport here, the bar, the glow, and the notices. The layout view of `map-views.md` section 12 comes first or in the same PR.

## 8. Decisions

The user's choices are in section 1. These calls were made in planning and logged for the user to push back on:

1. **The arrival is the map's player spawn,** where a newcomer by portal would stand, not a chosen point.
2. **What cancels:** moving, jumping, attacking, a skill, a landed hit, and death. Not a MISS, potions, NPCs, or windows.
3. **A refused or cancelled teleport just tells the player why,** with no cooldown after it.
4. **Others see no glow,** only the player leaving.

## 9. Alternatives considered

- **Instant teleport:** a free escape from any fight. The user chose the channel.
- **The client timing the channel alone:** a modified client could skip it. The server runs it.
- **Arriving by a portal of the target map:** a map with several portals would need a rule for which. The spawn is one point, and it's where the map expects newcomers.
