// The keys as the player has bound them, and each named as their keyboard prints it (key-bindings.md section 6).
import type { Action } from '@/input/actions';
import { keyOf, keyTable } from '@/input/bindings';
import type { KeyTable } from '@/input/bindings';
import { keyboardLayout } from '@/state/keyboard-layout';
import { settings } from '@/state/settings';

export interface Keys {
  readonly table: KeyTable;
  /** The name of the key an action takes. */
  readonly name: (action: Action) => string;
}

export function useKeys(): Keys {
  const table = keyTable(settings.use((s) => s.keys));
  const printed = keyboardLayout.use((s) => s.printed);
  return { table, name: (action) => keyOf(table, action, printed) };
}
