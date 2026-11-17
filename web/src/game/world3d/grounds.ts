// Each map's ground colour by its look key, shared by the 3D ground and the map's image from above.

const GROUNDS: Readonly<Record<string, number>> = {
  grass: 0x8cc265,
  sand: 0xe2cf9a,
  snow: 0xeef2f6,
  dirt: 0xa88b62,
  stone: 0x6f6a63,
  brick: 0x9c5d4b,
};
const GROUND_DEFAULT = 0x9a9a9a;

/** Ground too steep to walk keeps this share of its colour in linear light, so a rise reads whichever way it faces the sun. */
export const CLIFF = 0.6;

/** The ground's colour, 0xrrggbb sRGB, for a map's look key. */
export function groundOf(key: string): number {
  return GROUNDS[key] ?? GROUND_DEFAULT;
}
