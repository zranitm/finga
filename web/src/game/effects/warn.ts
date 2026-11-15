const logged = new Set<string>();

/** Logs a problem with the game's art once a page: a model, a sheet or a sound; the game plays on without it. */
export function warnOnce(message: string): void {
  if (!logged.has(message)) {
    logged.add(message);
    console.warn(`finga: ${message}`);
  }
}
