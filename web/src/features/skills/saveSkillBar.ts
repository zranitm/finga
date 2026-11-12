// The skill bar is saved a moment after the player's last change, so a run of drags makes one PUT; leaving the world, or closing the tab, saves what's pending at once.
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
  let timer: ReturnType<typeof setTimeout> | null = null;
  let last: Promise<void> | null = null; // the save in flight, or the last one queued behind it
  // One save at a time, each of the bar as it stood when it was due, so the last change lands last.
  const save = () => {
    timer = null;
    const slots = skillBar.getState().slots;
    const put = () =>
      rest.putSkillBar(characterId, slots).then(
        () => undefined,
        (err: unknown) => {
          notice(err instanceof RestError ? err.message : 'Could not save the skill bar.');
        },
      );
    const mine = last === null ? put() : last.then(put);
    last = mine;
    void mine.then(() => {
      if (last === mine) last = null;
    });
  };
  const flush = () => {
    if (timer !== null) {
      clearTimeout(timer);
      save();
    }
  };
  window.addEventListener('pagehide', flush);
  const off = skillBar.subscribe((now, before) => {
    if (now.edits === before.edits) {
      return;
    }
    if (timer !== null) {
      clearTimeout(timer);
    }
    timer = setTimeout(save, SAVE_DELAY_MS);
  });
  return () => {
    off();
    window.removeEventListener('pagehide', flush);
    flush();
    return last ?? Promise.resolve();
  };
}
