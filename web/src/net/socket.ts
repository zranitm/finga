// The game socket (page 7, Sending and Connection loss; page 4, WebSocket):
// Hello on open, typed subscriptions, requests matched to their Response by
// req, one Input per tick, a Ping every 15 s, reconnect with backoff after
// a drop, and the server's close codes and Kicked reasons as typed events.
// It knows nothing about stores or Phaser; app/wiring connects them.
import { backoff } from '@/lib/backoff';
import type { BackoffOptions } from '@/lib/backoff';
import { requestId } from '@/lib/id';
import { JsonCodec, ProtocolError } from '@/protocol/codec';
import type { Codec } from '@/protocol/codec';
import type { ErrorCode, KickReason } from '@/protocol/errors';
import type { Data, Message, MessageOf, MessageType } from '@/protocol/messages';
import type { Input } from '@/sim/input';

import { clearToken, getToken, setToken } from './token';

export const PROTOCOL_VERSION = 1;

/** The server's close codes (page 4). */
export const CLOSE_SLOW_CONSUMER = 4001;
export const CLOSE_NO_PONG = 4002;
export const CLOSE_KICKED = 4003;
export const CLOSE_BAD_MESSAGE = 4004;

export type SocketStatus = 'idle' | 'connecting' | 'connected' | 'reconnecting' | 'closed';

/** Why the socket is closed for good. */
export type ClosedReason =
  | 'user' // close() was called
  | 'kicked' // a Kicked reason the player must see: logged_in_elsewhere, abuse
  | 'session_ended' // unauthenticated or session_expired: the token was cleared, log in again
  | 'outdated_protocol' // the page must reload
  | 'bad_message' // 4004: a client bug a retry would repeat
  | 'no_token'; // connect was called without a session token

export interface StatusEvent {
  readonly status: SocketStatus;
  readonly reason?: ClosedReason;
  readonly kick?: KickReason;
  readonly code?: number;
  readonly attempt?: number; // reconnect attempts so far
}

/** A request the server refused, or one that got no answer. */
export class RequestError extends Error {
  constructor(
    readonly code: ErrorCode | 'timeout' | 'disconnected',
    message: string,
  ) {
    super(message);
    this.name = 'RequestError';
  }
}

type RequestType =
  | 'InventoryMove'
  | 'InventoryDiscard'
  | 'PartyInvite'
  | 'PartyRespond'
  | 'PartyLeave'
  | 'PartyKick'
  | 'PartyPromote';
type ActionType = 'Attack' | 'PickUp' | 'EnterPortal';

export interface SocketOptions {
  /** ws(s)://host/ws */
  readonly url: string;
  readonly codec?: Codec;
  readonly WebSocket?: new (url: string) => WebSocket;
  readonly now?: () => number;
  readonly requestTimeoutMs?: number;
  readonly helloTimeoutMs?: number; // under the server's 5 s window
  readonly pingIntervalMs?: number;
  readonly pongTimeoutMs?: number;
  readonly backoff?: BackoffOptions;
  /** Development only: a one-way delay on every frame sent and received, for latency checks. */
  readonly delayMs?: number;
}

interface Pending {
  readonly t: RequestType;
  readonly d: Data<RequestType>;
  readonly resolve: (d: Data<'Response'>) => void;
  readonly reject: (err: RequestError) => void;
  timer: ReturnType<typeof setTimeout>;
  resent: boolean;
}

type Handler<T extends MessageType> = (msg: MessageOf<T>) => void;

const OPEN = 1;

export class GameSocket {
  private readonly codec: Codec;
  private readonly WebSocketImpl: new (url: string) => WebSocket;
  private readonly now: () => number;
  private readonly requestTimeoutMs: number;
  private readonly helloTimeoutMs: number;
  private readonly pingIntervalMs: number;
  private readonly pongTimeoutMs: number;
  private readonly backoffOptions: BackoffOptions;
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
  private readonly unmatchedHandlers = new Set<(d: Data<'Response'>) => void>();
  private readonly errorHandlers = new Set<(err: ProtocolError) => void>();

  private pingTimer: ReturnType<typeof setInterval> | null = null;
  private pongDeadline: ReturnType<typeof setTimeout> | null = null;

  /** Round-trip time from the last Pong, in milliseconds. */
  rtt: number | null = null;
  /** server time minus client time, from the last Pong. */
  serverTimeOffset: number | null = null;

