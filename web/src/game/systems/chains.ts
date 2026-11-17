// Skill chains (skill-chains.md section 3): which skill a key offers now, its root or the first of the root's follow-ups that has a point, is off cooldown and whose condition holds, judged as the world will judge the press.
import { ticksIn } from '@/lib/fixed-step';
import { chainsOf } from '@/progression/chains';
import type { ChainCondition } from '@/progression/chains';
import { pointsIn, skillById } from '@/progression/skills';
import type { StatusOn } from '@/progression/statuses';
import type { Cooldown } from '@/state/skill-bar';

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
  /** Whether a hard status holds the player as of the newest frame. */
  readonly held: boolean;
  /** The locked mob: how far its centre is from the feet across the ground, and its statuses as of the newest frame's tick; null for none. */
  readonly lock: {
    readonly far: number;
    readonly statuses: readonly StatusOn[];
    readonly tick: number;
  } | null;
}

/** Whether a follow-up of root's waiting on w may go now: windows count inputs, as the world does, from the root's hit or the dodge's frame; a control is a running status of the lock's, the player's own hold one of theirs, and far is farther than w.far. */
export function holds(root: string, w: ChainCondition, n: ChainNow): boolean {
  if (w.controlled_self) {
    return n.held;
  }
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
      pointsOf(f.skill, n.invested) >= 1 &&
      !(n.now < (n.cooldowns[f.skill]?.until ?? 0)) &&
      holds(root, f.when, n),
  );
  return live?.skill ?? root;
}

const pointsOf = (id: string, invested: Readonly<Record<string, number>>): number => {
  const skill = skillById(id);
  return skill ? pointsIn(skill, invested) : 0;
};

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
