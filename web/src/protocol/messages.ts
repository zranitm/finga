// One typed definition of every Stage 1 message (page 4), as valibot
// schemas the TypeScript types are inferred from, so a type and its check
// can't drift. The golden files in server/testdata/contracts/ are the reference:
// the protocol test decodes and re-encodes every one of them.
import * as v from 'valibot';

import { errorCodes, kickReasons } from './errors';

const int = v.pipe(v.number(), v.integer());
const id = v.string();
const optionalBool = v.optional(v.boolean(), false); // Go omits a false bool tagged omitempty

export const chatChannelSchema = v.picklist(['map', 'party']);
export const tabSchema = v.picklist(['equip', 'use', 'etc']);

export const statsSchema = v.object({ str: int, dex: int, int: int, agi: int, vit: int });

export const characterViewSchema = v.object({
  id,
  name: v.string(),
  class: v.string(),
  level: int,
  exp: int,
  stats: statsSchema,
  hp: int,
  max_hp: int,
  mp: int,
  max_mp: int,
});

export const itemViewSchema = v.object({
  item_id: id,
  template: v.string(),
  tab: tabSchema,
  slot: int,
  quantity: int,
});

export const partyMemberSchema = v.object({
  character_id: id,
  name: v.string(),
  level: int,
  hp: int,
  max_hp: int,
  map_id: v.string(),
  online: v.boolean(),
});

export const partyViewSchema = v.object({ leader: id, members: v.array(partyMemberSchema) });

// Entities on the map, shared by Snapshot and Delta. dead and invulnerable
// (backend step 19) are omitted by the server when false.
export const playerViewSchema = v.object({
  id,
  name: v.string(),
  class: v.string(),
  level: int,
  x: v.number(),
  y: v.number(),
  vx: v.number(),
  vy: v.number(),
  facing: v.picklist([-1, 1]),
  on_ground: v.boolean(),
  climbing: v.boolean(),
  hp: int,
  max_hp: int,
  dead: optionalBool,
  invulnerable: optionalBool,
});

export const mobViewSchema = v.object({
  id: int,
  type: v.string(),
  x: v.number(),
  y: v.number(),
  facing: v.picklist([-1, 1]),
  hp: int,
  max_hp: int,
});

export const dropViewSchema = v.object({
  id: int,
  template: v.string(),
  quantity: int,
  x: v.number(),
  y: v.number(),
});

export const inventorySlotSchema = v.object({
  tab: tabSchema,
  slot: int,
  item: v.optional(itemViewSchema), // absent when the slot emptied
});

const empty = v.object({});

/** The payload schema of every message, keyed by its wire type. */
export const schemas = {
  // Client to server
  Hello: v.object({ token: v.string(), protocol_version: int, character_id: id }),
  Input: v.object({
    left: optionalBool,
    right: optionalBool,
    up: optionalBool,
    down: optionalBool,
    jump: optionalBool,
  }),
  Attack: empty,
  PickUp: empty,
  EnterPortal: empty,
  ChatSend: v.object({ channel: chatChannelSchema, text: v.string() }),
  PartyInvite: v.object({ character_id: id }),
  PartyRespond: v.object({ invite_id: v.string(), accept: v.boolean() }),
  PartyLeave: empty,
  PartyKick: v.object({ character_id: id }),
  PartyPromote: v.object({ character_id: id }),
  InventoryMove: v.object({ idempotency_key: v.string(), item_id: id, tab: tabSchema, slot: int }),
  InventoryDiscard: v.object({ idempotency_key: v.string(), item_id: id, quantity: int }),
  Ping: v.object({ client_time: int }),

  // Server to client
  Welcome: v.object({
    token: v.string(),
    character: characterViewSchema,
    inventory: v.array(itemViewSchema),
    party: v.optional(partyViewSchema),
    map_id: v.string(),
    tick: int,
    server_time: int,
  }),
  Snapshot: v.object({
    players: v.array(playerViewSchema),
    mobs: v.array(mobViewSchema),
    drops: v.array(dropViewSchema),
  }),
  Delta: v.object({
    ack_seq: int,
    players: v.optional(v.array(playerViewSchema), []),
    mobs: v.optional(v.array(mobViewSchema), []),
    drops: v.optional(v.array(dropViewSchema), []),
    removed_players: v.optional(v.array(id), []),
    removed_mobs: v.optional(v.array(int), []),
    removed_drops: v.optional(v.array(int), []),
  }),
  Correction: v.object({
    seq: int,
    x: v.number(),
    y: v.number(),
    vx: v.number(),
    vy: v.number(),
    on_ground: v.boolean(),
    climbing: v.boolean(),
  }),
  Damage: v.object({
    source: v.string(), // p:<character id> or m:<mob id>
    target: v.string(),
    amount: int,
    crit: optionalBool,
    miss: optionalBool,
  }),
  Died: v.object({ character_id: id, x: v.number(), y: v.number() }),
  Respawned: v.object({
    character_id: id,
    map_id: v.string(),
    x: v.number(),
    y: v.number(),
    hp: int,
  }),
  ExpGained: v.object({ amount: int, exp: int }),
  LevelUp: v.object({ character: characterViewSchema }),
  InventoryChanged: v.object({ slots: v.array(inventorySlotSchema) }),
  MapChange: v.object({ map_id: v.string(), x: v.number(), y: v.number() }),
  ChatMessage: v.object({
    channel: chatChannelSchema,
    sender: id,
    sender_name: v.string(),
    text: v.string(),
  }),
  PartyState: v.object({ party: v.nullable(partyViewSchema) }),
  PartyInvited: v.object({ invite_id: v.string(), from: id, from_name: v.string() }),
  Response: v.object({
    ok: v.boolean(),
    code: v.optional(v.picklist(errorCodes)),
    message: v.optional(v.string()),
  }),
  Kicked: v.object({ reason: v.picklist(kickReasons) }),
  Pong: v.object({ client_time: int, server_time: int }),
} as const;

export type MessageType = keyof typeof schemas;

export const messageTypes = Object.keys(schemas) as MessageType[];

export function isMessageType(t: string): t is MessageType {
  return Object.hasOwn(schemas, t);
}

/** The decoded payload of one message type. */
export type Data<T extends MessageType> = v.InferOutput<(typeof schemas)[T]>;

/** One decoded message: the envelope plus its typed payload. */
export interface MessageOf<T extends MessageType> {
  readonly t: T;
  readonly req?: string;
  readonly seq?: number;
  readonly tick?: number;
  readonly d: Data<T>;
}

/** Every message, as a discriminated union on t. */
export type Message = { [T in MessageType]: MessageOf<T> }[MessageType];

export type PlayerView = v.InferOutput<typeof playerViewSchema>;
export type MobView = v.InferOutput<typeof mobViewSchema>;
export type DropView = v.InferOutput<typeof dropViewSchema>;
export type CharacterView = v.InferOutput<typeof characterViewSchema>;
export type ItemView = v.InferOutput<typeof itemViewSchema>;
export type InventorySlot = v.InferOutput<typeof inventorySlotSchema>;
export type PartyView = v.InferOutput<typeof partyViewSchema>;
export type PartyMember = v.InferOutput<typeof partyMemberSchema>;
export type Stats = v.InferOutput<typeof statsSchema>;
export type ChatChannel = v.InferOutput<typeof chatChannelSchema>;
export type Tab = v.InferOutput<typeof tabSchema>;
export type InputData = Data<'Input'>;
