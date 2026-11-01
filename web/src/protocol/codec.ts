import { fromBinary, fromJsonString, toBinary, toJsonString } from '@bufbuild/protobuf';

import { ClientMessageSchema, ServerMessageSchema } from './messages';
import type { ClientMessage, ServerBody, ServerMessage } from './messages';

/** A decoded server message always has a body. */
export type ServerFrame = ServerMessage & { readonly body: ServerBody };

/** The client's side of a codec; the server picks it by its WebSocket subprotocol. */
export interface Codec {
  readonly subprotocol: string;
  encode(msg: ClientMessage): string | Uint8Array<ArrayBuffer>;
  decode(raw: string | ArrayBuffer): ServerFrame;
}

export class ProtocolError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'ProtocolError';
  }
}

export const binaryCodec = {
  subprotocol: 'finga.v1.proto',
  encode: (msg) => toBinary(ClientMessageSchema, msg),
  decode: (raw) => {
    if (typeof raw === 'string') {
      throw new ProtocolError('a text frame on the binary codec');
    }
    return framed(() => fromBinary(ServerMessageSchema, new Uint8Array(raw)));
  },
} satisfies Codec;

/** protojson with the .proto field names, so frames read well in devtools. */
export const jsonCodec = {
  subprotocol: 'finga.v1.json',
  encode: (msg) => toJsonString(ClientMessageSchema, msg, { useProtoFieldName: true }),
  decode: (raw) => {
    const text = typeof raw === 'string' ? raw : new TextDecoder().decode(raw);
    return framed(() => fromJsonString(ServerMessageSchema, text, { ignoreUnknownFields: true }));
  },
} satisfies Codec;

function framed(decode: () => ServerMessage): ServerFrame {
  let msg: ServerMessage;
  try {
    msg = decode();
  } catch (err) {
    throw new ProtocolError(`bad frame: ${err instanceof Error ? err.message : String(err)}`);
  }
  if (!hasBody(msg)) {
    throw new ProtocolError('no body, or one this client does not know');
  }
  return msg;
}

function hasBody(msg: ServerMessage): msg is ServerFrame {
  return msg.body.case !== undefined;
}
