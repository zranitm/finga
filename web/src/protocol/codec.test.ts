import { readdirSync, readFileSync } from 'node:fs';
import path from 'node:path';

import { describe, expect, it } from 'vitest';

import { JsonCodec, ProtocolError } from './codec';
import { messageTypes } from './messages';
import type { Message, MessageType } from './messages';

// npm runs the scripts from web/, so the server's golden files are in ../server.
const contracts = path.resolve(process.cwd(), '..', 'server', 'testdata', 'contracts');
const goldens = readdirSync(contracts)
  .filter((name) => name.endsWith('.json'))
  .map((name) => ({ name, json: readFileSync(path.join(contracts, name), 'utf8') }));

const codec = new JsonCodec();

describe('golden messages', () => {
  it('cover every message type, and every message type has a golden', () => {
    const typed = new Set<string>(messageTypes);
    const files = new Set(goldens.map((g) => g.name.replace(/\.json$/, '')));
    expect([...files].sort()).toEqual([...typed].sort());
    expect(goldens).toHaveLength(31);
  });

  // Discovered at test time: a new golden file fails here until it is typed.
  it.each(goldens)('$name decodes and re-encodes to the same JSON', ({ name, json }) => {
    const msg = codec.decode(json);
    expect(msg.t).toBe(name.replace(/\.json$/, ''));
    expect(JSON.parse(codec.encode(msg))).toEqual(JSON.parse(json));
  });

  it('decodes binary frames as UTF-8 too', () => {
    const golden = goldens.find((g) => g.name === 'Pong.json');
    if (!golden) throw new Error('no Pong golden');
    const bytes = new TextEncoder().encode(golden.json);
    expect(codec.decode(bytes.buffer)).toEqual(codec.decode(golden.json));
  });
});

describe('decode rejects', () => {
  it.each([
    { name: 'an unknown t', raw: '{"t":"MarketBuy","d":{}}', want: /unknown message type/ },
    { name: 'a missing d', raw: '{"t":"Attack"}', want: /missing d/ },
    {
      name: 'a req that is not a string',
      raw: '{"t":"PartyLeave","req":7,"d":{}}',
      want: /bad envelope/,
    },
    {
      name: 'a seq that is not an integer',
      raw: '{"t":"Attack","seq":1.5,"d":{}}',
      want: /bad envelope/,
    },
    {
      name: 'a payload of the wrong shape',
      raw: '{"t":"Ping","d":{"client_time":"now"}}',
      want: /Ping: client_time/,
    },
    {
      name: 'an unknown error code',
      raw: '{"t":"Response","d":{"ok":false,"code":"teapot"}}',
      want: /Response: code/,
    },
    {
      name: 'an unknown kick reason',
      raw: '{"t":"Kicked","d":{"reason":"bored"}}',
      want: /Kicked: reason/,
    },
    {
      name: 'a facing that is not left or right',
      raw: '{"t":"Snapshot","d":{"players":[],"mobs":[{"id":1,"type":"x","x":0,"y":0,"facing":0,"hp":1,"max_hp":1}],"drops":[]}}',
      want: /facing/,
    },
    { name: 'not JSON at all', raw: '{"t":', want: /not JSON/ },
    { name: 'JSON that is not an object', raw: '[1,2]', want: /bad envelope/ },
  ])('$name', ({ raw, want }) => {
    expect(() => codec.decode(raw)).toThrow(ProtocolError);
    expect(() => codec.decode(raw)).toThrow(want);
  });
});

