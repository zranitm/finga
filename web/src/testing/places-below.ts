// The game's maps with a place below the test region, which no shipped region has yet: The Deep, a cave under it reached by a gate at (60, 40) and left by one at (20, 64); and Next Region beside it, through a gate at (250, 128).
type Loose = Readonly<Record<string, unknown>>; // a map as data, past the generated file's literal types

const square = [
  [8, 8],
  [120, 8],
  [120, 120],
  [8, 120],
];
const area = (id: string, name: string, levels: readonly [number, number] | null) => ({
  id,
  name,
  kind: levels ? 'hunting' : 'town',
  levels,
  area: square,
  boss: null,
});
const gate = (name: string, x: number, z: number, to: string) => ({ name, x, z, radius: 2, to });

/** real's maps, the test region's gates added, The Deep and Next Region with it; for a vi.mock of '@/gamedata/maps'. */
export function belowTheTestRegion(real: unknown): { maps: readonly Loose[] } {
  const all = (real as { maps: readonly Loose[] }).maps;
  const test = all.find((m) => m.id === 'test_region')!;
  return {
    maps: [
      ...all.map((m) =>
        m === test
          ? {
              ...test,
              portals: [gate('deep', 60, 40, 'deep'), gate('east', 250, 128, 'next_region')],
            }
          : m,
      ),
      {
        ...test,
        id: 'deep',
        name: 'The Deep',
        sky: 'cave',
        under: 'test_region',
        spawns: [],
        portals: [gate('mouth', 20, 64, 'test_region')],
        areas: [
          area('camp', 'Camp', null),
          area('upper', 'Upper Halls', [3, 5]),
          area('lower', 'Lower Halls', [4, 7]),
        ],
      },
      {
        ...test,
        id: 'next_region',
        name: 'Next Region',
        under: null,
        spawns: [],
        portals: [gate('west', 6, 128, 'test_region')],
        areas: [],
      },
    ],
  };
}
