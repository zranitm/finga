import { arenaBuild } from '@/state/arena-build';
import { character } from '@/state/character';
import { chat } from '@/state/chat';
import { combat } from '@/state/combat';
import { combatLog } from '@/state/combat-log';
import { connection } from '@/state/connection';
import { energy } from '@/state/energy';
import { fieldBoss } from '@/state/field-boss';
import { fighters } from '@/state/fighters';
import { inventory } from '@/state/inventory';
import { keyboardLayout } from '@/state/keyboard-layout';
import { mapView } from '@/state/map-view';
import { nearby } from '@/state/nearby';
import { party } from '@/state/party';
import { pouches } from '@/state/pouches';
import { pvp } from '@/state/pvp';
import { pvpProfile } from '@/state/pvp-profile';
import { quests } from '@/state/quests';
import { session } from '@/state/session';
import { settings } from '@/state/settings';
import { skillBar } from '@/state/skill-bar';
import { statuses } from '@/state/statuses';
import { target } from '@/state/target';
import { teleport } from '@/state/teleport';
import { toasts } from '@/state/toasts';
import { ui } from '@/state/ui';
import { wardrobe } from '@/state/wardrobe';
import { waypoints } from '@/state/waypoints';

export function resetStores(): void {
  for (const store of [
    session,
    character,
    energy,
    inventory,
    party,
    quests,
    chat,
    combatLog,
    combat,
    connection,
    settings,
    keyboardLayout,
    ui,
    toasts,
    nearby,
    skillBar,
    fieldBoss,
    target,
    statuses,
    mapView,
    teleport,
    pvp,
    fighters,
    pouches,
    arenaBuild,
    pvpProfile,
    waypoints,
    wardrobe,
  ]) {
    store.reset();
  }
}