describe('decode defaults', () => {
  it('a Delta with no removed lists decodes them as empty arrays, never undefined', () => {
    const msg = codec.decode('{"t":"Delta","tick":5,"d":{"ack_seq":3}}');
    if (msg.t !== 'Delta') throw new Error(msg.t);
    expect(msg.d).toEqual({
      ack_seq: 3,
      players: [],
      mobs: [],
      drops: [],
      removed_players: [],
      removed_mobs: [],
      removed_drops: [],
    });
    expect(msg.tick).toBe(5);
    expect(msg.req).toBeUndefined();
    expect(JSON.parse(codec.encode(msg))).toEqual({ t: 'Delta', tick: 5, d: { ack_seq: 3 } });
  });

  it('a Response with no req decodes (a failed pickup, a refused chat line)', () => {
    const msg = codec.decode(
      '{"t":"Response","d":{"ok":false,"code":"rate_limited","message":"slow down"}}',
    );
    if (msg.t !== 'Response') throw new Error(msg.t);
    expect(msg.req).toBeUndefined();
    expect(msg.d).toEqual({ ok: false, code: 'rate_limited', message: 'slow down' });
    expect(JSON.parse(codec.encode({ t: 'Response', req: 'r-1', d: { ok: true } }))).toEqual({
      t: 'Response',
      req: 'r-1',
      d: { ok: true },
    });
  });

  it('a player without dead or invulnerable decodes them as false, and false is omitted again', () => {
    const player =
      '{"id":"p1","name":"Ayla","class":"warrior","level":1,"x":0,"y":0,"vx":0,"vy":0,"facing":1,"on_ground":true,"climbing":false,"hp":1,"max_hp":1}';
    const msg = codec.decode(`{"t":"Delta","d":{"ack_seq":0,"players":[${player}]}}`);
    if (msg.t !== 'Delta') throw new Error(msg.t);
    expect(msg.d.players[0]?.dead).toBe(false);
    expect(msg.d.players[0]?.invulnerable).toBe(false);
    expect(JSON.parse(codec.encode(msg))).toEqual({
      t: 'Delta',
      d: { ack_seq: 0, players: [JSON.parse(player)] },
    });
    const dead = codec.decode(
      `{"t":"Delta","d":{"ack_seq":0,"players":[${player.replace('"hp":1', '"hp":0,"dead":true,"invulnerable":true')}]}}`,
    );
    if (dead.t !== 'Delta') throw new Error(dead.t);
    expect(dead.d.players[0]).toMatchObject({ dead: true, invulnerable: true });
  });

  it('an Input with no keys decodes every key as false and encodes as an empty object', () => {
    const msg = codec.decode('{"t":"Input","seq":9,"d":{}}');
    if (msg.t !== 'Input') throw new Error(msg.t);
    expect(msg.d).toEqual({ left: false, right: false, up: false, down: false, jump: false });
    expect(codec.encode(msg)).toBe('{"t":"Input","seq":9,"d":{}}');
  });

  it('an emptied slot has no item; an absent party is left out', () => {
    const msg = codec.decode('{"t":"InventoryChanged","d":{"slots":[{"tab":"etc","slot":2}]}}');
    if (msg.t !== 'InventoryChanged') throw new Error(msg.t);
    expect(msg.d.slots[0]?.item).toBeUndefined();
    const state = codec.decode('{"t":"PartyState","d":{"party":null}}');
    if (state.t !== 'PartyState') throw new Error(state.t);
    expect(state.d.party).toBeNull();
    expect(JSON.parse(codec.encode(state))).toEqual({ t: 'PartyState', d: { party: null } });
  });
});

// The union is exhaustive: a switch on t with never in the default compiles.
function family(msg: Message): 'client' | 'server' {
  switch (msg.t) {
    case 'Hello':
    case 'Input':
    case 'Attack':
    case 'PickUp':
    case 'EnterPortal':
    case 'ChatSend':
    case 'PartyInvite':
    case 'PartyRespond':
    case 'PartyLeave':
    case 'PartyKick':
    case 'PartyPromote':
    case 'InventoryMove':
    case 'InventoryDiscard':
    case 'Ping':
      return 'client';
    case 'Welcome':
    case 'Snapshot':
    case 'Delta':
    case 'Correction':
    case 'Damage':
    case 'Died':
    case 'Respawned':
    case 'ExpGained':
    case 'LevelUp':
    case 'InventoryChanged':
    case 'MapChange':
    case 'ChatMessage':
    case 'PartyState':
    case 'PartyInvited':
    case 'Response':
    case 'Kicked':
    case 'Pong':
      return 'server';
    default: {
      const never: never = msg;
      throw new Error(`unhandled message ${String(never)}`);
    }
  }
}

describe('the message union', () => {
  it('sorts every golden into client or server messages', () => {
    const counts = { client: 0, server: 0 };
    for (const { json } of goldens) {
      counts[family(codec.decode(json))]++;
    }
    expect(counts).toEqual({ client: 14, server: 17 });
    const types: MessageType[] = messageTypes;
    expect(types).toHaveLength(31);
  });
});
