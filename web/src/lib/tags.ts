// Name tags kept clear of each other, for the world's labels and the map window's.

/** Text on the screen drawn from its bottom centre: x its centre, bottom the line it sits on, in pixels down. */
export interface TagBox {
  readonly x: number;
  readonly bottom: number;
  readonly w: number;
  readonly h: number;
}

/** Room left between a lifted tag and the text under it. */
export const TAG_GAP = 2;

/** Whether two tags would cover each other: side by side, and less than TAG_GAP apart up and down. */
export function clashes(a: TagBox, b: TagBox): boolean {
  return (
    Math.abs(a.x - b.x) * 2 < a.w + b.w &&
    a.bottom + TAG_GAP > b.bottom - b.h &&
    a.bottom - a.h < b.bottom + TAG_GAP
  );
}

/** Each tag's bottom, in order, lifted above any fixed box or earlier tag it would cover; tags only go up, off their owner's body. */
export function stackTags(fixed: readonly TagBox[], tags: readonly TagBox[]): number[] {
  const placed = [...fixed];
  return tags.map((t) => {
    let at = t;
    // Lifting past a box clears it for good, so each box lifts the tag at most once.
    for (let lifted = true; lifted;) {
      lifted = false;
      for (const p of placed) {
        if (clashes(at, p)) {
          at = { ...at, bottom: p.bottom - p.h - TAG_GAP };
          lifted = true;
        }
      }
    }
    placed.push(at);
    return at.bottom;
  });
}
