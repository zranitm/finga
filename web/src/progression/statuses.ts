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

/** A status's name as text says it: "armor break" for armor_break. */
export function statusName(id: string): string {
  return id.replaceAll('_', ' ');
}
