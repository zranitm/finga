// The skill bar of the character in play: the skill on each of its eight keys, as the server keeps it, a chain's root or a skill in no chain, what each key offers now, and the cooldowns and buffs this client times for it. In a match, from a countdown, the keys are the arena bar the match put on (pvp.md section 6).
import { defineStore } from './store';

export const SKILL_KEYS = 8; // the server's SkillBarKeys, and the input's skill keys

/** What a skill dragged from the skill window carries: its ID. */
export const SKILL_DRAG_TYPE = 'application/x-finga-skill';

/** A skill's cooldown, in performance.now() milliseconds. */
export interface Cooldown {
  readonly from: number;
  readonly until: number;
}

/** One of the player's own buffs: its skill at the points and trait it was used with, "" the default, until a time in performance.now() milliseconds. */
export interface ActiveBuff {
  readonly skill: string;
  readonly points: number;
  readonly trait: string;
  readonly until: number;
}

export interface SkillBarState {
  /** The bar in play: the arena bar while a match has one on, else the world bar. */
  readonly slots: readonly string[];
  /** The player's own bar, which their changes edit and the server keeps. */
  readonly world: readonly string[];
  /** The arena bar a match put on at its last countdown; null out of a match, or before its first. */
  readonly arena: readonly string[] | null;
  /** What each key offers now: its slot's skill, or a follow-up its chain gives now (skill-chains.md section 5). */
  readonly live: readonly string[];
  /** Counts the player's own changes, which are saved; a load is not one. */
  readonly edits: number;
  /** Counts the asks to save a change still waiting at once, as the skill window closes. */
  readonly saveAsks: number;
  readonly cooldowns: Readonly<Record<string, Cooldown>>;
  readonly buffs: readonly ActiveBuff[];
  /** The world's tick, which every cooldown is rounded up to whole ones of. */
  readonly tickMs: number;
  load(slots: readonly string[]): void;
  /** A match's countdown puts on its arena bar, as the world tells it. */
  wear(bar: readonly string[]): void;
  /** Out of the match, the world bar is back. */
  unwear(): void;
  /** What each key offers now, as the world's frame loop judges it; the same again changes nothing. */
  showLive(live: readonly string[]): void;
  /** Puts the skill on the world bar's key; on another key already, it leaves that one. */
  put(key: number, skill: string): void;
  saveNow(): void;
  swap(a: number, b: number): void;
  clear(key: number): void;
  cool(skill: string, cooldown: Cooldown | null): void;
  /** A buff used again starts over. */
  buff(b: ActiveBuff): void;
  /** Ends the buffs whose time is up by now, or every buff. */
  endBuffs(now?: number): void;
  /** A join brings the world's cooldowns and buffs, as its last checkpoint left them, and the client's follow; a map change carries both. */
  restoreTimers(
    cooldowns: Readonly<Record<string, Cooldown>>,
    buffs: readonly ActiveBuff[],
    tickMs: number,
  ): void;
}

function barOf(slots: readonly string[]): string[] {
  return Array.from({ length: SKILL_KEYS }, (_, k) => slots[k] ?? '');
}

export const skillBar = defineStore<SkillBarState>((set, get) => {
  /** The world bar changed by the player: in play unless an arena bar is on. */
  const edited = (world: string[]) => {
    const arena = get().arena;
    set({ world, ...(arena === null && { slots: world, live: world }), edits: get().edits + 1 });
  };
  return {
    slots: barOf([]),
    world: barOf([]),
    arena: null,
    live: barOf([]),
    edits: 0,
    saveAsks: 0,
    cooldowns: {},
    buffs: [],
    tickMs: 50,
    load: (slots) => {
      const world = barOf(slots);
      set({ slots: world, world, arena: null, live: world, cooldowns: {}, buffs: [] });
    },
    wear: (bar) => {
      const arena = barOf(bar);
      set({ slots: arena, arena, live: arena });
    },
    unwear: () => {
      set({ slots: get().world, arena: null, live: get().world });
    },
    showLive: (live) => {
      const bar = barOf(live);
      if (bar.some((s, k) => s !== get().live[k])) {
        set({ live: bar });
      }
    },
    saveNow: () => {
      set({ saveAsks: get().saveAsks + 1 });
    },
    put: (key, skill) => {
      const world = get().world.map((s) => (s === skill ? '' : s));
      world[key] = skill;
      edited(world);
    },
    swap: (a, b) => {
      const world = [...get().world];
      [world[a], world[b]] = [world[b] ?? '', world[a] ?? ''];
      edited(world);
    },
    clear: (key) => {
      const world = [...get().world];
      world[key] = '';
      edited(world);
    },
    cool: (skill, cooldown) => {
      const rest = Object.fromEntries(Object.entries(get().cooldowns).filter(([k]) => k !== skill));
      set({ cooldowns: cooldown ? { ...rest, [skill]: cooldown } : rest });
    },
    buff: (b) => {
      set({ buffs: [...get().buffs.filter((x) => x.skill !== b.skill), b] });
    },
    endBuffs: (now) => {
      const buffs = now === undefined ? [] : get().buffs.filter((b) => b.until > now);
      if (buffs.length !== get().buffs.length) {
        set({ buffs });
      }
    },
    restoreTimers: (cooldowns, buffs, tickMs) => {
      set({ cooldowns, buffs, tickMs });
    },
  };
});
