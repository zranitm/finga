// The class tree from game data: a class's name, and the classes above it a weapon of theirs still fits.
import { classes } from '@/gamedata/classes';

interface ClassData {
  readonly id: string;
  readonly name: string;
  readonly parent: string | null;
}

const tree: readonly ClassData[] = classes;

/** The class's name, or its ID for one this client doesn't know. */
export function className(id: string): string {
  return tree.find((c) => c.id === id)?.name ?? id;
}

/** The class and every class above it, up to the one everyone starts as. */
export function lineOf(id: string): readonly string[] {
  const line: string[] = [];
  for (
    let c = tree.find((k) => k.id === id);
    c && !line.includes(c.id);
    c = tree.find((k) => k.id === c?.parent)
  ) {
    line.push(c.id);
  }
  return line;
}

/** Whether a weapon for itemClass fits cls: one of cls's own class or a class above it, or one with no class. */
export function fits(itemClass: string | undefined, cls: string): boolean {
  return itemClass === undefined || lineOf(cls).includes(itemClass);
}
