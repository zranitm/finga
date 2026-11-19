// The party members a ticket goes with, as the queue tag and the offer card name them (pvp.md section 13).
import { character } from '@/state/character';
import { party } from '@/state/party';

const listed = new Intl.ListFormat('en', { type: 'conjunction' });

/** "with Robin and Jade": the ticket's other members by the names their party gives them; empty for one queued alone. */
export function useWith(members: readonly string[]): string {
  const me = character.use((s) => s.character?.id ?? '');
  const known = party.use((s) => s.party?.members);
  const names = members
    .filter((id) => id !== me)
    .map((id) => known?.find((m) => m.characterId === id)?.name)
    .filter((n): n is string => !!n);
  return names.length > 0 ? `with ${listed.format(names)}` : '';
}
