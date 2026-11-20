// The arena build is saved as the skill bar is: a moment after the player's last change, and at once on leaving the world or closing the tab.
import { saveQueue } from '@/lib/save-queue';
import { RestError } from '@/net/rest';
import type { RestClient } from '@/net/rest';
import { arenaBuild } from '@/state/arena-build';

import { SAVE_DELAY_MS } from './saveSkillBar';

/** Saves the character's arena build after its changes until the returned stop, which saves any change still waiting and settles once every save has. */
export function watchArenaBuild(
  rest: Pick<RestClient, 'putArenaBuild'>,
  characterId: string,
  notice: (text: string) => void,
): () => Promise<void> {
  const queue = saveQueue(() => {
    const { bar, traits } = arenaBuild.getState(); // the build as it stood when its save was due
    return () =>
      rest.putArenaBuild(characterId, { skill_bar: bar ?? [], traits }).then(
        () => undefined,
        (err: unknown) => {
          notice(err instanceof RestError ? err.message : 'Could not save the arena build.');
        },
      );
  }, SAVE_DELAY_MS);
  window.addEventListener('pagehide', queue.flush);
  const off = arenaBuild.subscribe((now, before) => {
    if (now.edits !== before.edits) {
      queue.changed();
    }
  });
  return () => {
    off();
    window.removeEventListener('pagehide', queue.flush);
    queue.flush();
    return queue.settled();
  };
}
