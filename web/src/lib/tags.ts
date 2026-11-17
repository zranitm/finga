// Name tags kept clear of each other, for the world's labels and the map window's.

/** Text on the screen drawn from its bottom centre: x its centre, bottom the line it sits on, in pixels down. */
export interface TagBox {
  readonly x: number;
  readonly bottom: number;
  readonly w: number;
  readonly h: number;
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
