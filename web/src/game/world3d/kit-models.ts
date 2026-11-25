// The kit models solids and props draw with, by the model key a map names; a key with none draws a stand-in.

/** A model file under /models, scaled evenly and raised y metres. */
export interface KitModel {
  readonly file: string;
  readonly scale: number;
  readonly y: number;
}

/** KayKit Dungeon's torches and their flame, at the characters' scale (the Knight's 2.467 units drawn 1.75 m); each file has its cup's centre at its origin, so a torch stands raised to its flame, kit.yaml's light height. */
export const TORCH_SCALE = 0.71;

export const kitModels: Readonly<Record<string, KitModel>> = {
  tree_oak: { file: 'kaykit/Tree_2_C.glb', scale: 1, y: 0 },
  tree_pine: { file: 'kaykit/Tree_4_A.glb', scale: 1.3, y: 0 },
  rock_large: { file: 'kaykit/Rock_1_H.glb', scale: 1.6, y: 0 },
  boulder: { file: 'kaykit/Rock_3_K.glb', scale: 1.9, y: 0 },
  pillar: { file: 'kaykit/Rock_1_N.glb', scale: 1, y: 0 },
  rock_wall: { file: 'kaykit/Rock_1_M.glb', scale: 1, y: 0 },
  rock_small: { file: 'kaykit/Rock_1_E.glb', scale: 1, y: 0 },
  crag: { file: 'kaykit/Rock_3_Q.glb', scale: 1, y: 0 }, // half sunk: a prop on cliff tops, never a solid
  pebbles: { file: 'kaykit/Rock_2_B.glb', scale: 0.35, y: 0 },
  giant_mushroom: { file: 'quaternius/Mushroom_Common.glb', scale: 8, y: 0 },
  ring_mushroom: { file: 'quaternius/Mushroom_Common.glb', scale: 5.5, y: 0 },
  small_mushrooms: { file: 'quaternius/Mushroom_Common.glb', scale: 1.2, y: 0 }, // a prop
  tiny_mushrooms: { file: 'quaternius/Mushroom_Common.glb', scale: 0.7, y: 0 }, // a prop, under a shin
  bush_round: { file: 'kaykit/Bush_2_C.glb', scale: 1.4, y: 0 },
  dead_tree: { file: 'kaykit/Tree_Bare_2_B.glb', scale: 1.4, y: 0 }, // a prop, on banks
  dead_shrub: { file: 'kaykit/Tree_Bare_1_B.glb', scale: 1.2, y: 0 }, // a prop, on banks
  grass_tuft: { file: 'kaykit/Grass_1_A.glb', scale: 1, y: 0 },
  stump: { file: 'quaternius/TreeStump_Moss.glb', scale: 1, y: 0 },
  flowers: { file: 'quaternius/Flowers.glb', scale: 0.7, y: 0 },
  torch_mounted: { file: 'kaykit/torch_mounted.glb', scale: TORCH_SCALE, y: 2.2 }, // its bracket behind it, on the wall it faces away from
  torch_lit: { file: 'kaykit/torch_lit.glb', scale: TORCH_SCALE, y: 1.8 }, // on a stake to the ground
  fountain_flowers: { file: 'quaternius/Flowers.glb', scale: 1.2, y: 0 },
  // The desert's: KayKit's rocks recoloured to sandstone, at the grey ones' scales so every solid keeps its size, and Ultimate Nature's plants.
  sandstone_wall: { file: 'kaykit/Rock_1_M_Sand.glb', scale: 1, y: 0 },
  sandstone_pillar: { file: 'kaykit/Rock_1_N_Sand.glb', scale: 1, y: 0 },
  sandstone_boulder: { file: 'kaykit/Rock_3_K_Sand.glb', scale: 1.9, y: 0 },
  sandstone_crag: { file: 'kaykit/Rock_3_Q_Sand.glb', scale: 1, y: 0 }, // half sunk: a prop on cliff tops, never a solid
  sandstone_small: { file: 'kaykit/Rock_1_E_Sand.glb', scale: 1, y: 0 },
  sand_pebbles: { file: 'kaykit/Rock_2_B_Sand.glb', scale: 0.35, y: 0 },
  cactus_round: { file: 'quaternius/Cactus_1.glb', scale: 1, y: 0 },
  cactus_tall: { file: 'quaternius/Cactus_2.glb', scale: 1, y: 0 },
  cactus_small: { file: 'quaternius/Cactus_5.glb', scale: 1, y: 0 },
  cactus_flowering: { file: 'quaternius/CactusFlowers_2.glb', scale: 1, y: 0 },
  cactus_flowers: { file: 'quaternius/CactusFlower_1.glb', scale: 1, y: 0 }, // a prop, ground cover for the sand
  palm: { file: 'quaternius/PalmTree_1.glb', scale: 1, y: 0 },
  palm_short: { file: 'quaternius/PalmTree_4.glb', scale: 1, y: 0 },
  dry_tree: { file: 'quaternius/CommonTree_Dead_2.glb', scale: 1, y: 0 }, // a prop, on banks
  // The older Medieval Village Pack, at one scale that makes its doors 2.08 m.
  house_small: { file: 'quaternius/House_1.glb', scale: 4, y: 0 },
  house_large: { file: 'quaternius/Inn.glb', scale: 4, y: 0 },
  shop: { file: 'quaternius/MarketStand_1.glb', scale: 4, y: 0 },
  well: { file: 'quaternius/Well.glb', scale: 4, y: 0 },
  crate: { file: 'quaternius/Crate.glb', scale: 4, y: 0 },
};
