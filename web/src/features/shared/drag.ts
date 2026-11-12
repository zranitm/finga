// What a drag between the inventory and the character window carries, so neither imports the other.
import * as v from 'valibot';

const TYPE = 'application/x-finga-item';

const Dragged = v.variant('from', [
  v.object({ from: v.literal('inventory'), itemId: v.string() }),
  v.object({ from: v.literal('worn'), equipSlot: v.string() }),
]);

export type DraggedItem = v.InferOutput<typeof Dragged>;

/** A drag event as both windows see it. */
interface Drag {
  readonly dataTransfer: DataTransfer;
}

export function carry(e: Drag, item: DraggedItem): void {
  e.dataTransfer.setData(TYPE, JSON.stringify(item));
}

/** The item a drop carries; null for anything else, such as a file or text. */
export function carried(e: Drag): DraggedItem | null {
  const raw = e.dataTransfer.getData(TYPE);
  if (!raw) {
    return null;
  }
  try {
    const parsed = v.safeParse(Dragged, JSON.parse(raw));
    return parsed.success ? parsed.output : null;
  } catch {
    return null;
  }
}
