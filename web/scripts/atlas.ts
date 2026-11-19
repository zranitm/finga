// A KayKit atlas as raw pixels: an 8 × 4 grid of swatches, each a gradient, which a colourway recolours and the client draws shrunk.
import { FACE_SWATCH } from '../src/game/world3d/outfit-looks.ts';

export interface Raw {
  readonly data: Uint8Array;
  readonly width: number;
  readonly height: number;
  readonly channels: number;
}

const COLUMNS = 8;
const ROWS = 4;

/** The swatch a pixel lies in, "column,row" from the top left. */
function cellAt(raw: Raw, x: number, y: number): string {
  return `${Math.floor((x * COLUMNS) / raw.width)},${Math.floor((y * ROWS) / raw.height)}`;
}

function rgbAt(raw: Raw, i: number): number {
  const d = raw.data;
  return (d[i]! << 16) | (d[i + 1]! << 8) | d[i + 2]!;
}

function lum(rgb: number): number {
  return 0.2126 * (rgb >> 16) + 0.7152 * ((rgb >> 8) & 0xff) + 0.0722 * (rgb & 0xff);
}

/** Calls f with each pixel's swatch and its first byte. */
function eachPixel(raw: Raw, f: (cell: string, i: number) => void): void {
  for (let y = 0; y < raw.height; y++) {
    for (let x = 0; x < raw.width; x++) {
      f(cellAt(raw, x, y), (y * raw.width + x) * raw.channels);
    }
  }
}

/** The swatches that are skin: the face's, and any sharing a colour with it, as a mage's hands do. */
export function skinSwatches(raw: Raw): Set<string> {
  const face = new Set<number>();
  eachPixel(raw, (cell, i) => {
    if (cell === FACE_SWATCH) face.add(rgbAt(raw, i));
  });
  const skin = new Set<string>();
  eachPixel(raw, (cell, i) => {
    if (face.has(rgbAt(raw, i))) skin.add(cell);
  });
  return skin;
}

/** A copy with each named swatch in its new colour: each pixel the colour scaled by its lightness against the swatch's mean, so the gradient stays. */
export function recolour(raw: Raw, swatches: Readonly<Record<string, number>>): Raw {
  const sum = new Map<string, { total: number; n: number }>();
  eachPixel(raw, (cell, i) => {
    if (swatches[cell] !== undefined) {
      const s = sum.get(cell) ?? { total: 0, n: 0 };
      s.total += lum(rgbAt(raw, i));
      s.n++;
      sum.set(cell, s);
    }
  });
  const data = Uint8Array.from(raw.data);
  eachPixel(raw, (cell, i) => {
    const to = swatches[cell];
    const s = sum.get(cell);
    if (to !== undefined && s) {
      const k = lum(rgbAt(raw, i)) / (s.total / s.n);
      data[i] = Math.min(255, Math.round((to >> 16) * k));
      data[i + 1] = Math.min(255, Math.round(((to >> 8) & 0xff) * k));
      data[i + 2] = Math.min(255, Math.round((to & 0xff) * k));
    }
  });
  return { ...raw, data };
}

/** The atlas shrunk to size a side, each pixel the mean of its block, so no swatch bleeds into the next. */
export function shrink(raw: Raw, size: number): Raw {
  const bx = raw.width / size;
  const by = raw.height / size;
  if (!Number.isInteger(bx) || !Number.isInteger(by) || size % COLUMNS || size % ROWS) {
    throw new Error(
      `atlas: ${raw.width}×${raw.height} doesn't shrink to whole swatches at ${size}`,
    );
  }
  const data = new Uint8Array(size * size * raw.channels);
  for (let y = 0; y < size; y++) {
    for (let x = 0; x < size; x++) {
      for (let c = 0; c < raw.channels; c++) {
        let total = 0;
        for (let j = 0; j < by; j++) {
          for (let i = 0; i < bx; i++) {
            total += raw.data[((y * by + j) * raw.width + x * bx + i) * raw.channels + c]!;
          }
        }
        data[(y * size + x) * raw.channels + c] = Math.round(total / (bx * by));
      }
    }
  }
  return { data, width: size, height: size, channels: raw.channels };
}

/** The swatches whose pixels differ between two atlases of one size. */
export function changedSwatches(a: Raw, b: Raw): Set<string> {
  const changed = new Set<string>();
  eachPixel(a, (cell, i) => {
    for (let c = 0; c < a.channels; c++) {
      if (a.data[i + c] !== b.data[i + c]) changed.add(cell);
    }
  });
  return changed;
}
