// A WebSocket double for tests: records the frames the client sends and
// lets a test play the server (open, push frames, close with a code).
// Imported by tests only.
export class FakeWebSocket {
  static instances: FakeWebSocket[] = [];

  static reset(): void {
    FakeWebSocket.instances = [];
  }

  /** The newest socket the code under test opened. */
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
  readonly sent: string[] = [];
  closedWith: { code: number; reason: string } | null = null;

  onopen: ((ev: Event) => void) | null = null;
  onmessage: ((ev: MessageEvent<unknown>) => void) | null = null;
  onclose: ((ev: CloseEvent) => void) | null = null;
  onerror: ((ev: Event) => void) | null = null;

  constructor(readonly url: string) {
    FakeWebSocket.instances.push(this);
  }

  // --- the client's side ---

  send(data: string | ArrayBuffer): void {
    if (this.readyState !== FakeWebSocket.OPEN) {
      throw new Error('send on a socket that is not open');
    }
    this.sent.push(typeof data === 'string' ? data : new TextDecoder().decode(data));
  }

  close(code = 1000, reason = ''): void {
    if (this.readyState === FakeWebSocket.CLOSED) {
      return;
    }
    this.closedWith = { code, reason };
    this.readyState = FakeWebSocket.CLOSED;
    this.onclose?.({ code, reason, wasClean: true } as CloseEvent);
  }

  // --- the server's side ---

  /** The server accepts the connection. */
  open(): void {
    this.readyState = FakeWebSocket.OPEN;
    this.onopen?.(new Event('open'));
  }

  /** The server sends a frame (an object is encoded as JSON). */
  push(frame: string | object): void {
    const data = typeof frame === 'string' ? frame : JSON.stringify(frame);
    this.onmessage?.({ data } as MessageEvent<unknown>);
  }

  /** The server closes with a code. */
  closeFromServer(code: number, reason = ''): void {
    this.readyState = FakeWebSocket.CLOSED;
    this.onclose?.({ code, reason, wasClean: true } as CloseEvent);
  }

  /** The frames sent so far, parsed. */
  frames(): { t: string; req?: string; seq?: number; d: Record<string, unknown> }[] {
    return this.sent.map(
      (s) => JSON.parse(s) as { t: string; req?: string; seq?: number; d: Record<string, unknown> },
    );
  }
}

/** FakeWebSocket as the constructor type GameSocket wants. */
export const fakeWebSocketClass = FakeWebSocket as unknown as new (url: string) => WebSocket;
