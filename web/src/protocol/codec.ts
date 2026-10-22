// The codec turns wire frames into typed messages and back. JSON in Stage 1;
// Stage 2 adds a Protobuf codec behind the same interface. Decoding checks
// the envelope and the payload shape and rejects unknown types. Encoding
// follows the server's JSON rules (Go's omitempty), so a decoded golden
// message re-encodes to exactly the file it came from.
import * as v from 'valibot';

import { isMessageType, schemas } from './messages';
import type { Data, Message, MessageType } from './messages';

export interface Codec {
  encode(msg: Message): string | ArrayBuffer;
  decode(raw: string | ArrayBuffer): Message;
}

/** A frame that is not a well-formed message. */
export class ProtocolError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'ProtocolError';
  }
}

const int = v.pipe(v.number(), v.integer());
const envelopeSchema = v.object({
  t: v.string(),
  req: v.optional(v.string()),
  seq: v.optional(int),
  tick: v.optional(int),
  d: v.optional(v.unknown()),
});

export class JsonCodec implements Codec {
  decode(raw: string | ArrayBuffer): Message {
    const text = typeof raw === 'string' ? raw : new TextDecoder().decode(raw);
    let parsed: unknown;
    try {
      parsed = JSON.parse(text);
    } catch {
      throw new ProtocolError('frame is not JSON');
    }
    const env = v.safeParse(envelopeSchema, parsed);
    if (!env.success) {
      throw new ProtocolError(`bad envelope: ${describe(env.issues)}`);
    }
    const { t, req, seq, tick, d } = env.output;
    if (!isMessageType(t)) {
      throw new ProtocolError(`unknown message type ${JSON.stringify(t)}`);
    }
    if (d === undefined) {
      throw new ProtocolError(`${t}: missing d`);
    }
    const data = v.safeParse(schemas[t], d);
    if (!data.success) {
      throw new ProtocolError(`${t}: ${describe(data.issues)}`);
    }
    return {
      t,
      ...(req !== undefined && { req }),
      ...(seq !== undefined && { seq }),
      ...(tick !== undefined && { tick }),
      d: data.output,
    } as Message;
  }

  encode(msg: Message): string {
    const out: Record<string, unknown> = { t: msg.t };
    if (msg.req !== undefined) {
      out.req = msg.req;
    }
    if (msg.seq !== undefined) {
      out.seq = msg.seq;
    }
    if (msg.tick !== undefined) {
      out.tick = msg.tick;
    }
    out.d = omitEmpty(msg.t, msg.d);
    return JSON.stringify(out);
  }
}

function describe(issues: readonly v.BaseIssue<unknown>[]): string {
  return issues
    .map((issue) => {
      const path = issue.path?.map((p) => String(p.key)).join('.') ?? '';
      return path ? `${path}: ${issue.message}` : issue.message;
    })
    .join('; ');
}

type Plain = Record<string, unknown>;

// omitEmpty applies the server's omitempty tags: a false bool, an empty
// list or an absent object tagged omitempty is left out of the JSON.
function omitEmpty(t: MessageType, d: Data<MessageType>): unknown {
  switch (t) {
    case 'Input':
      return dropFalse(d as Data<'Input'>, ['left', 'right', 'up', 'down', 'jump']);
    case 'Damage':
      return dropFalse(d as Data<'Damage'>, ['crit', 'miss']);
    case 'Snapshot': {
      const snap = d as Data<'Snapshot'>;
      return { ...snap, players: snap.players.map(encodePlayer) };
    }
    case 'Delta': {
      const delta = d as Data<'Delta'>;
      const out: Plain = { ack_seq: delta.ack_seq };
      const lists = {
        players: delta.players.map(encodePlayer),
        mobs: delta.mobs,
        drops: delta.drops,
        removed_players: delta.removed_players,
        removed_mobs: delta.removed_mobs,
        removed_drops: delta.removed_drops,
      };
      for (const [key, list] of Object.entries(lists)) {
        if (list.length > 0) {
          out[key] = list;
        }
      }
      return out;
    }
    case 'Response': {
      const r = d as Data<'Response'>;
      const out: Plain = { ok: r.ok };
      if (r.code !== undefined) {
        out.code = r.code;
      }
      if (r.message !== undefined && r.message !== '') {
        out.message = r.message;
      }
      return out;
    }
    default:
      return d; // undefined fields (Welcome.party, a slot's item) vanish in JSON.stringify
  }
}

function encodePlayer(p: Data<'Snapshot'>['players'][number]): Plain {
  return dropFalse(p, ['dead', 'invulnerable']);
}

function dropFalse<T extends object>(value: T, keys: readonly (keyof T & string)[]): Plain {
  const out: Plain = { ...(value as unknown as Plain) };
  for (const key of keys) {
    if (out[key] === false) {
      delete out[key];
    }
  }
  return out;
}
