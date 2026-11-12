// Knows nothing about stores or Phaser; app/wiring connects them.
import { create } from '@bufbuild/protobuf';

import { backoff } from '@/lib/backoff';
import type { BackoffOptions } from '@/lib/backoff';
import { requestId } from '@/lib/id';
import { binaryCodec, ProtocolError } from '@/protocol/codec';
import type { Codec, ServerFrame } from '@/protocol/codec';
import { errorText, noAnswerText, refusalCode } from '@/protocol/errors';
import { ClientMessageSchema, KickReason } from '@/protocol/messages';
import type {
  ClientBodyInit,
  ClientInit,
  ClientMessageType,
  Data,
  MessageType,
} from '@/protocol/messages';
import type { Input } from '@/sim/input';

import { clearToken, getToken, setToken } from './token';

export const PROTOCOL_VERSION = 1;

export const CLOSE_NO_SUBPROTOCOL = 1008; // the server speaks none of the codecs asked for
export const CLOSE_SLOW_CONSUMER = 4001;
export const CLOSE_NO_PONG = 4002;
export const CLOSE_KICKED = 4003;
export const CLOSE_BAD_MESSAGE = 4004;

export type SocketStatus = 'idle' | 'connecting' | 'connected' | 'reconnecting' | 'closed';

/** Why the socket is closed for good. */
export type ClosedReason =
  | 'user'
  | 'kicked' // logged in elsewhere, or abuse
  | 'session_ended' // the token was cleared; log in again
  | 'outdated_protocol' // the page must reload, as for close 1008
  | 'bad_message' // 4004: a client bug a retry would repeat
  | 'no_token';

export interface StatusEvent {
  readonly status: SocketStatus;
  readonly reason?: ClosedReason;
  readonly kick?: KickReason;
  readonly code?: number;
  readonly attempt?: number;
}

export class RequestError extends Error {
  constructor(
    readonly code: number | 'timeout' | 'disconnected', // the server's refusal code, or why no answer came
    message: string,
  ) {
    super(message);
    this.name = 'RequestError';
  }
}

type RequestType =
  | 'inventoryMove'
  | 'inventoryDiscard'
  | 'equip'
  | 'unequip'
  | 'useItem'
  | 'partyInvite'
  | 'partyRespond'
  | 'partyLeave'
  | 'partyKick'
  | 'partyPromote'
  | 'spendPoints'
  | 'spendSkillPoints'
  | 'useSkill'
  | 'promote'
  | 'resetPoints'
  | 'talk'
  | 'acceptQuest'
  | 'abandonQuest'
  | 'handInQuest';
type ActionType = 'attack' | 'enterPortal';

/** A server message as handlers see it: the body's case and value, with the envelope's req and tick. */
export interface Received<T extends MessageType> {
  readonly t: T;
  readonly req: string; // '' unless it answers a request
  readonly tick: number;
  readonly d: Data<T>;
}

export interface SocketOptions {
  readonly url: string;
  /** Binary unless set; the JSON codec is for reading frames in devtools. */
  readonly codec?: Codec;
  /** Development only: a one-way delay on every frame, for latency checks. */
  readonly delayMs?: number;
}

const REQUEST_TIMEOUT_MS = 5000;
const HELLO_TIMEOUT_MS = 4000; // under the server's 5 s window
const PING_INTERVAL_MS = 15_000;
const PONG_TIMEOUT_MS = 5000;
const BACKOFF: BackoffOptions = { baseMs: 500, capMs: 4_000 }; // a restarted gateway is back within a few seconds

interface Pending {
  readonly t: RequestType;
  readonly d: ClientInit<RequestType>;
  readonly resolve: (d: Data<'response'>) => void;
  readonly reject: (err: RequestError) => void;
  timer: ReturnType<typeof setTimeout>;
  resent: boolean;
}

type Handler<T extends MessageType> = (msg: Received<T>) => void;

const OPEN = 1;

export class GameSocket {
  private readonly codec: Codec;
  private readonly delayMs: number;

  private ws: WebSocket | null = null;
  private status: SocketStatus = 'idle';
  private characterId: string | null = null;
  private closedByUser = false;
  private reconnecting = false;
  private attempt = 0;
  private pendingKick: KickReason | null = null;

