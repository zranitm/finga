const logged = new Set<string>();

/** Logs a problem with the effects' art once a page: the game plays on without it. */
export function warnOnce(message: string): void {
  if (!logged.has(message)) {
    logged.add(message);
    console.warn(`finga: ${message}`);
  }
}
