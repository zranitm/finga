import { readdirSync, readFileSync } from 'node:fs';
import path from 'node:path';

import { create, fromBinary, fromJsonString, toBinary, toJson } from '@bufbuild/protobuf';
import type { DescMessage } from '@bufbuild/protobuf';
import { BinaryWriter, WireType } from '@bufbuild/protobuf/wire';
import { describe, expect, it } from 'vitest';

import { binaryCodec, jsonCodec, ProtocolError } from './codec';
import { ClientMessageSchema, DeltaSchema, ServerMessageSchema } from './messages';

// npm runs the scripts from web/, so the server's golden files are in ../server.
const contracts = path.resolve(process.cwd(), '..', 'server', 'testdata', 'contracts');
const goldens = readdirSync(contracts)
  .filter((file) => file.endsWith('.json'))
  .map((file) => ({
    name: file.replace(/\.json$/, ''),
    json: readFileSync(path.join(contracts, file), 'utf8'),
  }));

/** An envelope's body cases by message name, such as InventoryChanged: inventoryChanged. */
function bodyCases(envelope: DescMessage): Map<string, string> {
  const body = envelope.oneofs.find((o) => o.name === 'body');
  return new Map(
    body?.fields.flatMap((f) => (f.message ? [[f.message.name, f.localName] as const] : [])),
  );
}

const clientCases = bodyCases(ClientMessageSchema);
const serverCases = bodyCases(ServerMessageSchema);
const clientGoldens = goldens.filter((g) => clientCases.has(g.name));
const serverGoldens = goldens.filter((g) => serverCases.has(g.name));

describe('golden messages', () => {
  it('are one per body case of the two envelopes, and no more', () => {
    expect(goldens.map((g) => g.name).sort()).toEqual(
      [...clientCases.keys(), ...serverCases.keys()].sort(),
    );
    expect([clientCases.size, serverCases.size]).toEqual([27, 26]);
  });

  it.each(serverGoldens)('$name decodes as its ServerMessage and re-encodes the same', (g) => {
    const msg = jsonCodec.decode(g.json);
    expect(msg.body.case).toBe(serverCases.get(g.name));
    expect(toJson(ServerMessageSchema, msg, { useProtoFieldName: true })).toEqual(
      JSON.parse(g.json),
    );
  });

  it.each(clientGoldens)('$name decodes as its ClientMessage and encodes the same', (g) => {
    const msg = fromJsonString(ClientMessageSchema, g.json);
    expect(msg.body.case).toBe(clientCases.get(g.name));
    expect(JSON.parse(jsonCodec.encode(msg))).toEqual(JSON.parse(g.json));
  });

  it.each(clientGoldens)('$name is no message to the client', (g) => {
    expect(() => jsonCodec.decode(g.json)).toThrow(/no body/);
  });

  it.each(serverGoldens)('$name survives a binary round trip', (g) => {
    const msg = fromJsonString(ServerMessageSchema, g.json);
    expect(binaryCodec.decode(toBinary(ServerMessageSchema, msg).buffer)).toEqual(msg);
  });

  it.each(clientGoldens)('$name survives a binary round trip', (g) => {
    const msg = fromJsonString(ClientMessageSchema, g.json);
    expect(fromBinary(ClientMessageSchema, binaryCodec.encode(msg))).toEqual(msg);
  });

  it('a StatsChanged without a sheet decodes with none, in JSON and binary, so the store keeps the last', () => {
    const json = '{"tick":9,"stats_changed":{"hp":50,"max_hp":135,"mp":11,"max_mp":33}}';
    const msg = jsonCodec.decode(json);
    expect(msg.body.case).toBe('statsChanged');
    expect(msg.body.case === 'statsChanged' && msg.body.value.sheet).toBeUndefined();
    expect(binaryCodec.decode(toBinary(ServerMessageSchema, msg).buffer)).toEqual(msg);
  });

  it('a binary frame on the JSON codec decodes as UTF-8 text', () => {
    const pong = goldens.find((g) => g.name === 'Pong');
    if (!pong) throw new Error('no Pong golden');
    const bytes = new TextEncoder().encode(pong.json);
    expect(jsonCodec.decode(bytes.buffer)).toEqual(jsonCodec.decode(pong.json));
  });
});

