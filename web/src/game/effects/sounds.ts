// The effects' sounds (combat-effects.md section 3): Babylon's audio engine, made with the first sound listed, and one sound per effect that lists one.
import { CreateSoundAsync } from '@babylonjs/core/AudioV2/abstractAudio/audioEngineV2';
import { CreateAudioEngineAsync } from '@babylonjs/core/AudioV2/webAudio/webAudioEngine';

import type { EffectId } from './ids';
import { warnOnce } from './warn';

/** Loads each listed sound into `into` as it comes in; one that fails is left out and plays silent. */
export async function loadSounds(
  listed: readonly (readonly [EffectId, string])[],
  into: Map<EffectId, { play(): void }>,
): Promise<void> {
  const engine = await CreateAudioEngineAsync({ disableDefaultUI: true });
  await Promise.all(
    listed.map(async ([id, file]) => {
      try {
        into.set(
          id,
          await CreateSoundAsync(id, `${import.meta.env.BASE_URL}assets/${file}`, {}, engine),
        );
      } catch (err) {
        warnOnce(`effect ${id}: ${file} did not load (${String(err)}), so it plays silent`);
      }
    }),
  );
}
