// Each skill key uses the skill on its slot once the client's own checks pass and the action clock lets it start, starting its action, cooldown and spend at once and ending the cooldown at its time; a refusal from the world undoes them.
import { movement } from '@/gamedata/movement';
import { RequestError } from '@/net/socket';
import { skillById, skillCooldownTicks, stageAt } from '@/progression/skills';
import type { Shape, Skill, Stage } from '@/progression/skills';
import { ERR, errorText } from '@/protocol/errors';
import type { Cooldown } from '@/state/skill-bar';

import type { Point } from './drops';

/** What the checks read of the player; null before Welcome. */
export interface Caster {
  readonly invested: Readonly<Record<string, number>>;
  /** As shown: the world's count less the presses it hasn't counted yet. */
  readonly energy: number;
  readonly cooldownSpeed: number;
  readonly dead: boolean;
  readonly hurt: boolean;
  /** Where the caster's feet centre is, and each hurt party member's alive on the map, null for one not drawn yet: a heal reaching none of them finds no one, and the world would refuse it after the casting began. */
  readonly feet: Point | null;
  readonly hurtAllies: readonly (Point | null)[];
}

export interface SkillKeysDeps {
  readonly slots: () => readonly string[];
  readonly caster: () => Caster | null;
  readonly inTown: () => boolean;
  readonly tickMs: number;
  readonly now: () => number;
  readonly cooldowns: () => Readonly<Record<string, Cooldown>>;
  readonly cool: (skill: string, cooldown: Cooldown | null) => void;
  /** Resolves once the world's tick resolved it. */
  readonly use: (skill: string) => Promise<unknown>;
  /** Why a key did nothing, or the world's refusal. */
  readonly notice: (text: string) => void;
  /** Whether a skill may start now; if not, the press is held and pressed again at its moment. */
  readonly free: (again: () => void) => boolean;
  /** Whether a press short of energy waits for the world's count of the attack it cuts; if so, it is pressed again as the count comes or the attack ends. */
  readonly awaitCount: (again: () => void) => boolean;
  /** Starts the stage's action and shows it; the returned undo gives the action back. */
  readonly begin: (skill: Skill, stage: Stage) => () => void;
  /** Takes the cost off the energy shown, as pressed now; the returned undo gives it back. */
  readonly spend: (cost: number) => () => void;
}

export class SkillKeys {
  /** The cooldowns a press during them was told about. */
  private readonly told = new WeakSet<Cooldown>();
  /** The skills a press found short of energy, told once until a press finds enough. */
  private readonly short = new Set<string>();

  constructor(private readonly deps: SkillKeysDeps) {}

  press(key: number): void {
    const d = this.deps;
    const id = d.slots()[key] ?? '';
    const caster = d.caster();
    if (id === '' || !caster || caster.dead) {
      return;
    }
    const skill = skillById(id);
    const points = caster.invested[id] ?? 0;
    if (!skill || points < 1) {
      d.notice(errorText(ERR.SKILL_NOT_LEARNED));
      return;
    }
    if (skill.kind !== 'active') {
      d.notice(errorText(ERR.SKILL_PASSIVE));
      return;
    }
    if (skill.effect === 'damage' && d.inTown()) {
      d.notice(errorText(ERR.NOT_IN_TOWN));
      return;
    }
    const now = d.now();
    const cooling = d.cooldowns()[id];
    if (cooling && now < cooling.until) {
      if (!this.told.has(cooling)) {
        this.told.add(cooling); // once a cooldown, so a mashed key says it once
        d.notice(errorText(ERR.SKILL_COOLING_DOWN));
      }
      return;
    }
    const stage = skill.stages[stageAt(skill, points)]!;
    if (skill.effect === 'heal' && !healFinds(caster, stage.shape)) {
      d.notice(errorText(ERR.HP_FULL));
      return;
    }
    if (!d.free(() => this.press(key))) {
      return; // pressed again at its moment
    }
    if (caster.energy < stage.energy) {
      if (d.awaitCount(() => this.press(key))) {
        return;
      }
      if (!this.short.has(id)) {
        this.short.add(id); // once a stretch of shortage, so a mashed key says it once
        d.notice(errorText(ERR.NOT_ENOUGH_ENERGY));
      }
      return;
    }
    this.short.delete(id);
    const ticks = skillCooldownTicks(stage.cooldown_ms, caster.cooldownSpeed, d.tickMs);
    const cooldown = { from: now, until: now + ticks * d.tickMs };
    const end = () => {
      if (d.cooldowns()[id] === cooldown) {
        d.cool(id, null);
      }
    };
    d.cool(id, cooldown);
    setTimeout(end, cooldown.until - now);
    const giveBack = d.begin(skill, stage);
    const refund = d.spend(stage.energy);
    d.use(id).catch((err: unknown) => {
      if (!(err instanceof RequestError) || err.code === 'timeout' || err.code === 'disconnected') {
        return; // it may have landed, so its cooldown, action and spend stand
      }
      giveBack();
      refund();
      // Not ready keeps the cooldown: the world says the skill isn't ready, so it doesn't look ready.
      if (err.code !== ERR.SKILL_COOLING_DOWN) {
        end();
      }
      d.notice(err.message);
    });
  }
}

/** Whether a heal finds someone hurt: the caster, or a member its area reaches as the world measures it, from the caster's middle to the nearest point of their box. */
function healFinds(c: Caster, shape: Shape | null): boolean {
  if (c.hurt) {
    return true;
  }
  const radius = shape?.kind === 'area' ? (shape.radius ?? 0) : Infinity; // no slack: one sent past the edge is refused after its slow began
  const from = c.feet;
  return c.hurtAllies.some((to) => !to || !from || healReach(from, to) <= radius); // one not drawn yet: the world decides
}

/** How far a heal from feet at `from` measures to a player whose feet are at `to`: from the caster's middle to the nearest point of their box. */
export function healReach(from: Point, to: Point): number {
  const middle = from.y - movement.height / 2;
  const dx = Math.max(0, Math.abs(to.x - from.x) - movement.width / 2);
  const dy = Math.max(0, to.y - movement.height - middle, middle - to.y);
  return Math.hypot(dx, dy);
}
