// Typed access to import.meta.env (page 7). VITE_API_ORIGIN names the game
// server's origin; empty means same origin, which is the case behind the
// Vite proxy in development and behind nginx in Stage 2.
export interface Env {
  /** "" for same origin, else an origin such as http://192.168.1.10:8080 */
  readonly apiOrigin: string;
  readonly isDev: boolean;
}

export interface RawEnv {
  readonly VITE_API_ORIGIN?: string | undefined;
  readonly DEV: boolean;
}

/** Validates the raw variables; a malformed origin fails fast at start. */
export function parseEnv(raw: RawEnv): Env {
  const origin = (raw.VITE_API_ORIGIN ?? '').trim();
  if (origin === '') {
    return { apiOrigin: '', isDev: raw.DEV };
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
  return { apiOrigin: url.origin, isDev: raw.DEV };
}

export const env: Env = parseEnv(import.meta.env);
