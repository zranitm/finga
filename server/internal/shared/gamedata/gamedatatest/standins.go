package gamedatatest

// StandIns are the small plans Shipped swaps in for the shipped regions, by their file in the data folder: same ids, gates, start, waypoints, NPCs, bosses and the mobs of its camps, a fraction of the size, one oak standing alone in Oakshade for the walk test; the two places below are taller than 240 m so that a 120 m interest cell cannot hold one whole.
var StandIns = map[string]string{
	"regions/willowmere.yaml":    willowmere,
	"regions/greyfang_cave.yaml": greyfangCave,
	"regions/sunken_shrine.yaml": sunkenShrine,
	"regions/sunreach.yaml":      sunreach,
	"regions/spore_caves.yaml":   sporeCaves,
	"regions/ruin_crypt.yaml":    ruinCrypt,
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
  - {id: sunmeadow, name: Sunmeadow, area: [[96, 16], [240, 16], [240, 96], [96, 96]], levels: [1, 4], biome: meadow, kind: hunting, boss: {at: [225, 40], radius: 6, mob: king_slime}}
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
pieces:
  - {model: tree_oak, at: [80, 110], yaw: 0}
gates:
  - {name: north_pass, at: [232, 215], radius: 1.6, to: {region: sunreach, gate: south_gate}}
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

const sunreach = `name: Sunreach
size: [256, 256]
cell: 2
seed: 31
look: {sky: day}
ground: {noise: {scale: 60, amplitude: 0.5, octaves: 1}}
edge: {width: 16, height: [6, 10], biome: dunes}
colour: {slope: 0.25, high: 100}
areas:
  - {id: sporewood, name: Sporewood Gorge, area: [[16, 16], [112, 16], [112, 100], [16, 100]], levels: [12, 13], biome: gorge, kind: hunting, boss: {at: [90, 85], radius: 6, mob: spore_mother}}
  - {id: ledges, name: Toadstool Ledges, area: [[16, 100], [100, 100], [100, 180], [16, 180]], levels: [16, 17], biome: gorge, kind: hunting, boss: {at: [80, 140], radius: 6, mob: lantern_slime}}
  - {id: palmwater, name: Palmwater, area: [[100, 100], [156, 100], [156, 180], [100, 180]], biome: dunes, kind: town}
  - {id: dunes, name: Amber Dunes, area: [[112, 16], [240, 16], [240, 180], [156, 180], [156, 100], [112, 100]], levels: [18, 19], biome: dunes, kind: hunting, boss: {at: [225, 40], radius: 6, mob: dune_baron}}
  - {id: sandstone, name: Sandstone Cliffs, area: [[16, 180], [240, 180], [240, 240], [16, 240]], levels: [20, 21], biome: dunes, kind: hunting, boss: {at: [200, 220], radius: 6, mob: cliff_reaper}}
camps:
  - {area: sporewood, name: slimes, at: [70, 60], radius: 6, mob: spore_slime, max: 5, respawn: 30s}
  - {area: sporewood, name: mossbacks, at: [50, 85], radius: 6, mob: mossback_snail, max: 3, respawn: 40s}
  - {area: ledges, name: glow, at: [50, 135], radius: 6, mob: glow_slime, max: 5, respawn: 30s}
  - {area: ledges, name: elders, at: [40, 160], radius: 6, mob: elder_snail, max: 3, respawn: 40s}
  - {area: dunes, name: mice, at: [150, 60], radius: 6, mob: sand_mouse, max: 5, respawn: 30s}
  - {area: dunes, name: scarabs, at: [200, 50], radius: 6, mob: shellback_scarab, max: 3, respawn: 40s}
  - {area: sandstone, name: vultures, at: [60, 220], radius: 6, mob: dust_vulture, max: 5, respawn: 30s}
  - {area: sandstone, name: sun_slimes, at: [160, 225], radius: 6, mob: sun_slime, max: 3, respawn: 40s}
waypoints:
  - {id: gorge_mouth, name: Gorge Mouth, at: [40, 24]}
  - {id: sporewood, name: Sporewood, at: [100, 30]}
  - {id: hollow_mouth, name: Hollow Mouth, at: [24, 70]}
  - {id: palmwater, name: Palmwater, at: [128, 112]}
  - {id: ledges, name: Toadstool Ledges, at: [30, 108]}
  - {id: ledges_north, name: Ledges North, at: [92, 174]}
  - {id: oasis_shore, name: Oasis Shore, at: [170, 110]}
  - {id: south_dunes, name: South Dunes, at: [130, 24]}
  - {id: east_dunes, name: East Dunes, at: [232, 92]}
  - {id: cliff_road, name: Cliff Road, at: [100, 190]}
  - {id: mesa_top, name: Mesa Top, at: [100, 235]}
  - {id: west_mesa, name: West Mesa, at: [30, 190]}
  - {id: ruins_stair, name: Ruins Stair, at: [140, 190]}
  - {id: east_pass, name: East Pass, at: [225, 190]}
npcs:
  - {id: oasis_guide, at: [120, 130], yaw: 90}
gates:
  - {name: south_gate, at: [40, 18], radius: 1.6, to: {region: willowmere, gate: north_pass}}
  - {name: hollow_mouth, at: [20, 62], radius: 1.6, to: {region: spore_caves, gate: mouth}}
  - {name: ruins_stair, at: [140, 198], radius: 1.6, to: {region: ruin_crypt, gate: stair}}
  - {name: east_pass, at: [232, 198], radius: 1.6, to: {region: crossroads, gate: from_desert}}
start: {at: [128, 130], yaw: 0}
biomes:
  gorge:
    colours: {flat: grass, slope: dirt, steep: stone, high: stone}
  dunes:
    colours: {flat: sand, slope: sand, steep: stone, high: sand}
`

const sporeCaves = `name: Spore Hollow
under: sunreach
size: [128, 256]
cell: 2
seed: 43
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
  - {id: spore_caves, name: Spore Hollow, area: [[8, 8], [120, 8], [120, 248], [8, 248]], levels: [14, 15], biome: cave, kind: hunting,
     boss: {at: [102, 64], radius: 5, mob: moth_empress}}
camps:
  - {area: spore_caves, name: moths, at: [88, 84], radius: 4, mob: dust_moth, max: 3, respawn: 36s}
  - {area: spore_caves, name: rats, at: [88, 44], radius: 4, mob: toadstool_rat, max: 3, respawn: 45s}
waypoints:
  - {id: spore_caves, name: Spore Hollow, at: [30, 64]}
gates:
  - {name: mouth, at: [22, 64], radius: 1.6, to: {region: sunreach, gate: hollow_mouth}}
start: {at: [26, 64], yaw: 90}
biomes:
  cave:
    colours: {flat: cave_floor, slope: cave_floor, steep: cave_rock, high: cave_rock}
  rock:
    colours: {flat: cave_rock, slope: cave_rock, steep: cave_rock, high: cave_rock}
`

const ruinCrypt = `name: Sunken Ruins
under: sunreach
size: [128, 256]
cell: 2
seed: 47
look: {sky: cave}
ground: {noise: {scale: 40, amplitude: 0.5, octaves: 1}}
edge: {width: 8, height: [2, 4], biome: rock}
colour: {slope: 0.25, high: 100}
rock: {height: 5, rough: 1}
features:
  - {hall: {area: [[16, 52], [56, 52], [56, 76], [16, 76]], flat: true}}
  - {tunnel: {path: [[52, 64], [76, 64]], width: 8}}
  - {hall: {area: [[72, 32], [112, 32], [112, 96], [72, 96]], flat: true}}
areas:
  - {id: ruin_crypt, name: Sunken Ruins, area: [[8, 8], [120, 8], [120, 248], [8, 248]], levels: [21, 22], biome: crypt, kind: hunting,
     boss: {at: [102, 64], radius: 6, mob: crypt_warden}}
camps:
  - {area: ruin_crypt, name: rats, at: [88, 84], radius: 4, mob: tomb_rat, max: 3, respawn: 36s}
  - {area: ruin_crypt, name: wisps, at: [88, 44], radius: 4, mob: ruin_wisp, max: 3, respawn: 45s}
waypoints:
  - {id: ruin_crypt, name: Sunken Ruins, at: [30, 64]}
gates:
  - {name: stair, at: [22, 64], radius: 1.6, to: {region: sunreach, gate: ruins_stair}}
start: {at: [26, 64], yaw: 90}
biomes:
  crypt:
    colours: {flat: sand, slope: sand, steep: cave_rock, high: cave_rock}
  rock:
    colours: {flat: cave_rock, slope: cave_rock, steep: cave_rock, high: cave_rock}
`
