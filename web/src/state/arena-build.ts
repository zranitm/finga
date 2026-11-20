// The character's arena build (pvp.md section 6), as the skill window's Arena tab edits it: the skill on each of its eight keys and the trait chosen by skill. The server keeps it, and a match fights with it from its next countdown.
import { SKILL_KEYS } from './skill-bar';
import { defineStore } from './store';

export interface ArenaBuildState {
  /** The eight keys, '' for an empty one; null till the profile brings them. */
  readonly bar: readonly string[] | null;
  /** The trait chosen by skill, for any of the class's active skills, a follow-up's too, which rides on its root's key; a skill on its default isn't listed. */
  readonly traits: Readonly<Record<string, string>>;
  /** Counts the player's own changes, which are saved; a load is not one. */
  readonly edits: number;
  /** The server's build, taken while the player hasn't changed theirs: once they have, theirs is the newest. */
  load(build: {
    readonly skill_bar: readonly string[];
    readonly traits: Readonly<Record<string, string>>;
  }): void;
  /** Puts the skill on the key; on another key already, it leaves that one. */
  put(key: number, skill: string): void;
  swap(a: number, b: number): void;
  clear(key: number): void;
  /** Chooses the skill's trait, '' for its default. */
  choose(skill: string, trait: string): void;
}

function barOf(slots: readonly string[]): string[] {
  return Array.from({ length: SKILL_KEYS }, (_, k) => slots[k] ?? '');
}

export const arenaBuild = defineStore<ArenaBuildState>((set, get) => {
  const edited = (bar: string[], traits = get().traits) => {
    set({ bar, traits, edits: get().edits + 1 });
  };
  const own = () => barOf(get().bar ?? []);
  return {
    bar: null,
    traits: {},
    edits: 0,
    load: (build) => {
      if (get().edits === 0) {
        set({ bar: barOf(build.skill_bar), traits: { ...build.traits } });
      }
    },
    put: (key, skill) => {
      const bar = own().map((s) => (s === skill ? '' : s));
      bar[key] = skill;
      edited(bar);
    },
    swap: (a, b) => {
      const bar = own();
      [bar[a], bar[b]] = [bar[b] ?? '', bar[a] ?? ''];
      edited(bar);
    },
    clear: (key) => {
      const bar = own();
      bar[key] = '';
      edited(bar);
    },
    choose: (skill, trait) => {
      const rest = Object.fromEntries(Object.entries(get().traits).filter(([s]) => s !== skill));
      edited(own(), trait === '' ? rest : { ...rest, [skill]: trait });
    },
  };
});
