// The kit models solids and props draw with, by the model key a map names; a key with none draws a stand-in.

/** A model file under /models, scaled evenly and raised y metres. */
export interface KitModel {
  readonly file: string;
  readonly scale: number;
  readonly y: number;
}

export const kitModels: Readonly<Record<string, KitModel>> = {};
