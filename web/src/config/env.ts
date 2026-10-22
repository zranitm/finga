// Typed access to import.meta.env (page 7). VITE_API_ORIGIN names the game
// server's origin; empty means same origin, which is the case behind the
// Vite proxy in development and behind nginx in Stage 2. VITE_NET_DELAY_MS
// delays every frame the socket sends and receives, in development only,
// for the latency check of client step C08.
export interface Env {
  /** "" for same origin, else an origin such as http://192.168.1.10:8080 */
  readonly apiOrigin: string;
  readonly isDev: boolean;
  /** One-way delay added to the socket in development; 0 in builds and by default. */
  readonly netDelayMs: number;
}

export interface RawEnv {
  readonly VITE_API_ORIGIN?: string | undefined;
  readonly VITE_NET_DELAY_MS?: string | undefined;
  readonly DEV: boolean;
}

/** Validates the raw variables; a malformed origin fails fast at start. */
export function parseEnv(raw: RawEnv): Env {
  const origin = (raw.VITE_API_ORIGIN ?? '').trim();
  const netDelayMs = raw.DEV ? parseDelay(raw.VITE_NET_DELAY_MS) : 0;
  if (origin === '') {
    return { apiOrigin: '', isDev: raw.DEV, netDelayMs };
  }
  let url: URL;
  try {
    url = new URL(origin);
  } catch {
    throw new Error(`VITE_API_ORIGIN must be an origin like http://host:8080, got ${origin}`);
  }
  if ((url.protocol !== 'http:' && url.protocol !== 'https:') || url.origin !== origin) {
    throw new Error(`VITE_API_ORIGIN must be an http(s) origin with no path, got ${origin}`);
  }
  return { apiOrigin: url.origin, isDev: raw.DEV, netDelayMs };
}

function parseDelay(raw: string | undefined): number {
  const text = (raw ?? '').trim();
  if (text === '') {
    return 0;
  }
  const ms = Number(text);
  if (!Number.isInteger(ms) || ms < 0 || ms > 5000) {
    throw new Error(
      `VITE_NET_DELAY_MS must be a whole number of milliseconds up to 5000, got ${text}`,
    );
  }
  return ms;
}

export const env: Env = parseEnv(import.meta.env);
