// The glTF files the world draws, loaded into a scene once per file.
import type { AssetContainer } from '@babylonjs/core/assetContainer';
import { LoadAssetContainerAsync } from '@babylonjs/core/Loading/sceneLoader';
import type { Scene } from '@babylonjs/core/scene';
import { registerBuiltInLoaders } from '@babylonjs/loaders/dynamic';
import type { IGLTFLoaderData } from '@babylonjs/loaders/glTF/glTFFileLoader';

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

  /** The file's container; a load that failed is forgotten, so the next asks again. */
  load(file: string): Promise<AssetContainer> {
    let c = this.containers.get(file);
    if (!c) {
      c = LoadAssetContainerAsync(this.source(file), this.scene, {
        pluginExtension: '.glb',
        // Quaternius's skins name a joint that isn't their joints' common root; without this Babylon skins them lying down, 100 times over.
        pluginOptions: {
          gltf: {
            alwaysComputeSkeletonRootNode: true,
            onParsed: (data: IGLTFLoaderData) => unnameStrayRoots(data.json),
          },
        },
      });
      this.containers.set(file, c);
      c.catch(() => this.containers.delete(file));
    }
    return c;
  }
}

interface SkinsJson {
  readonly nodes?: readonly { readonly children?: readonly number[] }[];
  readonly skins?: { skeleton?: number; readonly joints: readonly number[] }[];
}

/** Forgets the root a skin names when it isn't above all its joints, so Babylon finds their common root itself, as it would anyway, without a warning each load. */
function unnameStrayRoots(json: object): void {
  const { nodes = [], skins = [] } = json as SkinsJson;
  const parent = new Map<number, number>();
  nodes.forEach((n, i) => n.children?.forEach((c) => parent.set(c, i)));
  const above = (root: number, j: number | undefined): boolean =>
    j !== undefined && (j === root || above(root, parent.get(j)));
  for (const skin of skins) {
    const root = skin.skeleton;
    if (root !== undefined && !skin.joints.every((j) => above(root, j))) {
      delete skin.skeleton;
    }
  }
}
