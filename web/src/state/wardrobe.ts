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
  readonly worn: Picks;
  /** The account's pieces, in place of any before. */
  collect(ids: readonly string[]): void;
  /** The character's own frame wears look; the store keeps its picks alone. */
  wear(look: Picks): void;
  clear(): void;
}

export const wardrobe = defineStore<WardrobeState>((set, get) => ({
  collected: null,
  worn: OWN_LOOK,
  collect: (ids) => {
    set({ collected: new Set(ids) });
  },
  wear: (look) => {
    if (!samePicks(get().worn, look)) {
      const { outfit, headwear, face, weapon, wings, hideHeadwear } = look;
      set({ worn: { outfit, headwear, face, weapon, wings, hideHeadwear } }); // the picks alone, not a frame's message
    }
  },
  clear: () => {
    set({ collected: null, worn: OWN_LOOK });
  },
}));
