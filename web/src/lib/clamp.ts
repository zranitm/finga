// Keeps a number inside [min, max]. A helper with no game knowledge, as
// everything in lib (page 7, layer 1).
export function clamp(value: number, min: number, max: number): number {
  if (min > max) {
    throw new RangeError(`clamp: min ${min} is above max ${max}`);
  }
  return Math.min(Math.max(value, min), max);
}
