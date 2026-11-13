// The effects manifest: an optional sprite sheet and sound per effect id, paths under assets/, replacing drawn versions.
import * as v from 'valibot';

import type { EffectId } from './ids';

export const MANIFEST_FILE = 'assets/effects/manifest.json';

export interface Sheet {
  readonly file: string;
  readonly frameWidth: number;
  readonly frameHeight: number;
  readonly frames: number;
  readonly fps: number;
  /** The point of a frame that sits at the effect's (x, y), as shares of its width and height. */
  readonly anchor: readonly [number, number];
}

export interface Entry {
  readonly sheet?: Sheet;
  readonly sound?: string;
}

/** The entries the game can use, and why it refused the others. */
export interface Manifest {
  readonly entries: ReadonlyMap<EffectId, Entry>;
  readonly problems: readonly string[];
}

export const emptyManifest: Manifest = { entries: new Map(), problems: [] };

const file = v.pipe(v.string('is not a file path'), v.minLength(1, 'is not a file path'));
const positive = v.pipe(
  v.number('is not a number'),
  v.finite('is not a finite number'),
  v.gtValue(0, 'is not above 0'),
);
const share = v.pipe(
  v.number('is not a number'),
  v.minValue(0, 'is below 0'),
  v.maxValue(1, 'is above 1'),
);

const line = v.pipe(
  v.strictObject(
    {
      sheet: v.optional(file),
      frameWidth: v.optional(positive),
      frameHeight: v.optional(positive),
      frames: v.optional(v.pipe(positive, v.integer('is not a whole number'))),
      fps: v.optional(positive),
      anchor: v.optional(v.tuple([share, share], 'is not two shares from 0 to 1'), [0.5, 0.5]),
      sound: v.optional(file),
    },
    'is not an object',
  ),
  v.check(
    (e) => e.sheet !== undefined || e.sound !== undefined,
    'names neither a sheet nor a sound',
  ),
  v.check(
    (e) =>
      e.sheet === undefined ||
      [e.frameWidth, e.frameHeight, e.frames, e.fps].every((n) => n !== undefined),
    'has a sheet without its frameWidth, frameHeight, frames and fps',
  ),
);

/** An entry, or why it can't be used. */
function entryOf(raw: unknown): Entry | string {
  const parsed = v.safeParse(line, raw);
  if (!parsed.success) {
    const issue = parsed.issues[0];
    const at = v.getDotPath(issue);
    if (at && issue.type === 'strict_object') {
      return `has ${at}, not a field it knows`; // a typo, say
    }
    return at ? `${at} ${issue.message}` : issue.message;
  }
  const {
    sheet,
    frameWidth = 0,
    frameHeight = 0,
    frames = 0,
    fps = 0,
    anchor,
    sound,
  } = parsed.output;
  return {
    ...(sheet !== undefined && {
      sheet: { file: sheet, frameWidth, frameHeight, frames, fps, anchor },
    }),
    ...(sound !== undefined && { sound }),
  };
}

/** Reads the manifest, refusing an id that isn't one of known and an entry it can't use; refused lines are left to their drawn versions. */
export function parseManifest(raw: unknown, known: readonly EffectId[]): Manifest {
  if (typeof raw !== 'object' || raw === null || Array.isArray(raw)) {
    return { entries: new Map(), problems: ['the manifest is not an object of effect ids'] };
  }
  const ids = new Set<string>(known);
  const entries = new Map<EffectId, Entry>();
  const problems: string[] = [];
  for (const [id, e] of Object.entries(raw)) {
    if (!ids.has(id)) {
      problems.push(`"${id}" is not an effect id`);
      continue;
    }
    const entry = entryOf(e);
    if (typeof entry === 'string') {
      problems.push(`"${id}" ${entry}`);
      continue;
    }
    entries.set(id as EffectId, entry);
  }
  return { entries, problems };
}