  private seq = 0;
  private lastInputTick: number | null = null;
  private readonly pending = new Map<string, Pending>();
  private readonly handlers = new Map<MessageType, Set<Handler<MessageType>>>();
  private readonly statusHandlers = new Set<(e: StatusEvent) => void>();
  private readonly unmatchedHandlers = new Set<(d: Data<'response'>) => void>();

  private pingTimer: ReturnType<typeof setInterval> | null = null;
  private pongDeadline: ReturnType<typeof setTimeout> | null = null;

  rtt: number | null = null;
  /** Server time minus client time. */
  serverTimeOffset: number | null = null;

  constructor(private readonly opts: SocketOptions) {
    this.codec = opts.codec ?? binaryCodec;
    this.delayMs = opts.delayMs ?? 0;
  }

  /** The last Input sequence number sent. */
  get sequence(): number {
    return this.seq;
  }

  /** Resolves on Welcome. */
  connect(characterId: string): Promise<Data<'welcome'>> {
    if (getToken() === null) {
      this.setStatus({ status: 'closed', reason: 'no_token' });
      return Promise.reject(new Error('connect: no session token'));
    }
    this.characterId = characterId;
    this.closedByUser = false;
    this.attempt = 0;
    this.lastInputTick = null;
    return this.open();
  }

  close(): void {
    this.closedByUser = true;
    this.stopPing();
    const ws = this.ws;
    this.ws = null;
    if (ws?.readyState === OPEN) {
      ws.close(1000, 'bye');
    }
    this.failPending('disconnected', 'the connection was closed');
    this.setStatus({ status: 'closed', reason: 'user' });
  }

  on<T extends MessageType>(t: T, handler: Handler<T>): () => void {
    let set = this.handlers.get(t);
    if (!set) {
      set = new Set();
      this.handlers.set(t, set);
    }
    const h = handler as unknown as Handler<MessageType>; // the set for t only ever sees a t
    set.add(h);
    return () => {
      set.delete(h);
    };
  }

  onStatus(handler: (e: StatusEvent) => void): () => void {
    this.statusHandlers.add(handler);
    return () => {
      this.statusHandlers.delete(handler);
    };
  }

  /** A Response with no req: a refused attack, portal or chat line. */
  onUnmatchedResponse(handler: (d: Data<'response'>) => void): () => void {
    this.unmatchedHandlers.add(handler);
    return () => {
      this.unmatchedHandlers.delete(handler);
    };
  }

  /** A second call for the same tick is refused, so no caller can exceed the server's one Input per tick. */
  sendInput(input: Input, tick: number): boolean {
    if (!this.isOpen() || tick === this.lastInputTick) {
      return false;
    }
    this.lastInputTick = tick;
    this.seq++;
    this.transmit('input', input, { seq: this.seq });
    return true;
  }

  sendAction(t: ActionType): boolean {
    if (!this.isOpen()) {
      return false;
    }
    this.transmit(t, {}, { seq: this.seq });
    return true;
  }

  send<T extends 'chatSend' | 'ping'>(t: T, d: ClientInit<T>): boolean {
    if (!this.isOpen()) {
      return false;
    }
    this.transmit(t, d);
    return true;
  }

  /** Pending across a reconnect, it is re-sent once with a new req and the same payload, keeping its idempotency key. */
  request<T extends RequestType>(t: T, d: ClientInit<T>): Promise<Data<'response'>> {
    return new Promise((resolve, reject) => {
      const req = requestId();
      const pending: Pending = {
        t,
        d,
        resolve,
        reject,
        resent: false,
        timer: setTimeout(() => {
          this.pending.delete(req);
          console.info(`finga: ${t} got no response within ${REQUEST_TIMEOUT_MS} ms`);
          reject(new RequestError('timeout', noAnswerText.timeout));
        }, REQUEST_TIMEOUT_MS),
      };
      this.pending.set(req, pending);
      if (this.isOpen()) {
        this.transmit(t, d, { req });
      }
    });
  }

