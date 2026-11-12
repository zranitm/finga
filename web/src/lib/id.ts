// UUIDv7 like the server's: crypto.randomUUID is v4 and secure-context only, and a counter repeats after a reload.
const hex = Array.from({ length: 256 }, (_, n) => n.toString(16).padStart(2, '0'));

export function uuidV7(): string {
  const ms = Date.now();
  if (!Number.isSafeInteger(ms) || ms < 0 || ms >= 2 ** 48) {
    throw new RangeError(`uuidV7 needs a 48-bit millisecond time, got ${ms}`);
  }
  const b = crypto.getRandomValues(new Uint8Array(16));
  for (let i = 5, t = ms; i >= 0; i--, t = Math.floor(t / 256)) {
    b[i] = t % 256;
  }
  b[6] = ((b[6] ?? 0) & 0x0f) | 0x70; // version 7
  b[8] = ((b[8] ?? 0) & 0x3f) | 0x80; // variant 10xx
  const h = Array.from(b, (n) => hex[n]).join('');
  return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`;
}

/** A request ID for a command that expects a Response: r-<uuid>. */
export function requestId(): string {
  return `r-${uuidV7()}`;
}

/** An idempotency key for an item action, made once per click: k-<uuid>. */
export function idempotencyKey(): string {
  return `k-${uuidV7()}`;
}
