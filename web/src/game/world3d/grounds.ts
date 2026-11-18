// Each map's ground colour by its look key, shared by the 3D ground and the map's image from above: kit.yaml's grounds, through npm run gen.
import { grounds } from '@/gamedata/grounds';

const GROUNDS: Readonly<Record<string, number>> = grounds;
const GROUND_DEFAULT = 0x9a9a9a;

/** Ground too steep to walk keeps this share of its colour in linear light, so a rise reads whichever way it faces the sun. */
export const CLIFF = 0.6;

/** The ground's colour, 0xrrggbb sRGB, for a map's look key. */
export function groundOf(key: string): number {
  return GROUNDS[key] ?? GROUND_DEFAULT;
}
