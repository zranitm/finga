/** The indefinite article before name. */
export function article(name: string): string {
  return /^[AEIOU]/i.test(name) ? 'an' : 'a';
}

/** A time left as m:ss, rounded up, and never below 0:00. */
export function timeLeft(ms: number): string {
  const s = Math.max(0, Math.ceil(ms / 1000));
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;
}
