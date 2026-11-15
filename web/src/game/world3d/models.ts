// The glTF files the world draws, loaded into a scene once per file.
import type { AssetContainer } from '@babylonjs/core/assetContainer';
import { LoadAssetContainerAsync } from '@babylonjs/core/Loading/sceneLoader';
import type { Scene } from '@babylonjs/core/scene';
import { registerBuiltInLoaders } from '@babylonjs/loaders/dynamic';

/** Where a model file comes from: its URL in the page, its bytes in a test. */
export type ModelSource = (file: string) => string | ArrayBufferView;

let loadersRegistered = false;

/** Loads files into scene from source, each once however often it's asked for. */
export class Models {
  private readonly containers = new Map<string, Promise<AssetContainer>>();

  constructor(
    private readonly scene: Scene,
    private readonly source: ModelSource,
  ) {
    if (!loadersRegistered) {
      registerBuiltInLoaders();
      loadersRegistered = true;
    }
  }

  load(file: string): Promise<AssetContainer> {
    let c = this.containers.get(file);
    if (!c) {
      c = LoadAssetContainerAsync(this.source(file), this.scene, {
        pluginExtension: '.glb',
        // Quaternius's skins name a joint that isn't their joints' common root; without this Babylon skins them lying down, 100 times over.
        pluginOptions: { gltf: { alwaysComputeSkeletonRootNode: true } },
      });
      this.containers.set(file, c);
    }
    return c;
  }
}
