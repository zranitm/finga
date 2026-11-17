// The abnormal statuses of server/data/statuses.yaml, as the world runs them.
import { statuses as data } from '@/gamedata/statuses';

/** A status: its group, what it blocks, its factors (1 when it has none), and damage over time's tick. */
export interface Status {
  readonly id: string;
  readonly group: string;
  readonly blocks: readonly string[];
  readonly move_speed: number;
  readonly accuracy: number;
  readonly attack: number;
  readonly physical_defense: number;
  readonly magic_defense: number;
  /** The next hit that lands ends it, times this; 0 for none. */
  readonly shatter: number;
  /** Any damage ends it. */
  readonly wakes: boolean;
  /** Damage over time: how often it ticks; 0 for other statuses. */
  readonly every_ms: number;
  /** Damage over time: a tick's factor when the body moved since the last. */
  readonly moving: number;
}

const byId: ReadonlyMap<string, Status> = new Map(data.map((s: Status) => [s.id, s]));

/** The status with id, if the data has it. */
export function statusOf(id: string): Status | undefined {
  return byId.get(id);
}

/** A status on a unit as the world sends it: its id and the tick it ends on. */
export interface StatusOn {
  readonly id: string;
  readonly ends: number;
}

/** What statuses still running at tick keep their body from: acting at all, or using skills. */
export function blocked(on: readonly StatusOn[], tick: number): { act: boolean; skills: boolean } {
  const running = on.filter((s) => s.ends > tick).map((s) => statusOf(s.id)?.blocks ?? []);
  return {
    act: running.some((b) => b.includes('act')),
    skills: running.some((b) => b.includes('skills')),
  };
}

// Each status's colour, shared by its mark, its icon, its damage-over-time numbers and the telegraphs of the mob attacks that inflict it.
const colors: Readonly<Record<string, number>> = {
  stun: 0xffd84a,
  freeze: 0x8fdcff,
  sleep: 0xb9a6ff,
  knockdown: 0xffb347,
  pin: 0xa8813f,
  slow: 0x5b8cff,
  silence: 0xc77dff,
  blind: 0x8a7fa0,
  poison: 0x7ed957,
  burn: 0xff7a1a,
  bleed: 0xe0303a,
  armor_break: 0xc9a66b,
  weaken: 0xa0a0b0,
  taunt: 0xff3b3b,
  lose_sight: 0x9aa0a6,
};
const OTHER = 0x6b8cff; // a status this client doesn't know: a status skill's colour

// Each status's icon from game-icons.net, author/name under public/icons/statuses (statuses.md section 6).
export const statusIcons: Readonly<Record<string, string>> = {
  stun: 'delapouite/knocked-out-stars',
  freeze: 'lorc/frozen-block',
  sleep: 'lorc/sleepy',
  knockdown: 'delapouite/half-body-crawling',
  pin: 'lorc/root-tip',
  slow: 'lorc/snail',
  silence: 'delapouite/silenced',
  blind: 'delapouite/blindfold',
  poison: 'lorc/poison-gas',
  burn: 'lorc/burning-embers',
  bleed: 'lorc/bleeding-wound',
  armor_break: 'lorc/broken-shield',
  weaken: 'lorc/sword-break',
  taunt: 'delapouite/angry-eyes',
  lose_sight: 'delapouite/invisible',
};

/** A status's icon, author/name under public/icons/statuses; undefined for one this client doesn't know. */
export function statusIcon(id: string): string | undefined {
  return statusIcons[id];
}

/** A status's colour, 0xrrggbb. */
export function statusColor(id: string): number {
  return colors[id] ?? OTHER;
}

/** A status's name as text says it: "armor break" for armor_break. */
export function statusName(id: string): string {
  return id.replaceAll('_', ' ');
}
