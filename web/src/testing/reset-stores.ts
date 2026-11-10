import { character } from '@/state/character';
import { chat } from '@/state/chat';
import { combatLog } from '@/state/combat-log';
import { connection } from '@/state/connection';
import { inventory } from '@/state/inventory';
import { nearby } from '@/state/nearby';
import { party } from '@/state/party';
import { session } from '@/state/session';
import { settings } from '@/state/settings';
import { toasts } from '@/state/toasts';
import { ui } from '@/state/ui';

export function resetStores(): void {
  for (const store of [
    session,
    character,
    inventory,
    party,
    chat,
    combatLog,
    connection,
    settings,
    ui,
    toasts,
    nearby,
  ]) {
    store.reset();
  }
}
