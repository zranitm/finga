// The test plans' bakes, as cmd/regionbake writes them and its tests keep current: tiny, 128 m at 2 m a sample in four chunks of 33 samples a side; steep, as steep as the wide plan gets; hollow, a small cave below tiny with its torches' light map; and shoal, a small sea below tiny.
import { readdirSync, readFileSync } from 'node:fs';
import path from 'node:path';

export const TINY = { id: 'tiny', hash: 'fixture' } as const;
export const HOLLOW = { id: 'hollow', hash: 'fixture' } as const;
export const SHOAL = { id: 'shoal', hash: 'fixture' } as const;

const REGIONS = path.resolve(process.cwd(), 'src/testing/regions');

/** A file of test plan id's bake, by its path in the bake. */
export function fixtureFile(id: string, file: string): ArrayBuffer {
  const b = readFileSync(path.join(REGIONS, id, file));
  return b.buffer.slice(b.byteOffset, b.byteOffset + b.byteLength);
}

/** A file of the tiny bake, by its path in the bake. */
export function tinyFile(file: string): ArrayBuffer {
  return fixtureFile('tiny', file);
}

/** The paths of test plan id's chunks in its bake. */
export function fixtureChunks(id: string): string[] {
  return readdirSync(path.join(REGIONS, id, 'chunks')).map((f) => `chunks/${f}`);
}
