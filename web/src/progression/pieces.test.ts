import { describe, expect, it } from 'vitest';

import { quests } from '@/gamedata/quests';

import { howToGet, pieceById, pieceOfItem, pieces, piecesFor } from './pieces';
import type { Piece } from './pieces';

describe('the wardrobe’s pieces (outfits.md section 3)', () => {
  it('are held by their items, piece.<id>, and by no other', () => {
    expect(pieceOfItem('piece.knight_plate')).toBe(pieceById('knight_plate'));
    expect(pieceOfItem('relic.knight_plate')).toBeUndefined();
    expect(pieceOfItem('knight_plate')).toBeUndefined();
    expect(pieceOfItem('piece.nothing_like_it')).toBeUndefined();
  });

  it('give each slot its pieces in the data’s order, and a class only weapon looks of its kind', () => {
    expect(piecesFor('outfit', 'warrior').map((p) => p.id)).toEqual(
      pieces.filter((p) => p.slot === 'outfit').map((p) => p.id),
    );
    expect(piecesFor('weapon', 'warrior').map((p) => p.kind)).toEqual([
      'one_handed',
      'one_handed',
      'one_handed',
    ]);
    expect(piecesFor('weapon', 'magician').map((p) => p.id)).toEqual(['oak_wand', 'bronze_staff']);
    expect(piecesFor('weapon', 'bowman').map((p) => p.id)).toEqual([
      'hand_crossbow',
      'bronze_crossbow',
    ]);
    expect(piecesFor('weapon', 'thief').map((p) => p.id)).toEqual(['bronze_knife']);
  });

  it('say how each is got, a line a source: a boss’s drop, a rank in the rated modes, a quest, a feat', () => {
    expect(howToGet(pieceById('knight_plate')!)).toEqual(['Dropped by King Slime']);
    const quest = quests[0];
    const every: Piece = {
      id: 'every',
      slot: 'headwear',
      name: 'Every Way',
      stars: 3,
      kind: null,
      from: [
        { kind: 'boss', id: 'vampire_bat' },
        { kind: 'rank', id: 'gold' },
        { kind: 'quest', id: quest.id },
        { kind: 'feat', id: 'level_10' },
        { kind: 'feat', id: 'no_such_feat' },
      ],
    };
    expect(howToGet(every)).toEqual([
      'Dropped by Vampire Bat',
      'Reach Gold in 1v1 or 3v3',
      `Quest: ${quest.name}`,
      'Feat: Reach level 10',
      'Feat: no_such_feat', // one the data lacks: its id
    ]);
  });
});
