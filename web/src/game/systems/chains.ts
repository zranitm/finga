// Skill chains (skill-chains.md section 3): which skill a key offers now, its root or the first of the root's follow-ups that has a point, is off cooldown and whose condition holds, judged as the world will judge the press.
import { classes } from '@/gamedata/classes';
import { ticksIn } from '@/lib/fixed-step';
import { statusName } from '@/progression/statuses';
import type { StatusOn } from '@/progression/statuses';
import type { Cooldown } from '@/state/skill-bar';

/** Holds exactly one of its fields; the others are null. */
export interface ChainCondition {
  readonly after_ms: number | null;
  readonly controlled: readonly string[] | null;
  readonly far: number | null;
  readonly dodged_ms: number | null;
}

export interface FollowUp {
  readonly skill: string;
  readonly when: ChainCondition;
}

export interface Chain {
  readonly root: string;
  readonly follow: readonly FollowUp[];
}

/** A class's chains, which are the only ones that apply to it: a promoted class's replace the Beginner's. */
export function chainsOf(cls: string): readonly Chain[] {
  return (classes.find((c) => c.id === cls)?.chains ?? []) as readonly Chain[];
}

/** Whether skill may sit on a key of its own for cls, as the world lets it be used from one: a root of the class's chains, or in none of them. */
export function keyable(cls: string, skill: string): boolean {
  const chains = chainsOf(cls);
  return (
    chains.some((c) => c.root === skill) ||
    !chains.some((c) => c.follow.some((f) => f.skill === skill))
  );
}

/** What a key's conditions are judged by now. */
export interface ChainNow {
  /** The input a press now goes with. */
  readonly seq: number;
  readonly tickMs: number;
  /** performance.now(), which cooldowns are timed in. */
  readonly now: number;
  readonly invested: Readonly<Record<string, number>>;
  readonly cooldowns: Readonly<Record<string, Cooldown>>;
  /** The input each skill's latest hit falls in, as the world counts it: the input it was pressed during, plus its hit point in ticks. */
  readonly hits: Readonly<Record<string, number>>;
  /** The input acked by the frame that carried the player's latest dodge; null for none. */
  readonly dodged: number | null;
  /** The locked mob: how far its centre is from the feet across the ground, and its statuses as of the newest frame's tick; null for none. */
  readonly lock: {
    readonly far: number;
    readonly statuses: readonly StatusOn[];
    readonly tick: number;
  } | null;
}

/** Whether a follow-up of root's waiting on w may go now: windows count inputs, as the world does, from the root's hit or the dodge's frame; a control is a running status of the lock's, and far is farther than w.far. */
export function holds(root: string, w: ChainCondition, n: ChainNow): boolean {
  if (w.after_ms !== null) {
    const hit = n.hits[root];
    return hit !== undefined && hit <= n.seq && n.seq <= hit + ticksIn(w.after_ms, n.tickMs);
  }
  if (w.dodged_ms !== null) {
    return n.dodged !== null && n.seq <= n.dodged + ticksIn(w.dodged_ms, n.tickMs);
  }
  const lock = n.lock;
  if (!lock) {
    return false;
  }
  if (w.controlled !== null) {
    return lock.statuses.some((s) => s.ends > lock.tick && w.controlled!.includes(s.id));
  }
  return lock.far > (w.far ?? Infinity);
}

/** The skill root's key offers now for cls: the first of its follow-ups, in their order, with a point, off cooldown and whose condition holds, else root itself. */
export function liveSkill(cls: string, root: string, n: ChainNow): string {
  const chain = chainsOf(cls).find((c) => c.root === root);
  const live = chain?.follow.find(
    (f) =>
      (n.invested[f.skill] ?? 0) >= 1 &&
      !(n.now < (n.cooldowns[f.skill]?.until ?? 0)) &&
      holds(root, f.when, n),
  );
  return live?.skill ?? root;
}

/** The follow-up skill is to root's key for cls, if it is one. */
export function followUpOf(cls: string, root: string, skill: string): FollowUp | undefined {
  return chainsOf(cls)
    .find((c) => c.root === root)
    ?.follow.find((f) => f.skill === skill);
}

// How a hard status reads after "the target is".
const controlledAs: Readonly<Record<string, string>> = {
  stun: 'stunned',
  freeze: 'frozen',
  sleep: 'asleep',
  knockdown: 'knocked down',
  pin: 'pinned',
};

/** "a", "a or b", "a, b or c". */
function or(items: readonly string[]): string {
  return items.length < 2 ? items.join('') : `${items.slice(0, -1).join(', ')} or ${items.at(-1)}`;
}

/** A follow-up's condition as the skill window and a key's tooltip say it, its root named rootName: "right after Strike", "while the target is stunned or frozen", "while the target is over 3 m away", "right after a dodge". */
export function conditionText(w: ChainCondition, rootName: string): string {
  if (w.after_ms !== null) {
    return `right after ${rootName}`;
  }
  if (w.dodged_ms !== null) {
    return 'right after a dodge';
  }
  if (w.controlled !== null) {
    return `while the target is ${or(w.controlled.map((s) => controlledAs[s] ?? statusName(s)))}`;
  }
  return `while the target is over ${w.far ?? 0} m away`;
}

/** What chains remember of the player's own play on a map, as the world keeps it: each skill's latest hit and the latest dodge, in inputs. A new map, or a rejoin, starts it over. */
export class ChainMemory {
  private readonly hitAt = new Map<string, number>();
  private dodgedAt: number | null = null;

  get hits(): Readonly<Record<string, number>> {
    return Object.fromEntries(this.hitAt);
  }

  get dodged(): number | null {
    return this.dodgedAt;
  }

  /** A skill pressed during input seq lands its hit hitTicks on; the returned undo forgets it, for a use the world refused. */
  used(skill: string, seq: number, hitTicks: number): () => void {
    const before = this.hitAt.get(skill);
    const at = seq + hitTicks;
    this.hitAt.set(skill, at);
    return () => {
      if (this.hitAt.get(skill) !== at) {
        return; // a later use stands
      }
      if (before === undefined) {
        this.hitAt.delete(skill);
      } else {
        this.hitAt.set(skill, before);
      }
    };
  }

  /** A mob's attack passed through the player's dodge, in the frame acking input ackSeq. */
  dodge(ackSeq: number): void {
    this.dodgedAt = ackSeq;
  }

  clear(): void {
    this.hitAt.clear();
    this.dodgedAt = null;
  }
}
