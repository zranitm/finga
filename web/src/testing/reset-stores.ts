// Back to the initial state of every store between test cases, so state
// never leaks from one to the next. Imported by tests only.
import { character } from '@/state/character';
import { chat } from '@/state/chat';
import { connection } from '@/state/connection';
import { inventory } from '@/state/inventory';
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
    connection,
    settings,
    ui,
    toasts,
  ]) {
    store.reset();
  }
}
