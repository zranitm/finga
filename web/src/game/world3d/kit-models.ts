// The kit models solids and props draw with, by the model key a map names; a key with none draws a stand-in.

/** A model file under /models, scaled evenly and raised y metres. */
export interface KitModel {
  readonly file: string;
  readonly scale: number;
  readonly y: number;
}

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
  grass_tuft: { file: 'kaykit/Grass_1_A.glb', scale: 1, y: 0 },
  stump: { file: 'quaternius/TreeStump_Moss.glb', scale: 1, y: 0 },
  flowers: { file: 'quaternius/Flowers.glb', scale: 0.7, y: 0 },
  fountain_flowers: { file: 'quaternius/Flowers.glb', scale: 1.2, y: 0 },
  // The older Medieval Village Pack, at one scale that makes its doors 2.08 m.
  house_small: { file: 'quaternius/House_1.glb', scale: 4, y: 0 },
  house_large: { file: 'quaternius/Inn.glb', scale: 4, y: 0 },
  shop: { file: 'quaternius/MarketStand_1.glb', scale: 4, y: 0 },
  well: { file: 'quaternius/Well.glb', scale: 4, y: 0 },
  crate: { file: 'quaternius/Crate.glb', scale: 4, y: 0 },
};
