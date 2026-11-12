import { create, fromBinary, fromJsonString, toBinary, toJsonString } from '@bufbuild/protobuf';
import type { MessageInitShape } from '@bufbuild/protobuf';

import { jsonCodec } from '@/protocol/codec';
import { ClientMessageSchema, ServerMessageSchema } from '@/protocol/messages';
import type { ClientBody, ClientMessageType, MessageType } from '@/protocol/messages';

type ServerBodyInit = Extract<
  MessageInitShape<typeof ServerMessageSchema>['body'],
  { case: MessageType }
>;
/** A server message as a test writes it: any field left out is its default. */
export type ServerInit<T extends MessageType> = Extract<ServerBodyInit, { case: T }>['value'];

/** A sent ClientMessage, flattened like the socket's Received. */
export interface SentFrame {
  readonly t: ClientMessageType | undefined;
  readonly seq: number;
  readonly req: string;
  readonly d: ClientBody['value'] | undefined;
}

export class FakeWebSocket {
  static instances: FakeWebSocket[] = [];

  static reset(): void {
    FakeWebSocket.instances = [];
  }

  static latest(): FakeWebSocket {
    const ws = FakeWebSocket.instances.at(-1);
    if (!ws) {
      throw new Error('no FakeWebSocket was opened');
    }
    return ws;
  }

  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSING = 2;
  static readonly CLOSED = 3;

  readyState = FakeWebSocket.CONNECTING;
  binaryType: BinaryType = 'blob';
  readonly sent: (string | Uint8Array)[] = [];
  closedWith: { code: number; reason: string } | null = null;

  onopen: ((ev: Event) => void) | null = null;
  onmessage: ((ev: MessageEvent<unknown>) => void) | null = null;
  onclose: ((ev: CloseEvent) => void) | null = null;
  onerror: ((ev: Event) => void) | null = null;

  constructor(
    readonly url: string,
    readonly protocols: string[] = [],
  ) {
    FakeWebSocket.instances.push(this);
  }

  /** The server takes the first subprotocol asked for. */
  get protocol(): string {
    return this.protocols[0] ?? '';
  }

  send(data: string | Uint8Array): void {
    if (this.readyState !== FakeWebSocket.OPEN) {
      throw new Error('send on a socket that is not open');
    }
    this.sent.push(data);
  }

  close(code = 1000, reason = ''): void {
    if (this.readyState === FakeWebSocket.CLOSED) {
      return;
    }
    this.closedWith = { code, reason };
    this.readyState = FakeWebSocket.CLOSED;
    this.onclose?.({ code, reason, wasClean: true } as CloseEvent);
  }

  open(): void {
    this.readyState = FakeWebSocket.OPEN;
    this.onopen?.(new Event('open'));
  }

  /** Sends a server message in the subprotocol's codec. */
  push<T extends MessageType>(
    t: T,
    d: ServerInit<T>,
    envelope: { readonly req?: string; readonly tick?: number } = {},
  ): void {
    const body = { case: t, value: d } as ServerBodyInit;
    const msg = create(ServerMessageSchema, { ...envelope, body });
    this.pushRaw(
      this.protocol === jsonCodec.subprotocol
        ? toJsonString(ServerMessageSchema, msg, { useProtoFieldName: true })
        : toBinary(ServerMessageSchema, msg).buffer,
    );
  }

  /** Sends a frame as it is, such as one no codec would write. */
  pushRaw(data: unknown): void {
    this.onmessage?.({ data } as MessageEvent<unknown>);
  }

  closeFromServer(code: number, reason = ''): void {
    this.readyState = FakeWebSocket.CLOSED;
    this.onclose?.({ code, reason, wasClean: code !== 1006 } as CloseEvent); // a browser's 1006 is a drop, never clean
  }

  frames(): SentFrame[] {
    return this.sent.map((raw) => {
      const msg =
        typeof raw === 'string'
          ? fromJsonString(ClientMessageSchema, raw)
          : fromBinary(ClientMessageSchema, raw);
      return { t: msg.body.case, seq: msg.seq, req: msg.req, d: msg.body.value };
    });
  }
}
