// The wardrobe's pieces from game data (outfits.md sections 3 and 7): each slot's, a class's weapon looks, and how to get each, in words.
import { classes } from '@/gamedata/classes';
import { outfits } from '@/gamedata/outfits';
import { pvp } from '@/gamedata/pvp';

import { mobName, rankName } from './names';
import { questName } from './quests';

export type Slot = 'outfit' | 'headwear' | 'face' | 'weapon' | 'wings';

/** A piece as the data has it: its sources' kinds are every one a piece can name, not only those the catalogue uses yet. */
export interface Piece {
  readonly id: string;
  readonly slot: string;
  readonly name: string;
  readonly stars: number;
  readonly kind: string | null;
  readonly from: readonly {
    readonly kind: 'boss' | 'rank' | 'quest' | 'feat';
    readonly id: string;
  }[];
}

export const pieces: readonly Piece[] = outfits.pieces;
const feats: readonly { readonly id: string; readonly name: string }[] = outfits.feats;

export function pieceById(id: string): Piece | undefined {
  return pieces.find((p) => p.id === id);
}

/** The pieces of slot a character of class cls can wear, in the data's order: of a weapon look, those of its kind alone. */
export function piecesFor(slot: Slot, cls: string): Piece[] {
  const kind = classes.find((c) => c.id === cls)?.weapon;
  return pieces.filter((p) => p.slot === slot && (slot !== 'weapon' || p.kind === kind));
}

/** The rated modes' names, as a rank is reached in: "1v1 or 3v3". */
const rated = () =>
  pvp.modes
    .filter((m) => m.rated)
    .map((m) => m.name)
    .join(' or ');

/** How piece is got, a line for each of its sources. */
export function howToGet(piece: Piece): string[] {
  return piece.from.map((f) => {
    switch (f.kind) {
      case 'boss':
        return `Dropped by ${mobName(f.id)}`;
      case 'rank':
        return `Reach ${rankName(f.id)} in ${rated()}`;
      case 'quest':
        return `Quest: ${questName(f.id)}`;
      case 'feat':
        return `Feat: ${feats.find((t) => t.id === f.id)?.name ?? f.id}`;
    }
  });
}
