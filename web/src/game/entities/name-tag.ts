import type Phaser from 'phaser';

/** A name tag's bottom, over a player or an NPC, sits this far above the feet. */
export const NAME_TAG_ABOVE = 70;
/** How far above the feet a name tag reaches: its offset plus the line of its outlined 12 px text. */
export const NAME_TAG_TOP = NAME_TAG_ABOVE + 18;

/** A dark outline on the world's labels, tags and markers, so they read on a light sky or hill. */
export const OUTLINE = { stroke: '#0a0c16', strokeThickness: 3 } as const;

export const NAME_TAG_STYLE = { fontSize: '12px', fontFamily: 'sans-serif', ...OUTLINE } as const;

/** Text drawn from its bottom centre: x is its centre, bottom the line it sits on. */
export interface TagBox {
  readonly x: number;
  readonly bottom: number;
  readonly w: number;
  readonly h: number;
}

/** The box of a text with its origin at its bottom centre, as every tag and marker has. */
export function tagBox(t: Phaser.GameObjects.Text): TagBox {
  return { x: t.x, bottom: t.y, w: t.width, h: t.height };
}

/** Room left between a lifted tag and the text under it. */
const TAG_GAP = 2;

/** Each tag's bottom, in order, lifted above any fixed box or earlier tag it would cover; tags only go up, off their owner's body. */
export function stackTags(fixed: readonly TagBox[], tags: readonly TagBox[]): number[] {
  const placed = [...fixed];
  return tags.map((t) => {
    const near = placed.filter((p) => Math.abs(p.x - t.x) * 2 < p.w + t.w);
    let bottom = t.bottom;
    // Lifting past a box clears it for good, so each box lifts the tag at most once.
    for (let lifted = true; lifted;) {
      lifted = false;
      for (const p of near) {
        const top = p.bottom - p.h;
        if (bottom + TAG_GAP > top && bottom - t.h < p.bottom + TAG_GAP) {
          bottom = top - TAG_GAP;
          lifted = true;
        }
      }
    }
    placed.push({ ...t, bottom });
    return bottom;
  });
}
