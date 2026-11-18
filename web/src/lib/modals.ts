// The modal dialogs open now, newest last: the game takes no keys while one is, and Esc closes the newest.
const open: symbol[] = [];
const opening = new Set<() => void>();

/** A modal open until its close: whether it's the newest, which Esc closes. */
export interface OpenModal {
  readonly newest: () => boolean;
  readonly close: () => void;
}

export function openModal(): OpenModal {
  const id = Symbol('modal');
  open.push(id);
  for (const fn of opening) {
    fn();
  }
  return {
    newest: () => open.at(-1) === id,
    close: () => {
      const at = open.indexOf(id);
      if (at >= 0) {
        open.splice(at, 1);
      }
    },
  };
}

/** Calls fn as each modal opens, until the stop it returns. */
export function onModalOpen(fn: () => void): () => void {
  opening.add(fn);
  return () => {
    opening.delete(fn);
  };
}

/** Whether any modal is open. */
export function anyModal(): boolean {
  return open.length > 0;
}