  private open(): Promise<Data<'welcome'>> {
    return new Promise((resolve, reject) => {
      const characterId = this.characterId;
      const token = getToken();
      if (characterId === null || token === null) {
        reject(new Error('connect: no character or token'));
        return;
      }
      this.setStatus({
        status: this.attempt > 0 ? 'reconnecting' : 'connecting',
        attempt: this.attempt,
      });
      const ws = new WebSocket(this.opts.url, [this.codec.subprotocol]);
      ws.binaryType = 'arraybuffer';
      this.ws = ws;
      this.pendingKick = null;
      let welcomed = false;
      const helloTimer = setTimeout(() => {
        if (!welcomed && this.ws === ws) {
          ws.close(1000, 'no welcome');
        }
      }, HELLO_TIMEOUT_MS);

      ws.onopen = () => {
        this.transmit('hello', { token, protocolVersion: PROTOCOL_VERSION, characterId });
      };
      const handle = (msg: ServerFrame) => {
        if (this.ws !== ws) {
          return; // delayed frame for a socket already replaced
        }
        if (msg.body.case === 'welcome' && !welcomed) {
          welcomed = true;
          clearTimeout(helloTimer);
          setToken(msg.body.value.token);
          this.attempt = 0;
          this.reconnecting = false;
          this.setStatus({ status: 'connected' });
          this.startPing();
          this.resendPending();
          this.dispatch(msg);
          resolve(msg.body.value);
          return;
        }
        this.receive(msg);
      };
      ws.onmessage = (event: MessageEvent<unknown>) => {
        const msg = this.decode(event.data);
        if (msg) {
          this.afterDelay(() => handle(msg));
        }
      };
      ws.onclose = (event: CloseEvent) => {
        clearTimeout(helloTimer);
        if (this.ws !== ws) {
          return;
        }
        // For a tester's console: 1006, never clean, is a drop with no close frame, which the gateway logs as end=dropped.
        console.info(
          `finga: the connection closed with ${event.code}${event.reason ? ` (${event.reason})` : ''}${event.wasClean ? '' : ', not clean'}`,
        );
        this.ws = null;
        this.stopPing();
        const kick = this.pendingKick;
        this.pendingKick = null;
        if (!welcomed) {
          reject(new Error(`connection closed before Welcome (${event.code})`));
        }
        this.afterClose(event.code, kick);
      };
      ws.onerror = () => {
        // onclose follows and decides.
      };
    });
  }

  private afterClose(code: number, kick: KickReason | null): void {
    if (this.closedByUser) {
      return;
    }
    if (code === CLOSE_BAD_MESSAGE) {
      this.failPending('disconnected', 'the server refused a message');
      this.setStatus({ status: 'closed', reason: 'bad_message', code });
      return;
    }
    if (code === CLOSE_NO_SUBPROTOCOL) {
      this.failPending('disconnected', 'the client is outdated');
      this.setStatus({ status: 'closed', reason: 'outdated_protocol', code });
      return;
    }
    if (code === CLOSE_KICKED) {
      switch (kick) {
        case KickReason.SERVER_RESTART:
          this.reconnect();
          return;
        case KickReason.UNAUTHENTICATED:
        case KickReason.SESSION_EXPIRED:
          clearToken();
          this.failPending('disconnected', 'the session ended');
          this.setStatus({ status: 'closed', reason: 'session_ended', kick, code });
          return;
        case KickReason.OUTDATED_PROTOCOL:
          this.failPending('disconnected', 'the client is outdated');
          this.setStatus({ status: 'closed', reason: 'outdated_protocol', kick, code });
          return;
        default: // logged in elsewhere, abuse, no Kicked, or a reason this client does not know
          this.failPending('disconnected', 'kicked');
          this.setStatus({
            status: 'closed',
            reason: 'kicked',
            ...(kick !== null && { kick }),
            code,
          });
          return;
      }
    }
    this.reconnect();
  }

  private reconnect(): void {
    if (this.reconnecting || this.closedByUser) {
      return;
    }
    this.reconnecting = true;
    const delay = backoff(this.attempt, BACKOFF);
    this.attempt++;
    this.setStatus({ status: 'reconnecting', attempt: this.attempt });
    setTimeout(() => {
      if (this.closedByUser) {
        this.reconnecting = false;
        return;
      }
      this.open().catch(() => {
        this.reconnecting = false;
        if (!this.closedByUser && this.status !== 'closed') {
          this.reconnect();
        }
      });
    }, delay);
  }

