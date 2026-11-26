package gamedatatest

// StandIns are the small plans Shipped swaps in for the shipped regions, by their file in the data folder: same ids, gates, start, waypoints, NPCs and bosses, a fraction of the size; the two places below are taller than 240 m so that a 120 m interest cell cannot hold one whole.
var StandIns = map[string]string{
	"regions/willowmere.yaml":    willowmere,
	"regions/greyfang_cave.yaml": greyfangCave,
	"regions/sunken_shrine.yaml": sunkenShrine,
}

const willowmere = `name: Willowmere
size: [256, 256]
cell: 2
seed: 23
look: {sky: day}
ground: {noise: {scale: 60, amplitude: 0.5, octaves: 1}}
edge: {width: 16, height: [6, 10], biome: meadow}
colour: {slope: 0.25, high: 100}
areas:
  - {id: willowbrook, name: Willowbrook, area: [[16, 16], [96, 16], [96, 80], [16, 80]], biome: meadow, kind: town}
  - {id: sunmeadow, name: Sunmeadow, area: [[96, 16], [240, 16], [240, 96], [96, 96]], levels: [1, 4], biome: meadow, kind: hunting, boss: {at: [225, 30], radius: 6, mob: king_slime}}
  - {id: oakshade, name: Oakshade Woods, area: [[16, 80], [96, 80], [96, 176], [16, 176]], levels: [4, 7], biome: meadow, kind: hunting, boss: {at: [80, 160], radius: 6, mob: oakshade_chief}}
  - {id: toadcap, name: Toadcap Glade, area: [[16, 176], [96, 176], [96, 240], [16, 240]], levels: [6, 9], biome: meadow, kind: hunting, boss: {at: [80, 232], radius: 6, mob: mushroom_king}}
  - {id: mirrorlake, name: Mirrorlake Shore, area: [[96, 96], [240, 96], [240, 176], [96, 176]], levels: [8, 11], biome: meadow, kind: hunting, boss: {at: [225, 130], radius: 6, mob: lake_lurker}}
  - {id: greyfang, name: Greyfang Cliffs, area: [[96, 176], [240, 176], [240, 240], [96, 240]], levels: [10, 12], biome: meadow, kind: hunting, boss: {at: [200, 190], radius: 6, mob: crag_king}}
camps:
  - {area: sunmeadow, name: slimes, at: [140, 80], radius: 6, mob: green_slime, max: 5, respawn: 24s}
  - {area: sunmeadow, name: mushrooms, at: [215, 80], radius: 6, mob: orange_mushroom, max: 4, respawn: 36s}
  - {area: oakshade, name: woods, at: [30, 150], radius: 6, mob: oak_birb, max: 5, respawn: 30s}
  - {area: toadcap, name: caps, at: [75, 200], radius: 6, mob: puffcap, max: 5, respawn: 30s}
  - {area: mirrorlake, name: shore, at: [180, 135], radius: 6, mob: glub, max: 5, respawn: 30s}
  - {area: greyfang, name: foot, at: [150, 205], radius: 6, mob: cliff_orc, max: 5, respawn: 30s}
waypoints:
  - {id: willowbrook, name: Willowbrook, at: [56, 24]}
  - {id: sunmeadow, name: Sunmeadow, at: [110, 40]}
  - {id: sunmeadow_east, name: Sunmeadow East, at: [180, 45]}
  - {id: oakshade, name: Oakshade Woods, at: [40, 95]}
  - {id: oakshade_north, name: Oakshade North, at: [60, 130]}
  - {id: fernbridge, name: Fernbridge, at: [110, 110]}
  - {id: toadcap, name: Toadcap Glade, at: [30, 185]}
  - {id: toadcap_north, name: Toadcap North, at: [50, 230]}
  - {id: mirror_landing, name: Lake Landing, at: [140, 110]}
  - {id: mirror_north, name: North Shore, at: [200, 160]}
  - {id: greyfang_foot, name: Cliff Foot, at: [110, 185]}
  - {id: greyfang_top, name: Greyfang Top, at: [160, 235]}
  - {id: north_pass, name: North Pass, at: [225, 215]}
npcs:
  - {id: town_guide, at: [48, 40], yaw: 90}
  - {id: warrior_trainer, at: [64, 30], yaw: 0}
  - {id: magician_trainer, at: [66, 50], yaw: 270}
  - {id: bowman_trainer, at: [46, 62], yaw: 0}
gates:
  - {name: north_pass, at: [232, 215], radius: 1.6, to: {region: shroom_grove, gate: from_cave}}
  - {name: cave_mouth, at: [125, 195], radius: 1.6, to: {region: greyfang_cave, gate: mouth}}
  - {name: dive_spot, at: [130, 130], radius: 1.6, to: {region: sunken_shrine, gate: shore}}
start: {at: [56, 40], yaw: 0}
beginning: true
biomes:
  meadow:
    colours: {flat: grass, slope: dirt, steep: stone, high: stone}
`

