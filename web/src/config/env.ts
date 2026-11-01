export interface Env {
  /** "" for same origin: the Vite proxy in development, nginx under make up. */
  readonly apiOrigin: string;
  readonly isDev: boolean;
  /** One-way socket delay for latency checks; always 0 in builds. */
  readonly netDelayMs: number;
}

export interface RawEnv {
  readonly VITE_API_ORIGIN?: string | undefined;
  readonly VITE_NET_DELAY_MS?: string | undefined;
  readonly DEV: boolean;
}

/** A malformed origin fails fast at start. */
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
