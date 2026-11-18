// Skill chains as game data has them (skill-chains.md section 2): each class's roots and the follow-ups their keys turn into, which skills may stand on a key of their own, and how a condition reads.
import { classes } from '@/gamedata/classes';

import { statusName } from './statuses';

/** Holds exactly one of its fields; the others are null. */
export interface ChainCondition {
  readonly after_ms: number | null;
  readonly controlled: readonly string[] | null;
  readonly far: number | null;
  readonly dodged_ms: number | null;
  /** Within this of the player's last block (C21). */
  readonly blocked_ms: number | null;
  /** While the player is held: a break-free's. */
  readonly controlled_self: boolean;
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
  return classes.find((c) => c.id === cls)?.chains ?? [];
}

/** Whether skill may sit on a key of its own for cls, as the world lets it be used from one: a root of the class's chains, or in none of them. */
export function keyable(cls: string, skill: string): boolean {
  const chains = chainsOf(cls);
  return (
    chains.some((c) => c.root === skill) ||
    !chains.some((c) => c.follow.some((f) => f.skill === skill))
  );
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

/** A condition in a few words, for the chains panel's arrows: "right after", "after a dodge", "after a block", "stunned or frozen" ("controlled" for four or more), "while held", "over 3 m". */
export function shortCondition(w: ChainCondition): string {
  if (w.after_ms !== null) {
    return 'right after';
  }
  if (w.dodged_ms !== null) {
    return 'after a dodge';
  }
  if (w.blocked_ms !== null) {
    return 'after a block';
  }
  if (w.controlled !== null) {
    return w.controlled.length >= 4
      ? 'controlled'
      : or(w.controlled.map((s) => controlledAs[s] ?? statusName(s)));
  }
  if (w.controlled_self) {
    return 'while held';
  }
  return `over ${w.far ?? 0} m`;
}

/** A follow-up's condition as the skill window and a key's tooltip say it, its root named rootName: "right after Strike", "while the target is stunned or frozen", "while the target is over 3 m away", "right after a dodge", "right after a block". */
export function conditionText(w: ChainCondition, rootName: string): string {
  if (w.after_ms !== null) {
    return `right after ${rootName}`;
  }
  if (w.dodged_ms !== null) {
    return 'right after a dodge';
  }
  if (w.blocked_ms !== null) {
    return 'right after a block';
  }
  if (w.controlled !== null) {
    return `while the target is ${or(w.controlled.map((s) => controlledAs[s] ?? statusName(s)))}`;
  }
  if (w.controlled_self) {
    return 'while you’re held: stunned, frozen, asleep, knocked down or pinned';
  }
  return `while the target is over ${w.far ?? 0} m away`;
}
