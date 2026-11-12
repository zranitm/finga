// The skill bar is saved a moment after the player's last change, so a run of drags makes one PUT; leaving the world saves what's pending at once.
import { RestError } from '@/net/rest';
import type { RestClient } from '@/net/rest';
import { skillBar } from '@/state/skill-bar';

export const SAVE_DELAY_MS = 800;

/** Saves the character's skill bar after its changes until the returned stop, which saves any change still waiting. */
export function watchSkillBar(
  rest: Pick<RestClient, 'putSkillBar'>,
  characterId: string,
  notice: (text: string) => void,
): () => void {
  let timer: ReturnType<typeof setTimeout> | null = null;
  const save = () => {
    timer = null;
    rest.putSkillBar(characterId, skillBar.getState().slots).catch((err: unknown) => {
      notice(err instanceof RestError ? err.message : 'Could not save the skill bar.');
    });
  };
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
    if (timer !== null) {
      clearTimeout(timer);
      save();
    }
  };
}