  constructor(private readonly opts: SocketOptions) {
    this.codec = opts.codec ?? new JsonCodec();
    this.WebSocketImpl = opts.WebSocket ?? WebSocket;
    this.now = opts.now ?? (() => Date.now());
    this.requestTimeoutMs = opts.requestTimeoutMs ?? 5000;
    this.helloTimeoutMs = opts.helloTimeoutMs ?? 4000;
    this.pingIntervalMs = opts.pingIntervalMs ?? 15_000;
    this.pongTimeoutMs = opts.pongTimeoutMs ?? 5000;
    this.backoffOptions = opts.backoff ?? { baseMs: 500, capMs: 10_000 };
    this.delayMs = opts.delayMs ?? 0;
  }

  get currentStatus(): SocketStatus {
    return this.status;
  }

  /** The sequence number the next Input will carry minus one: the last one sent. */
  get sequence(): number {
    return this.seq;
  }

  /** Opens the socket, says Hello for the character, and resolves on Welcome. */
  connect(characterId: string): Promise<Data<'Welcome'>> {
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

  /** Closes for good; nothing reconnects. */
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

  /** Subscribes to one message type; returns the unsubscribe function. */
  on<T extends MessageType>(t: T, handler: Handler<T>): () => void {
    let set = this.handlers.get(t);
    if (!set) {
      set = new Set();
      this.handlers.set(t, set);
    }
    const h = handler as Handler<MessageType>;
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

  /** A Response with no req: a failed pickup, a refused chat line. */
  onUnmatchedResponse(handler: (d: Data<'Response'>) => void): () => void {
    this.unmatchedHandlers.add(handler);
    return () => {
      this.unmatchedHandlers.delete(handler);
    };
  }

  /** A frame the codec rejected: reported, never thrown. */
  onProtocolError(handler: (err: ProtocolError) => void): () => void {
    this.errorHandlers.add(handler);
    return () => {
      this.errorHandlers.delete(handler);
    };
  }

  /**
   * Sends this tick's sampled keys with the next sequence number. One Input
   * per tick: a second call for the same tick is refused, so no caller can
   * exceed the server's limit.
   */
  sendInput(input: Input, tick: number): boolean {
    if (!this.isOpen() || tick === this.lastInputTick) {
      return false;
    }
    this.lastInputTick = tick;
    this.seq++;
    this.transmit({ t: 'Input', seq: this.seq, d: input });
    return true;
  }

  /** Attack, PickUp or EnterPortal, right away, with the current sequence number. */
  sendAction(t: ActionType): boolean {
    if (!this.isOpen()) {
      return false;
    }
    this.transmit({ t, seq: this.seq, d: {} });
    return true;
  }

  /** A fire-and-forget message with no sequence number: ChatSend, Ping. */
  send<T extends 'ChatSend' | 'Ping'>(t: T, d: Data<T>): boolean {
    if (!this.isOpen()) {
      return false;
    }
    this.transmit({ t, d } as Message);
    return true;
  }

  /**
   * A command that expects a Response, matched by req. Rejects with the
   * server's code, after the timeout, or when the connection ends for good.
   * A request pending across a reconnect is re-sent once with a new req and
   * the same payload, so its idempotency key survives.
   */
  request<T extends RequestType>(t: T, d: Data<T>): Promise<Data<'Response'>> {
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
          reject(
            new RequestError('timeout', `${t}: no response within ${this.requestTimeoutMs} ms`),
          );
        }, this.requestTimeoutMs),
      };
      this.pending.set(req, pending);
      if (this.isOpen()) {
        this.transmit({ t, req, d } as Message);
      }
    });
  }

  // --- connection ---

  private open(): Promise<Data<'Welcome'>> {
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
      const ws = new this.WebSocketImpl(this.opts.url);
      this.ws = ws;
      this.pendingKick = null;
      let welcomed = false;
      const helloTimer = setTimeout(() => {
        if (!welcomed && this.ws === ws) {
          ws.close(1000, 'no welcome');
        }
      }, this.helloTimeoutMs);

      ws.onopen = () => {
        this.transmit({
          t: 'Hello',
          d: { token, protocol_version: PROTOCOL_VERSION, character_id: characterId },
        });
      };
      const handle = (msg: Message) => {
        if (this.ws !== ws) {
          return; // arrived (after the dev delay) for a socket already replaced
        }
        if (msg.t === 'Welcome' && !welcomed) {
          welcomed = true;
          clearTimeout(helloTimer);
          setToken(msg.d.token);
          this.attempt = 0;
          this.reconnecting = false;
          this.setStatus({ status: 'connected' });
          this.startPing();
          this.resendPending();
          this.dispatch(msg);
          resolve(msg.d);
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
          return; // an old socket, already replaced
        }
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
        // The close event that follows carries the decision.
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
    if (code === CLOSE_KICKED) {
      switch (kick) {
        case 'server_restart':
          this.reconnect();
          return;
        case 'unauthenticated':
        case 'session_expired':
          clearToken();
          this.failPending('disconnected', 'the session ended');
          this.setStatus({ status: 'closed', reason: 'session_ended', kick, code });
          return;
        case 'outdated_protocol':
          this.failPending('disconnected', 'the client is outdated');
          this.setStatus({ status: 'closed', reason: 'outdated_protocol', kick, code });
          return;
        case 'logged_in_elsewhere':
        case 'abuse':
        case null:
          this.failPending('disconnected', 'kicked');
          this.setStatus({ status: 'closed', reason: 'kicked', ...(kick && { kick }), code });
          return;
      }
    }
    // 4001, 4002, a dropped connection, a normal close from the server: back off and come back.
    this.reconnect();
  }

  private reconnect(): void {
    if (this.reconnecting || this.closedByUser) {
      return;
    }
    this.reconnecting = true;
    const delay = backoff(this.attempt, this.backoffOptions);
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
        p.reject(new RequestError('disconnected', `${p.t}: the connection dropped twice`));
        continue;
      }
      p.resent = true;
      const again = requestId();
      this.pending.set(again, p);
      this.transmit({ t: p.t, req: again, d: p.d } as Message);
    }
  }

  private failPending(code: 'disconnected', why: string): void {
    for (const [req, p] of this.pending) {
      clearTimeout(p.timer);
      p.reject(new RequestError(code, `${p.t}: ${why}`));
      this.pending.delete(req);
    }
  }

  // --- messages ---

  private decode(raw: unknown): Message | null {
    try {
      if (typeof raw !== 'string' && !(raw instanceof ArrayBuffer)) {
        throw new ProtocolError('frame is neither text nor binary');
      }
      return this.codec.decode(raw);
    } catch (err) {
      const pe = err instanceof ProtocolError ? err : new ProtocolError(String(err));
      for (const h of this.errorHandlers) {
        h(pe);
      }
      return null;
    }
  }

  private receive(msg: Message): void {
    switch (msg.t) {
      case 'Response': {
        const p = msg.req !== undefined ? this.pending.get(msg.req) : undefined;
        if (msg.req !== undefined && p) {
          this.pending.delete(msg.req);
          clearTimeout(p.timer);
          if (msg.d.ok) {
            p.resolve(msg.d);
          } else {
            p.reject(new RequestError(msg.d.code ?? 'internal', msg.d.message ?? 'refused'));
          }
        } else {
          for (const h of this.unmatchedHandlers) {
            h(msg.d);
          }
        }
        break;
      }
      case 'Pong': {
        const now = this.now();
        this.rtt = now - msg.d.client_time;
        this.serverTimeOffset = msg.d.server_time - (now - this.rtt / 2);
        if (this.pongDeadline) {
          clearTimeout(this.pongDeadline);
          this.pongDeadline = null;
        }
        break;
      }
      case 'Kicked':
        this.pendingKick = msg.d.reason;
        break;
      default:
        break;
    }
    this.dispatch(msg);
  }

  private dispatch(msg: Message): void {
    const set = this.handlers.get(msg.t);
    if (set) {
      for (const h of set) {
        h(msg);
      }
    }
  }

  private transmit(msg: Message): void {
    const ws = this.ws;
    if (ws?.readyState === OPEN) {
      const frame = this.codec.encode(msg);
      this.afterDelay(() => {
        if (ws.readyState === OPEN) {
          ws.send(frame);
        }
      });
    }
  }

  /** Runs fn now, or after the development delay when one is set. */
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

  // --- ping ---

  private startPing(): void {
    this.stopPing();
    this.pingTimer = setInterval(() => {
      if (!this.isOpen()) {
        return;
      }
      this.send('Ping', { client_time: this.now() });
      this.pongDeadline ??= setTimeout(() => {
        this.pongDeadline = null;
        // A silent connection: drop it; the close handler reconnects.
        const ws = this.ws;
        if (ws) {
          ws.close(1000, 'no pong');
        }
      }, this.pongTimeoutMs);
    }, this.pingIntervalMs);
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
