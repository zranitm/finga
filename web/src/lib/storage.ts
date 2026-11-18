// localStorage behind a guard, for choices kept per browser: a missing, blocked or full storage reads as nothing and drops the write, so a choice is lost, never the game.

/** The value kept under key, or null when there's none or storage can't be read. */
export function readStored(key: string): string | null {
  try {
    return typeof localStorage === 'undefined' ? null : localStorage.getItem(key);
  } catch {
    return null;
  }
}

/** Keeps value under key, if storage takes it. */
export function writeStored(key: string, value: string): void {
  try {
    if (typeof localStorage !== 'undefined') {
      localStorage.setItem(key, value);
    }
  } catch {
    // only this browser forgets the choice
  }
}
