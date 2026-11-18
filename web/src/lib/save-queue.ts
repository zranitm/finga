// Saves a moment after the last of a run of changes, one at a time: each takes what it saves when it's due and sends it after the one before, so the last change lands last.
export interface SaveQueue {
  /** A change, saved delayMs after the last one. */
  readonly changed: () => void;
  /** Saves a change still waiting at once. */
  readonly flush: () => void;
  /** Settles once every save started has. */
  readonly settled: () => Promise<void>;
}

/** take is called when a save is due and returns the send, which must not reject. */
export function saveQueue(take: () => () => Promise<void>, delayMs: number): SaveQueue {
  let timer: ReturnType<typeof setTimeout> | null = null;
  let last: Promise<void> | null = null; // the save in flight, or the last one queued behind it
  const save = () => {
    timer = null;
    const send = take();
    const mine = last === null ? send() : last.then(send);
    last = mine;
    void mine.then(() => {
      if (last === mine) last = null;
    });
  };
  return {
    changed: () => {
      if (timer !== null) {
        clearTimeout(timer);
      }
      timer = setTimeout(save, delayMs);
    },
    flush: () => {
      if (timer !== null) {
        clearTimeout(timer);
        save();
      }
    },
    settled: () => last ?? Promise.resolve(),
  };
}