const greyfangCave = `name: Greyfang Cave
under: willowmere
size: [128, 256]
cell: 2
seed: 41
look: {sky: cave}
ground: {noise: {scale: 40, amplitude: 0.5, octaves: 1}}
edge: {width: 8, height: [2, 4], biome: rock}
colour: {slope: 0.25, high: 100}
rock: {height: 4.5, rough: 1}
features:
  - {hall: {area: [[16, 52], [56, 52], [56, 76], [16, 76]], flat: true}}
  - {tunnel: {path: [[52, 64], [76, 64]], width: 8}}
  - {hall: {area: [[72, 32], [112, 32], [112, 96], [72, 96]], flat: true}}
areas:
  - {id: greyfang_cave, name: Greyfang Cave, area: [[8, 8], [120, 8], [120, 248], [8, 248]], levels: [10, 12], biome: cave, kind: hunting,
     boss: {at: [102, 64], radius: 5, mob: vampire_bat}}
camps:
  - {area: greyfang_cave, name: boars, at: [88, 84], radius: 4, mob: wild_boar, max: 3, respawn: 36s}
  - {area: greyfang_cave, name: bats, at: [88, 44], radius: 4, mob: cave_bat, max: 3, respawn: 45s}
waypoints:
  - {id: greyfang_cave, name: Greyfang Cave, at: [30, 64]}
npcs:
  - {id: thief_trainer, at: [44, 70], yaw: 270}
gates:
  - {name: mouth, at: [22, 64], radius: 1.6, to: {region: willowmere, gate: cave_mouth}}
start: {at: [26, 64], yaw: 90}
biomes:
  cave:
    colours: {flat: cave_floor, slope: cave_floor, steep: cave_rock, high: cave_rock}
  rock:
    colours: {flat: cave_rock, slope: cave_rock, steep: cave_rock, high: cave_rock}
`

const sunkenShrine = `name: Sunken Shrine
under: willowmere
size: [128, 256]
cell: 2
seed: 43
look: {sky: sea}
ground: {noise: {scale: 40, amplitude: 0.5, octaves: 1}}
edge: {width: 8, height: [6, 10], biome: reef_wall}
colour: {slope: 0.25, high: 100}
areas:
  - {id: sunken_shrine, name: Sunken Shrine, area: [[8, 8], [120, 8], [120, 248], [8, 248]], levels: [9, 11], biome: seabed, kind: hunting,
     boss: {at: [100, 92], radius: 6, mob: anglerfish}}
camps:
  - {area: sunken_shrine, name: piranhas, at: [70, 30], radius: 5, mob: piranha, max: 4, respawn: 30s}
  - {area: sunken_shrine, name: lionfish, at: [70, 98], radius: 5, mob: lionfish, max: 3, respawn: 36s}
  - {area: sunken_shrine, name: swordfish, at: [70, 64], radius: 5, mob: swordfish, max: 3, respawn: 36s}
  - {area: sunken_shrine, name: sharks, at: [100, 34], radius: 5, mob: goblin_shark, max: 2, respawn: 45s}
waypoints:
  - {id: sunken_shrine, name: Sunken Shrine, at: [30, 64]}
gates:
  - {name: shore, at: [20, 64], radius: 1.6, to: {region: willowmere, gate: dive_spot}}
start: {at: [24, 64], yaw: 90}
biomes:
  seabed:
    colours: {flat: sea_sand, slope: sea_sand, steep: sea_rock, high: sea_rock}
  reef_wall:
    colours: {flat: sea_rock, slope: sea_rock, steep: sea_rock, high: sea_rock}
`