// The gateway encodes a tick's Delta once and appends each player's ack after it: protobuf reads that as one message.
describe('a Delta the gateway spliced', () => {
  it('decodes as one Delta with the shared fields and the ack', () => {
    const shared = toBinary(
      DeltaSchema,
      create(DeltaSchema, { players: [{ id: 'p-1', x: 3 }], removedMobs: [6] }),
    );
    const own = toBinary(DeltaSchema, create(DeltaSchema, { ackSeq: 7 }));
    const field = (name: string) => {
      const f = ServerMessageSchema.fields.find((x) => x.localName === name);
      if (!f) throw new Error(`no ${name} field`);
      return f.number;
    };
    const writer = new BinaryWriter();
    writer.tag(field('tick'), WireType.Varint).uint64(81235);
    writer.tag(field('delta'), WireType.LengthDelimited).bytes(new Uint8Array([...shared, ...own]));
    const msg = binaryCodec.decode(writer.finish().slice().buffer);
    expect(msg.tick).toBe(81235);
    expect(msg.body).toEqual({
      case: 'delta',
      value: create(DeltaSchema, { ackSeq: 7, players: [{ id: 'p-1', x: 3 }], removedMobs: [6] }),
    });
  });
});

describe('decode rejects', () => {
  it.each([
    { name: 'text that is not JSON', codec: jsonCodec, raw: '{"pong":', want: /bad frame/ },
    { name: 'JSON that is not an object', codec: jsonCodec, raw: '[1,2]', want: /bad frame/ },
    {
      name: 'a field of the wrong type',
      codec: jsonCodec,
      raw: '{"pong":{"client_time":"now"}}',
      want: /bad frame/,
    },
    { name: 'an envelope with no body', codec: jsonCodec, raw: '{"tick":5}', want: /no body/ },
    {
      name: 'a message from a newer server',
      codec: jsonCodec,
      raw: '{"trade":{}}',
      want: /no body/,
    },
    {
      name: 'bytes that are no ServerMessage',
      codec: binaryCodec,
      raw: new Uint8Array([0xff, 0xff, 0xff]).buffer,
      want: /bad frame/,
    },
    {
      name: 'a binary message from a newer server (field 99)',
      codec: binaryCodec,
      raw: new Uint8Array([0x9a, 0x06, 0x00]).buffer,
      want: /no body/,
    },
    { name: 'a text frame on the binary codec', codec: binaryCodec, raw: '{}', want: /text frame/ },
  ])('$name', ({ codec, raw, want }) => {
    expect(() => codec.decode(raw)).toThrow(ProtocolError);
    expect(() => codec.decode(raw)).toThrow(want);
  });
});

describe('proto3 defaults', () => {
  it('a Delta with nothing in its lists has them empty, and no req is an empty one', () => {
    const msg = jsonCodec.decode('{"tick":5,"delta":{"ack_seq":3}}');
    expect(msg).toMatchObject({
      req: '',
      tick: 5,
      body: {
        case: 'delta',
        value: { ackSeq: 3, players: [], removedPlayers: [], removedMobs: [], removedDrops: [] },
      },
    });
  });

  it('an emptied slot has no item, and no party is undefined, not null', () => {
    const changed = jsonCodec.decode('{"inventory_changed":{"slots":[{"tab":"etc","slot":2}]}}');
    if (changed.body.case !== 'inventoryChanged') throw new Error(changed.body.case);
    expect(changed.body.value.slots[0]?.item).toBeUndefined();
    const state = jsonCodec.decode('{"party_state":{}}');
    if (state.body.case !== 'partyState') throw new Error(state.body.case);
    expect(state.body.value.party).toBeUndefined();
  });

  it('64-bit EXP and times are bigints', () => {
    const msg = jsonCodec.decode('{"exp_gained":{"amount":"-3","exp":"9007199254740993"}}');
    expect(msg.body.value).toMatchObject({ amount: -3n, exp: 9007199254740993n });
  });

  it('an Input with no keys held encodes as an empty object, binary or JSON', () => {
    const msg = create(ClientMessageSchema, { seq: 9, body: { case: 'input', value: {} } });
    expect(jsonCodec.encode(msg)).toBe('{"seq":9,"input":{}}');
    expect([...binaryCodec.encode(msg)]).toEqual([0x08, 9, 0x1a, 0]);
  });
});
