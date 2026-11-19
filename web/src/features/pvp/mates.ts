// The party members a ticket goes with, as the queue tag and the offer card name them (pvp.md section 13).
import { character } from '@/state/character';
import { party } from '@/state/party';

const listed = new Intl.ListFormat('en', { type: 'conjunction' });

/** "Robin and Jade": the members by the names their party gives them, or "A party member" ("Party members") while it doesn't name them all. */
export function membersNamed(ids: readonly string[]): string {
  const known = party.getState().party?.members;
  const names = ids.map((id) => known?.find((m) => m.characterId === id)?.name);
  if (names.length > 0 && names.every((n) => n !== undefined)) {
    return listed.format(names);
  }
  return ids.length > 1 ? 'Party members' : 'A party member';
}

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
