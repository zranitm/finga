// The account's pieces from GET /api/wardrobe into the wardrobe's store: as a character enters the world, so the inventory knows a piece already collected, and as the wardrobe opens.
import type { RestClient } from '@/net/rest';
import { wardrobe } from '@/state/wardrobe';

/** Asks for the pieces; onFail hears a failed ask. The returned stop drops an answer that comes after it. */
export function loadWardrobe(
  rest: Pick<RestClient, 'getWardrobe'>,
  onFail: () => void,
): () => void {
  let on = true;
  rest.getWardrobe().then(
    (w) => on && wardrobe.getState().collect(w.pieces.map((p) => p.id)),
    () => on && onFail(),
  );
  return () => {
    on = false;
  };
}
