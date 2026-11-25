// The account's wardrobe as the client knows it (outfits.md section 7): the pieces collected, from GET /api/wardrobe, and the look the character wears, as its own frames say.
import { defineStore } from './store';

/** A look's five picks: each slot's piece id, empty for the class's own or no wings, and whether the head is bare. */
export interface Picks {
  readonly outfit: string;
  readonly headwear: string;
  readonly face: string;
  readonly weapon: string;
  readonly wings: string;
  readonly hideHeadwear: boolean;
}

/** A character drawn in 3D into a canvas of its own, as the game draws one for a window: dressed in a class and picks, turned by a drag, drawn each frame till it's closed. */
export interface Portrait {
  show(cls: string, look: Picks): void;
  turn(radians: number): void;
  close(): void;
}

/** The class's own look in every slot, and no wings. */
export const OWN_LOOK: Picks = {
  outfit: '',
  headwear: '',
  face: '',
  weapon: '',
  wings: '',
  hideHeadwear: false,
};

export const samePicks = (a: Picks, b: Picks) =>
  a.outfit === b.outfit &&
  a.headwear === b.headwear &&
  a.face === b.face &&
  a.weapon === b.weapon &&
  a.wings === b.wings &&
  a.hideHeadwear === b.hideHeadwear;

export interface WardrobeState {
  readonly collected: ReadonlySet<string> | null; // null till the list first answers
  readonly added: ReadonlySet<string>; // added since the character entered: a list asked for before an add may answer after it
  readonly worn: Picks;
  readonly trying: string | null; // a piece the inventory's Preview hands the wardrobe to try on
  /** The account's pieces, in place of any before, with every piece added since the character entered: a piece never leaves a wardrobe. */
  collect(ids: readonly string[]): void;
  /** One piece more, as an add from the inventory answers; a list not in yet will hold it. */
  add(id: string): void;
  preview(id: string | null): void;
  /** The character's own frame wears look; the store keeps its picks alone. */
  wear(look: Picks): void;
  clear(): void;
}

export const wardrobe = defineStore<WardrobeState>((set, get) => ({
  collected: null,
  added: new Set(),
  worn: OWN_LOOK,
  trying: null,
  collect: (ids) => {
    set({ collected: new Set([...ids, ...get().added]) });
  },
  add: (id) => {
    const { collected, added } = get();
    set({
      added: new Set([...added, id]),
      collected: collected && new Set([...collected, id]),
    });
  },
  preview: (id) => {
    set({ trying: id });
  },
  wear: (look) => {
    if (!samePicks(get().worn, look)) {
      const { outfit, headwear, face, weapon, wings, hideHeadwear } = look;
      set({ worn: { outfit, headwear, face, weapon, wings, hideHeadwear } }); // the picks alone, not a frame's message
    }
  },
  clear: () => {
    set({ collected: null, added: new Set(), worn: OWN_LOOK, trying: null });
  },
}));