  private resendPending(): void {
    for (const [req, p] of [...this.pending]) {
      this.pending.delete(req);
      if (p.resent) {
        clearTimeout(p.timer);
        console.info(`finga: ${p.t} lost its answer: the connection dropped twice`);
        p.reject(new RequestError('disconnected', noAnswerText.disconnected));
        continue;
      }
      p.resent = true;
      const again = requestId();
      this.pending.set(again, p);
      this.transmit(p.t, p.d, { req: again });
    }
  }

  private failPending(code: 'disconnected', why: string): void {
    for (const [req, p] of this.pending) {
      clearTimeout(p.timer);
      console.info(`finga: ${p.t} lost its answer: ${why}`);
      p.reject(new RequestError(code, noAnswerText[code]));
      this.pending.delete(req);
    }
  }

  private decode(raw: unknown): ServerFrame | null {
    try {
      if (typeof raw !== 'string' && !(raw instanceof ArrayBuffer)) {
        throw new ProtocolError('frame is neither text nor binary');
      }
      return this.codec.decode(raw);
    } catch (err) {
      console.info(
        `finga: dropped a frame this client cannot read: ${err instanceof ProtocolError ? err.message : String(err)}`,
      );
      return null;
    }
  }

  private receive(msg: ServerFrame): void {
    const { body } = msg;
    switch (body.case) {
      case 'response': {
        const p = this.pending.get(msg.req); // none for an empty req
        const r = body.value;
        if (p) {
          this.pending.delete(msg.req);
          clearTimeout(p.timer);
          if (r.ok) {
            p.resolve(r);
          } else {
            const code = refusalCode(r.code);
            p.reject(new RequestError(code, errorText(code)));
          }
        } else {
          for (const h of this.unmatchedHandlers) {
            h(r);
          }
        }
        break;
      }
      case 'pong': {
        const now = Date.now();
        this.rtt = now - Number(body.value.clientTime);
        this.serverTimeOffset = Number(body.value.serverTime) - (now - this.rtt / 2);
        if (this.pongDeadline) {
          clearTimeout(this.pongDeadline);
          this.pongDeadline = null;
        }
        break;
      }
      case 'kicked':
        this.pendingKick = body.value.reason;
        break;
      default:
        break;
    }
    this.dispatch(msg);
  }

  private dispatch(msg: ServerFrame): void {
    const set = this.handlers.get(msg.body.case);
    if (set) {
      const received = { t: msg.body.case, req: msg.req, tick: msg.tick, d: msg.body.value };
      for (const h of set) {
        h(received);
      }
    }
  }

  private transmit<T extends ClientMessageType>(
    t: T,
    d: ClientInit<T>,
    envelope: { readonly seq?: number; readonly req?: string } = {},
  ): void {
    const ws = this.ws;
    if (ws?.readyState === OPEN) {
      const body = { case: t, value: d } as ClientBodyInit;
      const frame = this.codec.encode(create(ClientMessageSchema, { ...envelope, body }));
      this.afterDelay(() => {
        if (ws.readyState === OPEN) {
          ws.send(frame);
        }
      });
    }
  }

  private afterDelay(fn: () => void): void {
    if (this.delayMs > 0) {
      setTimeout(fn, this.delayMs);
    } else {
      fn();
    }
  }

  private isOpen(): boolean {
    return this.status === 'connected' && this.ws !== null && this.ws.readyState === OPEN;
  }

  private setStatus(e: StatusEvent): void {
    this.status = e.status;
    for (const h of this.statusHandlers) {
      h(e);
    }
  }

  private startPing(): void {
    this.stopPing();
    this.pingTimer = setInterval(() => {
      if (!this.isOpen()) {
        return;
      }
      this.send('ping', { clientTime: BigInt(Date.now()) });
      this.pongDeadline ??= setTimeout(() => {
        this.pongDeadline = null;
        // onclose reconnects.
        const ws = this.ws;
        if (ws) {
          ws.close(1000, 'no pong');
        }
      }, PONG_TIMEOUT_MS);
    }, PING_INTERVAL_MS);
  }

  private stopPing(): void {
    if (this.pingTimer) {
      clearInterval(this.pingTimer);
      this.pingTimer = null;
    }
    if (this.pongDeadline) {
      clearTimeout(this.pongDeadline);
      this.pongDeadline = null;
    }
  }
}
