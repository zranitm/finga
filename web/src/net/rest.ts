// The token is read on every call, so a rotation after Hello is picked up at once.
import { env } from '@/config/env';
import { errorText, noAnswerText, refusalCode } from '@/protocol/errors';

import { clearToken, getToken, setToken } from './token';

export interface Account {
  readonly id: string;
  readonly username: string;
  readonly created_at: string;
}

export interface Stats {
  readonly str: number;
  readonly dex: number;
  readonly int: number;
  readonly agi: number;
  readonly vit: number;
}

export interface Character {
  readonly id: string;
  readonly name: string;
  readonly class: string;
  readonly level: number;
  readonly exp: number;
  readonly stats: Stats;
  readonly hp: number;
  readonly map_id: string;
  /** The skill on each of the bar's eight keys, '' for none. */
  readonly skill_bar: readonly string[];
}

export interface Settings {
  /** False when the account never set it. */
  readonly auto_assign_points?: boolean;
  /** False when the account never set it. */
  readonly chat_pinned?: boolean;
  /** False when the account never set it: the minimap shows. */
  readonly hide_minimap?: boolean;
  /** How far the camera has pulled back, 0 close to 1 far; 0 when the account never set it. */
  readonly zoom?: number;
  /** Walk mode, which never sprints; false when the account never set it: sprint mode. */
  readonly walk?: boolean;
  /** The changes to the default keys, an action to a KeyboardEvent.code; none when the account never set any. */
  readonly keys?: Readonly<Record<string, string>>;
}

/** A character's arena build (pvp.md section 6): eight keys, '' for an empty one, and the trait chosen by skill, one left out the skill's default. */
export interface ArenaBuild {
  readonly skill_bar: readonly string[];
  readonly traits: Readonly<Record<string, string>>;
}

/** A character's standing in a rated mode. */
export interface PvpStanding {
  readonly mode: string;
  readonly shown: number;
  readonly rank: string;
  /** Placement matches still to play, 0 once placed. */
  readonly placement_left: number;
  readonly wins: number;
  readonly losses: number;
  readonly draws: number;
  /** One past those placed above, 0 before placement. */
  readonly place: number;
}

/** A character's PvP profile: each rated mode, in pvp.yaml's order, and their arena build, the saved one or the world bar's copy. */
export interface PvpProfile {
  readonly standings: readonly PvpStanding[];
  readonly build: ArenaBuild;
}

export interface LeaderboardRow {
  readonly place: number;
  readonly character_id: string;
  readonly name: string;
  readonly class: string;
  readonly shown: number;
  readonly rank: string;
  readonly wins: number;
  readonly losses: number;
  readonly draws: number;
}

/** A rated mode's top 100 placed, highest first, a tie sharing its place. */
export interface Leaderboard {
  readonly mode: string;
  readonly rows: readonly LeaderboardRow[];
}

export class RestError extends Error {
  constructor(
    readonly status: number,
    readonly code: number | 'network' | 'malformed', // the server's refusal code, or why no readable answer came
    message: string,
  ) {
    super(message);
    this.name = 'RestError';
  }
}

export interface RestClient {
  register(username: string, password: string): Promise<Account>;
  login(username: string, password: string): Promise<void>;
  logout(): Promise<void>;
  /** Ends every session of the account; a failure other than 401 keeps the token, as the others may still be open. */
  logoutEverywhere(): Promise<void>;
  listCharacters(): Promise<Character[]>;
  /** Every new character is a Beginner; a class comes later, at a trainer. */
  createCharacter(name: string): Promise<Character>;
  getSettings(): Promise<Settings>;
  putSettings(settings: Settings): Promise<Settings>;
  /** Six skill IDs, '' for an empty key; the server keeps them for the character. */
  putSkillBar(characterId: string, slots: readonly string[]): Promise<{ slots: string[] }>;
  getPvpProfile(characterId: string): Promise<PvpProfile>;
  /** A match the character is in fights with it from its next round's countdown. */
  putArenaBuild(characterId: string, build: ArenaBuild): Promise<ArenaBuild>;
  getLeaderboard(mode: string): Promise<Leaderboard>;
}

export function createRestClient(): RestClient {
  async function call<T>(
    method: string,
    path: string,
    body?: unknown,
    extra?: Pick<RequestInit, 'keepalive'>,
  ): Promise<T> {
    const headers: Record<string, string> = {};
    if (body !== undefined) {
      headers['Content-Type'] = 'application/json';
    }
    const token = getToken();
    if (token !== null) {
      headers.Authorization = `Bearer ${token}`;
    }
    let res: Response;
    try {
      res = await fetch(env.apiOrigin + path, {
        method,
        headers,
        ...(body !== undefined && { body: JSON.stringify(body) }),
        ...extra,
      });
    } catch (err) {
      console.info(
        `finga: ${method} ${path} reached no server: ${err instanceof Error ? err.message : String(err)}`,
      );
      throw new RestError(0, 'network', noAnswerText.network);
    }
    if (res.status === 204) {
      return undefined as T;
    }
    let parsed: unknown;
    try {
      parsed = await res.json();
    } catch {
      console.info(`finga: ${method} ${path} answered ${res.status} with a body that is not JSON`);
      throw new RestError(res.status, 'malformed', noAnswerText.malformed);
    }
    if (!res.ok) {
      throw toRestError(`${method} ${path}`, res.status, parsed);
    }
    return parsed as T;
  }

  return {
    register: (username, password) =>
      call<Account>('POST', '/api/accounts', { username, password }),
    async login(username, password) {
      const { token } = await call<{ token: string }>('POST', '/api/sessions', {
        username,
        password,
      });
      setToken(token);
    },
    async logout() {
      try {
        await call<void>('DELETE', '/api/sessions/current');
      } finally {
        clearToken();
      }
    },
    async logoutEverywhere() {
      try {
        await call<void>('DELETE', '/api/sessions');
      } catch (err) {
        if (err instanceof RestError && err.status === 401) {
          clearToken();
        }
        throw err;
      }
      clearToken();
    },
    listCharacters: async () =>
      (await call<{ characters: Character[] }>('GET', '/api/characters')).characters,
    createCharacter: (name) => call<Character>('POST', '/api/characters', { name }),
    getSettings: () => call<Settings>('GET', '/api/settings'),
    putSettings: (settings) => call<Settings>('PUT', '/api/settings', settings),
    // keepalive: a save made as the tab closes still goes.
    putSkillBar: (characterId, slots) =>
      call<{ slots: string[] }>(
        'PUT',
        `/api/characters/${encodeURIComponent(characterId)}/skill-bar`,
        { slots },
        { keepalive: true },
      ),
    getPvpProfile: (characterId) =>
      call<PvpProfile>('GET', `/api/characters/${encodeURIComponent(characterId)}/pvp`),
    // keepalive, as the skill bar's
    putArenaBuild: (characterId, build) =>
      call<ArenaBuild>(
        'PUT',
        `/api/characters/${encodeURIComponent(characterId)}/pvp-build`,
        build,
        { keepalive: true },
      ),
    getLeaderboard: (mode) =>
      call<Leaderboard>('GET', `/api/pvp/leaderboard?mode=${encodeURIComponent(mode)}`),
  };
}

function toRestError(call: string, status: number, body: unknown): RestError {
  if (typeof body === 'object' && body !== null && 'code' in body) {
    const { code } = body;
    if (typeof code === 'number') {
      const refused = refusalCode(code);
      return new RestError(status, refused, errorText(refused));
    }
  }
  console.info(`finga: ${call} answered ${status} without an error body`);
  return new RestError(status, 'malformed', noAnswerText.malformed);
}
