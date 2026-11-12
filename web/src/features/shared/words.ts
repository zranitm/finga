/** The indefinite article before name. */
export function article(name: string): string {
  return /^[AEIOU]/i.test(name) ? 'an' : 'a';
}
