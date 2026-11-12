// Room for a reward, as the item service places it: a hand-in takes first, smallest stacks first, then each give fills its template's stacks and puts the rest in its tab's first free slot.
import { items } from '@/gamedata/items';
import { ERR } from '@/protocol/errors';
import type { ItemView } from '@/protocol/messages';
import { isTab, tabs } from '@/state/inventory';
import type { Slots, Tab } from '@/state/inventory';

/** Items of one template to place or take: what a quest takes or gives, a promotion's weapon, or a drop waiting for room. */
export interface Line {
  readonly template: string;
  readonly tab: Tab;
  readonly maxStack: number;
  readonly quantity: number;
}

/** The slots to free in each tab; a tab with none to free is left out. */
export type Shortfall = Partial<Record<Tab, number>>;

/** A line of a template this client knows; undefined for one it doesn't, which the server judges alone. */
export function lineOf(template: string, quantity: number): Line | undefined {
  const it = items.find((i) => i.id === template);
  return it && isTab(it.tab)
    ? { template, tab: it.tab, maxStack: it.max_stack, quantity }
    : undefined;
}

/** A promotion's gift, its class's first weapon: one Equip-tab item, whose template only the server knows. */
export const trainerWeapon: Line = { template: '', tab: 'equip', maxStack: 1, quantity: 1 };

/** Whether the gives fit after the takes now, as the item service judges a hand-in or a gift. */
export function fitsNow(slots: Slots, take: readonly Line[], give: readonly Line[]): boolean {
  return tabs.every((t) => fitsWith(slots[t], 0, [], inTab(take, t), inTab(give, t)));
}

/** The fewest slots to free in each tab so the gives fit once the drops waiting for room there have landed, as each takes a freed slot first; a tab short now, or the one a refusal named, needs at least one. */
export function toFree(
  slots: Slots,
  take: readonly Line[],
  give: readonly Line[],
  waiting: readonly Line[],
  refused?: Tab,
): Shortfall {
  const short: Shortfall = {};
  for (const t of tabs) {
    const [w, tk, g] = [inTab(waiting, t), inTab(take, t), inTab(give, t)];
    let n = t === refused || !fitsWith(slots[t], 0, [], tk, g) ? 1 : 0;
    // Each waiting or given line needs at most one slot of its own, so this ends.
    while (!fitsWith(slots[t], n, w, tk, g)) {
      n++;
    }
    if (n > 0) {
      short[t] = n;
    }
  }
  return short;
}

type Stack = { template: string; quantity: number } | null;

const inTab = (lines: readonly Line[], t: Tab) => lines.filter((l) => l.tab === t);

// One tab with extra free slots: the waiting drops that find room land, then the takes go, then every give must find room.
function fitsWith(
  slots: readonly (ItemView | null)[],
  extra: number,
  waiting: readonly Line[],
  take: readonly Line[],
  give: readonly Line[],
): boolean {
  const bag = [
    ...slots.map((it): Stack => (it ? { template: it.template, quantity: it.quantity } : null)),
    ...Array<Stack>(extra).fill(null),
  ];
  waiting.forEach((l) => place(bag, l));
  take.forEach((l) => takeOut(bag, l));
  return give.every((l) => place(bag, l));
}

// As the item service adds a line: its template's stacks first, the rest in the first free slot, or nothing when that has no room.
function place(bag: Stack[], l: Line): boolean {
  const stacks =
    l.maxStack > 1
      ? bag.filter(
          (s): s is NonNullable<Stack> => s?.template === l.template && s.quantity < l.maxStack,
        )
      : [];
  const room = stacks.reduce((n, s) => n + l.maxStack - s.quantity, 0);
  const free = bag.indexOf(null);
  if (room < l.quantity && free < 0) {
    return false;
  }
  let left = l.quantity;
  for (const s of stacks) {
    const add = Math.min(l.maxStack - s.quantity, left);
    s.quantity += add;
    left -= add;
  }
  if (left > 0) {
    bag[free] = { template: l.template, quantity: left };
  }
  return true;
}

// As a hand-in takes: the smallest stacks first, an emptied one freeing its slot.
function takeOut(bag: Stack[], l: Line) {
  const held = bag
    .map((s, k) => ({ s, k }))
    .filter((e): e is { s: NonNullable<Stack>; k: number } => e.s?.template === l.template)
    .sort((a, b) => a.s.quantity - b.s.quantity || a.k - b.k);
  let left = l.quantity;
  for (const { s, k } of held) {
    const n = Math.min(s.quantity, left);
    s.quantity -= n;
    left -= n;
    if (s.quantity === 0) {
      bag[k] = null;
    }
    if (left === 0) {
      break;
    }
  }
}

const tabNames: Record<Tab, string> = { equip: 'Equip', use: 'Use', etc: 'Etc' };

/** "Free 2 slots in your Equip tab and 1 in your Use tab to take this reward." */
export function freeText(short: Shortfall, what: string): string {
  const parts = tabs.flatMap((t) => {
    const n = short[t] ?? 0;
    return n > 0 ? [{ t, n }] : [];
  });
  const said = parts.map(({ t, n }, k) =>
    k === 0
      ? `${n} slot${n === 1 ? '' : 's'} in your ${tabNames[t]} tab`
      : `${n} in your ${tabNames[t]} tab`,
  );
  const listed =
    said.length > 1 ? `${said.slice(0, -1).join(', ')} and ${said.at(-1)!}` : (said[0] ?? '');
  return `Free ${listed} to take ${what}.`;
}

/** The tab a refusal says is full, from the item service's first short tab; undefined for any other refusal. */
export function fullTab(code: number | string): Tab | undefined {
  switch (code) {
    case ERR.EQUIP_TAB_FULL:
      return 'equip';
    case ERR.USE_TAB_FULL:
      return 'use';
    case ERR.ETC_TAB_FULL:
      return 'etc';
  }
  return undefined;
}
