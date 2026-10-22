// Stores register a reset function here when they are created (C05), and
// tests call resetStores() between cases so state never leaks from one to
// the next. Imported by tests and store modules only.
const resets = new Set<() => void>();

export function registerReset(reset: () => void): void {
  resets.add(reset);
}

export function resetStores(): void {
  for (const reset of resets) {
    reset();
  }
}
