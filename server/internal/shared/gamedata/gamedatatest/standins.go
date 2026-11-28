package gamedatatest

// StandIns are the small plans Shipped swaps in for the shipped regions, by their file in the data folder, and three land maps beside them: same ids, gates, start, waypoints, NPCs, bosses and the mobs of its camps, a fraction of the size, one oak standing alone in Oakshade for the walk test; the two places below are taller than 240 m so that a 120 m interest cell cannot hold one whole.
var StandIns = map[string]string{
	"regions/willowmere.yaml":    willowmere,
	"regions/greyfang_cave.yaml": greyfangCave,
	"regions/sunken_shrine.yaml": sunkenShrine,
	"regions/sunreach.yaml":      sunreach,
	"regions/spore_caves.yaml":   sporeCaves,
	"regions/ruin_crypt.yaml":    ruinCrypt,
	"regions/hoarfell.yaml":      hoarfell,
	"maps/crossroads.yaml":       crossroads,
	"maps/frost_trail.yaml":      frostTrail,
	"maps/icefall.yaml":          icefall,
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
  - {name: east_pass, at: [232, 198], radius: 1.6, to: {region: hoarfell, gate: west_pass}}
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

const hoarfell = `name: Hoarfell
size: [256, 256]
cell: 2
seed: 53
look: {sky: day}
ground: {noise: {scale: 60, amplitude: 0.5, octaves: 1}}
edge: {width: 16, height: [6, 10], biome: snow}
colour: {slope: 0.25, high: 100}
areas:
  - {id: emberhold, name: Emberhold, area: [[16, 16], [64, 16], [64, 64], [16, 64]], biome: snow, kind: town}
  - {id: frost_trail, name: Frost Trail, area: [[64, 16], [240, 16], [240, 100], [100, 100], [100, 240], [16, 240], [16, 64], [64, 64]], levels: [22, 24], biome: snow, kind: hunting, boss: {at: [50, 220], radius: 6, mob: avalanche_slime}}
  - {id: icefall, name: Icefall, area: [[100, 100], [240, 100], [240, 160], [172, 160], [172, 240], [100, 240]], levels: [25, 27], biome: snow, kind: hunting, boss: {at: [135, 225], radius: 6, mob: blizzard_wing}}
  - {id: last_hearth, name: Last Hearth, area: [[172, 160], [200, 160], [200, 192], [172, 192]], biome: snow, kind: town}
  - {id: glacier, name: Glacier Peak, area: [[200, 160], [240, 160], [240, 200], [200, 200], [200, 240], [172, 240], [172, 192], [200, 192]], levels: [28, 30], biome: snow, kind: hunting, boss: {at: [186, 225], radius: 6, mob: crystal_colossus}}
  - {id: fallen_keep, name: Fallen Keep, area: [[200, 200], [240, 200], [240, 240], [200, 240]], levels: [31, 33], biome: snow, kind: hunting, boss: {at: [226, 226], radius: 6, mob: shade_lord}}
camps:
  - {area: frost_trail, name: slimes, at: [160, 50], radius: 6, mob: frost_slime, max: 5, respawn: 30s}
  - {area: frost_trail, name: weasels, at: [210, 70], radius: 6, mob: snow_weasel, max: 3, respawn: 40s}
  - {area: icefall, name: icewings, at: [130, 130], radius: 6, mob: icewing, max: 5, respawn: 30s}
  - {area: icefall, name: snails, at: [200, 125], radius: 6, mob: glacier_snail, max: 3, respawn: 40s}
  - {area: glacier, name: crystal_slimes, at: [222, 178], radius: 6, mob: crystal_slime, max: 5, respawn: 30s}
  - {area: glacier, name: rime_rats, at: [186, 205], radius: 6, mob: rime_rat, max: 3, respawn: 40s}
  - {area: fallen_keep, name: bats, at: [218, 214], radius: 5, mob: shade_bat, max: 3, respawn: 30s}
  - {area: fallen_keep, name: gloom_slimes, at: [228, 206], radius: 4, mob: gloom_slime, max: 2, respawn: 40s}
waypoints:
  - {id: emberhold, name: Emberhold, at: [40, 36]}
  - {id: south_trail, name: South Trail, at: [100, 24]}
  - {id: frost_meadows, name: Frost Meadows, at: [200, 28]}
  - {id: east_snowfield, name: East Snowfield, at: [232, 90]}
  - {id: pinewood, name: Pinewood, at: [30, 120]}
  - {id: north_pines, name: North Pines, at: [30, 200]}
  - {id: icefall_steps, name: Icefall Steps, at: [110, 110]}
  - {id: frozen_shelf, name: Frozen Shelf, at: [232, 140]}
  - {id: north_ice, name: North Ice, at: [110, 180]}
  - {id: last_hearth, name: Last Hearth, at: [186, 176]}
  - {id: rime_ridge, name: Rime Ridge, at: [192, 168]}
  - {id: glacier_field, name: Glacier Field, at: [236, 152]}
  - {id: keep_gate, name: Keep Gate, at: [176, 164]}
npcs:
  - {id: mountain_guide, at: [30, 30], yaw: 90}
  - {id: hearth_keeper, at: [180, 170], yaw: 90}
gates:
  - {name: west_pass, at: [20, 40], radius: 1.6, to: {region: sunreach, gate: east_pass}}
start: {at: [40, 48], yaw: 90}
biomes:
  snow:
    colours: {flat: snow, slope: snow, steep: stone, high: snow}
`

// crossroads and frostTrail are the small land maps the world's tests walk and take portals between: the shipped game has none left but PvP's, so they exist in test data alone, with no guide and no way to a region.
const crossroads = `# Designed in 3D: a waystation village where three roads meet at a raised square round its well: the desert road from Sunreach's east pass west, and the frost trail climbing north.
name: Crossroads
kind: town
levels: [0, 0]
look: {ground: dirt, sky: dawn}
size: [52, 40]
terrain:
  cell: 0.5
  hills:
    - {at: [26, 44], radius: 12, height: 1.2}   # the rise the frost trail climbs, north
    - {at: [44, 6], radius: 5, height: 0.6}     # a sandy knoll by the dune road
  mesas:
    - {area: [20, 14, 12, 12], height: 0.4, edge: 2}   # the square, its edge a ramp walked up
solids:
  - {box: [12, 31, 11, 14.95], height: 14, yaw: 180, model: house_large}   # the waystation inn, its door south to the woods road
  - {box: [39, 31, 6.1, 7.4], height: 13.6, yaw: 180, model: house_small}
  - {box: [11, 8, 6.1, 7.4], height: 13.6, yaw: 0, model: house_small}     # its door north, to the road
  - {cylinder: [26, 20], radius: 1.35, height: 0.6, model: well}
  - {box: [22, 9.5, 2.4, 4.8], height: 4.2, yaw: 0, model: shop}
  - {box: [30, 9.5, 2.4, 4.8], height: 4.2, yaw: 0, model: shop}
  - {box: [33.6, 9.4, 0.64, 0.64], height: 0.64, yaw: 25, model: crate}
  - {box: [34.5, 10.3, 0.64, 0.64], height: 0.64, yaw: 70, model: crate}
  - {box: [17.8, 10.6, 0.64, 0.64], height: 0.64, yaw: 10, model: crate}
  - {box: [8.2, 22.7, 0.64, 0.64], height: 0.64, yaw: 15, model: crate}   # the inn's stores, by its door
  - {box: [9.0, 23.5, 0.64, 0.64], height: 0.64, yaw: 50, model: crate}
  - {cylinder: [4, 30], radius: 0.34, height: 6.5, model: tree_oak}
  - {cylinder: [5, 10], radius: 0.34, height: 6.5, model: tree_oak}
  - {cylinder: [19, 36.5], radius: 0.34, height: 6.5, model: tree_pine}
  - {cylinder: [33, 36.5], radius: 0.34, height: 6.5, model: tree_pine}
  - {cylinder: [46, 31], radius: 0.7, height: 1.13, model: rock_small}
  - {cylinder: [45, 11], radius: 0.7, height: 1.13, model: rock_small}
props:
  - {scatter: {model: grass_tuft, area: [0, 0, 24, 40], count: 70, seed: 41}}
  - {scatter: {model: bush_round, area: [0, 24, 6, 16], count: 6, seed: 42}}
  - {scatter: {model: small_mushrooms, area: [0, 0, 8, 16], count: 8, seed: 43}}
  - {scatter: {model: flowers, area: [18, 12, 16, 16], count: 10, seed: 44}}
  - {at: [23.4, 20], model: fountain_flowers}   # the beds round the well
  - {at: [28.6, 20], model: fountain_flowers}
  - {at: [26, 17.4], model: fountain_flowers}
  - {scatter: {model: bush_round, area: [34, 25, 10, 2], count: 3, seed: 48}}
  - {scatter: {model: bush_round, area: [7, 12.5, 9, 1.5], count: 3, seed: 49}}
  - {scatter: {model: pebbles, area: [38, 0, 14, 40], count: 22, seed: 45}}
  - {scatter: {model: dead_shrub, area: [44, 14, 8, 12], count: 3, seed: 46}}
  - {scatter: {model: tree_pine, area: [14, 37, 24, 3], count: 6, seed: 47}}
portals:
  - {name: to_frost, at: [26, 38], radius: 1.2, to: {map: frost_trail, portal: from_crossroads}}
player_spawn: {at: [17, 20], yaw: 90}
`

const frostTrail = `# Designed in 3D: a snow trail east between banks too steep to climb, from the crossroads' way in through the frost slimes' drifts and a stand of pines, up a long snow ramp past the weasels' rocks to the Avalanche Slime's open snowfield and the way up to the icefall.
name: Frost Trail
kind: hunting
town: crossroads
levels: [23, 24]
look: {ground: snow, sky: snow}
size: [80, 36]
terrain:
  cell: 0.5
  hills:
    # drifts: no slope steeper than a walk
    - {at: [23, 25], radius: 5.5, height: 1.6}
    - {at: [29, 9.5], radius: 5, height: 1.4}
    - {at: [66, 26], radius: 4.5, height: 1.2}
  mesas:
    # the banks: snow too steep to climb, pines on top
    - {area: [-5, 31, 90, 10], height: 3, edge: 0.5}
    - {area: [-5, -6, 90, 10], height: 3, edge: 0.5}
    # the ways in and out
    - {area: [-5, 4, 8, 10], height: 3, edge: 0.5}
    - {area: [-5, 22, 8, 9], height: 3, edge: 0.5}
    - {area: [77, 4, 8, 10], height: 3, edge: 0.5}
    - {area: [77, 22, 8, 9], height: 3, edge: 0.5}
    # the climb: the trail rises 2 m up a ramp 5 m long across the whole map
    - {area: [46, -20, 100, 76], height: 2, edge: 5}
solids:
  # rocks set into the banks, centred on the foot
  - {cylinder: [15, 30.5], radius: 2.33, height: 3.52, model: rock_wall}
  - {cylinder: [36, 4.5], radius: 2.33, height: 3.52, model: rock_wall}
  - {cylinder: [56, 30.5], radius: 2.33, height: 3.52, model: rock_wall}
  - {cylinder: [70, 4.5], radius: 2.33, height: 3.52, model: rock_wall}
  - {cylinder: [66, 30.5], radius: 2.33, height: 3.52, model: rock_wall}
  # the pines on the lower trail, in two stands, each 4 m or more from the next or hugging a bank
  - {cylinder: [11, 27], radius: 0.34, height: 6.5, model: tree_pine}
  - {cylinder: [15, 25], radius: 0.34, height: 6.5, model: tree_pine}
  - {cylinder: [11.5, 22], radius: 0.34, height: 6.5, model: tree_pine}
  - {cylinder: [26, 18], radius: 0.34, height: 6.5, model: tree_pine}
  - {cylinder: [30, 21.5], radius: 0.34, height: 6.5, model: tree_pine}
  - {cylinder: [34, 15.5], radius: 0.34, height: 6.5, model: tree_pine}
  - {cylinder: [30, 5], radius: 0.34, height: 6.5, model: tree_pine}
  - {cylinder: [24, 30], radius: 0.34, height: 6.5, model: tree_pine}
  # the weasels' rocks, on flat ground at the ramp's foot and top: past standing on, and 2.98 m or more from a bank, wider than a weasel or the boss
  - {cylinder: [36, 25], radius: 2.52, height: 2.31, model: boulder}
  - {cylinder: [50, 10.2], radius: 2.52, height: 2.31, model: boulder}
  # in the west, beyond every weasel's leash: two rocks to stand on
  - {cylinder: [9, 13], radius: 0.7, height: 1.13, model: rock_small}
  - {cylinder: [8, 24], radius: 0.7, height: 1.13, model: rock_small}
  # a touching pair on the snowfield's west side, 3 m or more outside the boss's circle
  - {cylinder: [51, 17], radius: 0.7, height: 1.13, model: rock_small}
  - {cylinder: [52.1, 17.9], radius: 0.7, height: 1.13, model: rock_small}
props:
  - {scatter: {model: pebbles, area: [3, 5, 74, 25], count: 40, seed: 51}}
  # pines and crags on the banks' tops, where nobody walks
  - {scatter: {model: tree_pine, area: [0, 31.6, 44, 4], count: 12, seed: 52}}
  - {scatter: {model: tree_pine, area: [0, 0.4, 44, 3.4], count: 9, seed: 53}}
  # bare trees on the upper banks, where the wind takes the needles
  - {scatter: {model: dead_tree, area: [44, 31.6, 36, 4], count: 9, seed: 54}}
  - {scatter: {model: dead_tree, area: [44, 0.4, 36, 3.4], count: 6, seed: 55}}
  - {at: [28, 33.5], model: crag}
  - {at: [47, 34], model: crag}
  - {at: [66, 33.5], model: crag}
  - {at: [18, 2], model: crag}
  - {at: [58, 1.5], model: crag}
portals:
  - {name: from_crossroads, at: [2, 18], radius: 1.2, to: {map: crossroads, portal: to_frost}}
spawns:
  - {name: west_slimes, at: [21, 9.5], radius: 4.5, mob: frost_slime, max: 2, respawn: 27s}
  - {name: pine_slimes, at: [31, 16], radius: 5, mob: frost_slime, max: 3, respawn: 27s}
  - {name: drift_slimes, at: [23, 25], radius: 4.5, mob: frost_slime, max: 2, respawn: 27s}
  - {name: field_slimes, at: [55, 8.5], radius: 3.5, mob: frost_slime, max: 2, respawn: 30s}
  - {name: bank_slimes, at: [52, 25.5], radius: 3.5, mob: frost_slime, max: 2, respawn: 30s}
  - {name: drift_east_slimes, at: [64, 25.5], radius: 3.5, mob: frost_slime, max: 2, respawn: 30s}
  - {name: rock_weasel, at: [41, 25], radius: 3, mob: snow_weasel, max: 1, respawn: 36s}
  - {name: ramp_weasel, at: [42, 9.5], radius: 3.5, mob: snow_weasel, max: 1, respawn: 36s}
  - {name: gate_weasel, at: [66, 9.5], radius: 3, mob: snow_weasel, max: 1, respawn: 36s}
player_spawn: {at: [4.5, 18], yaw: 90}
boss: {at: [61, 18], radius: 5, mob: avalanche_slime}
`

// icefall is frostTrail's twin, with the icefall's mobs, for the tests that need a second land map with a boss; it has no portals.
const icefall = `# Designed in 3D: a snow trail east between banks too steep to climb, from the crossroads' way in through the frost slimes' drifts and a stand of pines, up a long snow ramp past the weasels' rocks to the Avalanche Slime's open snowfield and the way up to the icefall.
name: Icefall
kind: hunting
town: crossroads
levels: [26, 27]
look: {ground: snow, sky: snow}
size: [80, 36]
terrain:
  cell: 0.5
  hills:
    # drifts: no slope steeper than a walk
    - {at: [23, 25], radius: 5.5, height: 1.6}
    - {at: [29, 9.5], radius: 5, height: 1.4}
    - {at: [66, 26], radius: 4.5, height: 1.2}
  mesas:
    # the banks: snow too steep to climb, pines on top
    - {area: [-5, 31, 90, 10], height: 3, edge: 0.5}
    - {area: [-5, -6, 90, 10], height: 3, edge: 0.5}
    # the ways in and out
    - {area: [-5, 4, 8, 10], height: 3, edge: 0.5}
    - {area: [-5, 22, 8, 9], height: 3, edge: 0.5}
    - {area: [77, 4, 8, 10], height: 3, edge: 0.5}
    - {area: [77, 22, 8, 9], height: 3, edge: 0.5}
    # the climb: the trail rises 2 m up a ramp 5 m long across the whole map
    - {area: [46, -20, 100, 76], height: 2, edge: 5}
solids:
  # rocks set into the banks, centred on the foot
  - {cylinder: [15, 30.5], radius: 2.33, height: 3.52, model: rock_wall}
  - {cylinder: [36, 4.5], radius: 2.33, height: 3.52, model: rock_wall}
  - {cylinder: [56, 30.5], radius: 2.33, height: 3.52, model: rock_wall}
  - {cylinder: [70, 4.5], radius: 2.33, height: 3.52, model: rock_wall}
  - {cylinder: [66, 30.5], radius: 2.33, height: 3.52, model: rock_wall}
  # the pines on the lower trail, in two stands, each 4 m or more from the next or hugging a bank
  - {cylinder: [11, 27], radius: 0.34, height: 6.5, model: tree_pine}
  - {cylinder: [15, 25], radius: 0.34, height: 6.5, model: tree_pine}
  - {cylinder: [11.5, 22], radius: 0.34, height: 6.5, model: tree_pine}
  - {cylinder: [26, 18], radius: 0.34, height: 6.5, model: tree_pine}
  - {cylinder: [30, 21.5], radius: 0.34, height: 6.5, model: tree_pine}
  - {cylinder: [34, 15.5], radius: 0.34, height: 6.5, model: tree_pine}
  - {cylinder: [30, 5], radius: 0.34, height: 6.5, model: tree_pine}
  - {cylinder: [24, 30], radius: 0.34, height: 6.5, model: tree_pine}
  # the weasels' rocks, on flat ground at the ramp's foot and top: past standing on, and 2.98 m or more from a bank, wider than a weasel or the boss
  - {cylinder: [36, 25], radius: 2.52, height: 2.31, model: boulder}
  - {cylinder: [50, 10.2], radius: 2.52, height: 2.31, model: boulder}
  # in the west, beyond every weasel's leash: two rocks to stand on
  - {cylinder: [9, 13], radius: 0.7, height: 1.13, model: rock_small}
  - {cylinder: [8, 24], radius: 0.7, height: 1.13, model: rock_small}
  # a touching pair on the snowfield's west side, 3 m or more outside the boss's circle
  - {cylinder: [51, 17], radius: 0.7, height: 1.13, model: rock_small}
  - {cylinder: [52.1, 17.9], radius: 0.7, height: 1.13, model: rock_small}
props:
  - {scatter: {model: pebbles, area: [3, 5, 74, 25], count: 40, seed: 51}}
  # pines and crags on the banks' tops, where nobody walks
  - {scatter: {model: tree_pine, area: [0, 31.6, 44, 4], count: 12, seed: 52}}
  - {scatter: {model: tree_pine, area: [0, 0.4, 44, 3.4], count: 9, seed: 53}}
  # bare trees on the upper banks, where the wind takes the needles
  - {scatter: {model: dead_tree, area: [44, 31.6, 36, 4], count: 9, seed: 54}}
  - {scatter: {model: dead_tree, area: [44, 0.4, 36, 3.4], count: 6, seed: 55}}
  - {at: [28, 33.5], model: crag}
  - {at: [47, 34], model: crag}
  - {at: [66, 33.5], model: crag}
  - {at: [18, 2], model: crag}
  - {at: [58, 1.5], model: crag}
spawns:
  - {name: west_slimes, at: [21, 9.5], radius: 4.5, mob: icewing, max: 2, respawn: 27s}
  - {name: pine_slimes, at: [31, 16], radius: 5, mob: icewing, max: 3, respawn: 27s}
  - {name: drift_slimes, at: [23, 25], radius: 4.5, mob: icewing, max: 2, respawn: 27s}
  - {name: field_slimes, at: [55, 8.5], radius: 3.5, mob: icewing, max: 2, respawn: 30s}
  - {name: bank_slimes, at: [52, 25.5], radius: 3.5, mob: icewing, max: 2, respawn: 30s}
  - {name: drift_east_slimes, at: [64, 25.5], radius: 3.5, mob: icewing, max: 2, respawn: 30s}
  - {name: rock_weasel, at: [41, 25], radius: 3, mob: glacier_snail, max: 1, respawn: 36s}
  - {name: ramp_weasel, at: [42, 9.5], radius: 3.5, mob: glacier_snail, max: 1, respawn: 36s}
  - {name: gate_weasel, at: [66, 9.5], radius: 3, mob: glacier_snail, max: 1, respawn: 36s}
player_spawn: {at: [4.5, 18], yaw: 90}
boss: {at: [61, 18], radius: 5, mob: blizzard_wing}
`
