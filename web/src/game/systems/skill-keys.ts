// Each skill key uses the skill on its slot once the client's own checks pass, starting its cooldown at once and ending it at its time; a refusal from the world undoes it.
import { RequestError } from '@/net/socket';
import { mpAt, skillById, skillCooldownTicks, stageAt } from '@/progression/skills';
import { ERR, errorText } from '@/protocol/errors';
import type { Cooldown } from '@/state/skill-bar';

/** What the checks read of the player; null before Welcome. */
export interface Caster {
  readonly invested: Readonly<Record<string, number>>;
  readonly mp: number;
  readonly cooldownSpeed: number;
  readonly dead: boolean;
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
}

export class SkillKeys {
  /** The cooldowns a press during them was told about. */
  private readonly told = new WeakSet<Cooldown>();

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
    if (caster.mp < mpAt(stage, points)) {
      d.notice(errorText(ERR.NOT_ENOUGH_MP));
      return;
    }
    const ticks = skillCooldownTicks(stage.cooldown_ms, caster.cooldownSpeed, d.tickMs);
    const cooldown = { from: now, until: now + ticks * d.tickMs };
    const end = () => {
      if (d.cooldowns()[id] === cooldown) {
        d.cool(id, null);
      }
    };
    d.cool(id, cooldown);
    setTimeout(end, cooldown.until - now);
    d.use(id).catch((err: unknown) => {
      if (!(err instanceof RequestError) || err.code === 'timeout' || err.code === 'disconnected') {
        return; // it may have landed, so its cooldown stands
      }
      end();
      d.notice(err.message);
    });
  }
}
