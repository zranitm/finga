// The tiny plan's bake, as cmd/regionbake writes it and its tests keep current: 128 m at 2 m a sample, four chunks of 33 samples a side.
import { readFileSync } from 'node:fs';
import path from 'node:path';

export const TINY = { id: 'tiny', hash: 'fixture' } as const;

/** A file of the tiny bake, by its path in the bake. */
export function tinyFile(file: string): ArrayBuffer {
  const b = readFileSync(path.resolve(process.cwd(), 'src/testing/regions/tiny', file));
  return b.buffer.slice(b.byteOffset, b.byteOffset + b.byteLength);
}
