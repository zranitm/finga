// The skill bar is saved a moment after the player's last change, so a run of drags makes one PUT; leaving the world, closing the tab or closing the skill window saves what's pending at once.
import { saveQueue } from '@/lib/save-queue';
import { RestError } from '@/net/rest';
import type { RestClient } from '@/net/rest';
import { skillBar } from '@/state/skill-bar';

export const SAVE_DELAY_MS = 800;

/** Saves the character's skill bar after its changes until the returned stop, which saves any change still waiting and settles once every save has. */
export function watchSkillBar(
  rest: Pick<RestClient, 'putSkillBar'>,
  characterId: string,
  notice: (text: string) => void,
): () => Promise<void> {
  const queue = saveQueue(() => {
    const slots = skillBar.getState().world; // the bar as it stood when its save was due, never a match's
    return () =>
      rest.putSkillBar(characterId, slots).then(
        () => undefined,
        (err: unknown) => {
          notice(err instanceof RestError ? err.message : 'Could not save the skill bar.');
        },
      );
  }, SAVE_DELAY_MS);
  window.addEventListener('pagehide', queue.flush);
  const off = skillBar.subscribe((now, before) => {
    if (now.edits !== before.edits) {
      queue.changed();
    }
    if (now.saveAsks !== before.saveAsks) {
      queue.flush();
    }
  });
  return () => {
    off();
    window.removeEventListener('pagehide', queue.flush);
    queue.flush();
    return queue.settled();
  };
}
