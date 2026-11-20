// Mirrors server/internal/shared/progression's skills for the skill window and the character window's preview; testdata/stats pins passives to the server's.
import { classes } from '@/gamedata/classes';
import { skills as data } from '@/gamedata/skills';
import { ticksIn } from '@/lib/fixed-step';

import { lineOf } from './classes';
import { derivedStats, moveSpeedWith, primaryKeys } from './stats';
import type { Roll } from './stats';

export interface Scaled {
  readonly base: number;
  readonly per_point: number;
}

/** Where an active skill finds its targets, in metres: an arc in front, a shot along the aim, or an area around the caster. */
export interface Shape {
  readonly kind: string;
  readonly width?: number;
  readonly height?: number;
  readonly range?: number;
  readonly radius?: number;
  /** An area's middle: 'lock' centres it on the locked mob within reach, else reach along the aim. */
  readonly at?: string;
  readonly reach?: number;
}

/** A skill's move of its user: a dash or leap over time_ms, or a blink at once, toward lock, back or heading. */
export interface Move {
  readonly kind: string;
  readonly distance: number;
  readonly time_ms: number;
  /** How long after the input following the press it starts, as Deflect leaps once its guard is down. */
  readonly delay_ms: number;
  readonly toward: string;
  /** A dash or leap ends at the first mob body it meets. */
  readonly stop: boolean;
  /** A dash or leap passes mobs' bodies and ends clear of them, as a blink does. */
  readonly through: boolean;
}

/** A status a skill puts on what it lands on, for time_ms (0 for a knockdown, which lasts the down pose); damage over time deals power × the user's attack each tick. */
export interface Inflict {
  readonly status: string;
  readonly time_ms: number;
  readonly power: Scaled;
}

/** A skill from at points on, every field resolved from the stages before it; bonus is in the units items use. */
export interface Stage {
  readonly at: number;
  readonly name: string;
  readonly description: string;
  /** A game-icons.net path, author/name: the SVG at public/icons/skills/<icon>.svg. */
  readonly icon: string;
  readonly damage: string | null;
  readonly shape: Shape | null;
  /** Active: what using it spends. */
  readonly energy: number;
  readonly cooldown_ms: number;
  readonly power: Scaled;
  readonly targets: number;
  readonly duration_ms: number;
  readonly bonus: Readonly<Record<string, Scaled>>;
  /** Active: how long using it keeps the player busy. */
  readonly length_ms: number;
  /** Active: where the hit lands, the cancel point falls and the look is back to ready, as shares of the length. */
  readonly hit: number;
  readonly cancel: number;
  readonly ready: number;
  /** Active: a basic attack may cut it off from its cancel point. */
  readonly cancelable: boolean;
  readonly move: Move | null;
  /** Active: from the action's start, mobs' attacks pass through the user. */
  readonly dodge_ms: number;
  /** Block: from the action's start, mobs' blockable attacks from the front are stopped (C21). */
  readonly block_ms: number;
  /** Damage and status: on what it lands on. */
  readonly status: Inflict | null;
  /** Damage and status: metres what it lands on is pushed away from the user; 0 for none. */
  readonly push: number;
  /** Damage: the hit repeats count times, every_ms apart. */
  readonly hits: { readonly count: number; readonly every_ms: number } | null;
  /** Buff: the caster and party members within the area's radius. */
  readonly reach_party: boolean;
  /** Buff: on what each of the user's hits lands on while it lasts. */
  readonly on_hit: Inflict | null;
  /** Buff: power × the user's magic attack, taken before HP. */
  readonly shield: { readonly power: Scaled } | null;
}

/** A way to use a skill (skill-traits.md section 2): the default changes nothing; the extra is its own stage, resolved on top of the last, open from the last evolution. */
export interface Trait {
  readonly id: string;
  readonly name: string;
  readonly description: string;
  /** The extra's stage; null for the default. */
  readonly stage: Stage | null;
}

export interface Skill {
  readonly id: string;
  readonly kind: string;
  readonly effect: string;
  /** The class that teaches it. */
  readonly class: string;
  /** The character level from which it takes its first point. */
  readonly level: number;
  /** A break-free's: usable only while its user is held, which it ends first, through cant-act and silence. */
  readonly breaks_control: boolean;
  /** Its class has it without points: it counts as one and takes no more. */
  readonly granted: boolean;
  /** The first at 0 points, the totals rising. */
  readonly stages: readonly Stage[];
  /** The default then the extra, or none. */
  readonly traits: readonly Trait[];
}

const skills: readonly Skill[] = data;

// Each apart from a basic attack's pale slash and from the forest's grass.
const colors = {
  physical: 0xff6a3d,
  magic: 0xb070f0,
  heal: 0x4fd8ff,
  buff: 0xf0b23a,
  status: 0x6b8cff,
} as const;

/** Each class's own block colour (C21), apart at a glance and from the warning areas' yellow and red: its guard's ring and its tile. */
export const blockColors: Readonly<Record<string, number>> = {
  beginner: 0xd8dde6, // silver: no hue near the warnings' yellow and red
  warrior: 0x4f86ff,
  thief: 0xa66bff,
  bowman: 0x46d160,
  magician: 0xff6ad5,
};

/** A skill's colour, for its look in the world and its icon's tile: a heal's, buff's or status skill's by effect, a block's by its class, a move's or passive's by its class's attack, others by damage type. */
export function colorOf(skill: Skill, stage: Stage): number {
  if (skill.effect === 'heal' || skill.effect === 'buff' || skill.effect === 'status') {
    return colors[skill.effect];
  }
  const own = skill.effect === 'block' ? blockColors[skill.class] : undefined;
  if (own !== undefined) {
    return own;
  }
  if (skill.effect === 'move' || skill.effect === 'block' || skill.kind === 'passive') {
    return damageColor(classes.find((c) => c.id === skill.class)?.attack.damage);
  }
  return damageColor(stage.damage);
}

/** A damage type's colour, as its skills wear it: magic's, or physical's for anything else. */
export function damageColor(damage: string | null | undefined): number {
  return damage === 'magic' ? colors.magic : colors.physical;
}

/** A value with points invested. */
export function scaledAt(s: Scaled, points: number): number {
  return s.base + s.per_point * points;
}

/** The skill with id, if this client knows it. */
export function skillById(id: string): Skill | undefined {
  return skills.find((s) => s.id === id);
}

/** The index of the stage a skill is in with points: the last whose total they reach. */
export function stageAt(skill: Skill, points: number): number {
  let k = 0;
  skill.stages.forEach((st, i) => {
    if (points >= st.at) k = i;
  });
  return k;
}

/** Whether a skill's extra trait is open with points: from its last evolution, or its first point for a skill with one stage. */
export function traitOpen(skill: Skill, points: number): boolean {
  return points >= Math.max(skill.stages.at(-1)!.at, 1);
}

/** The stage a skill is used at with points and the trait chosen: the extra's while it's open, else the stage the points reach, as the world resolves it. */
export function specAt(skill: Skill, points: number, trait: string | undefined): Stage {
  const extra = skill.traits[1];
  return extra?.stage && extra.id === trait && traitOpen(skill, points)
    ? extra.stage
    : skill.stages[stageAt(skill, points)]!;
}

/** The stage a SkillUsed names: its extra trait's, or its stage by index. */
export function usedStage(skill: Skill, stage: number, trait: string): Stage | undefined {
  return (
    (trait !== '' ? skill.traits.find((t) => t.id === trait)?.stage : null) ?? skill.stages[stage]
  );
}

/** Every skill a class has to use: its own, then each class's above it, up to the Beginner's; a granted skill is its own class's alone, since a promotion grants its own. */
export function learnable(cls: string): Skill[] {
  return lineOf(cls).flatMap((id) =>
    (classes.find((c) => c.id === id)?.skills ?? []).flatMap((s) => {
      const skill = skillById(s);
      return skill && (!skill.granted || id === cls) ? [skill] : [];
    }),
  );
}

/** The points a PvP match's kit gives each of cls's skills (pvp.md section 6): every one at its last stage, at least one; a granted skill's are one, as pointsIn reads them. */
export function fullPoints(cls: string): Readonly<Record<string, number>> {
  return Object.fromEntries(
    learnable(cls)
      .filter((s) => !s.granted)
      .map((s) => [s.id, Math.max(s.stages.at(-1)!.at, 1)]),
  );
}

/** The block cls is granted with (C21); undefined for a class with none. */
export function blockOf(cls: string): Skill | undefined {
  return learnable(cls).find((s) => s.effect === 'block');
}

/** The points a character with invested has in skill: a granted one's are one. */
export function pointsIn(skill: Skill, invested: Readonly<Record<string, number>>): number {
  return skill.granted ? 1 : (invested[skill.id] ?? 0);
}

const keyOf = new Map<string, keyof Roll>([
  ...primaryKeys.map((k) => [k, k] as const),
  ...derivedStats.map((s) => [s.data, s.key] as const),
]);

/** A bonus with points invested, each stat rounded to a whole number in the units items use, as an item's roll is. */
export function bonusAt(bonus: Readonly<Record<string, Scaled>>, points: number): Roll {
  const out: Partial<Record<keyof Roll, number>> = {};
  for (const [stat, s] of Object.entries(bonus)) {
    const key = keyOf.get(stat);
    if (key !== undefined) out[key] = Math.round(scaledAt(s, points));
  }
  return out;
}

function addTo(out: Partial<Record<keyof Roll, number>>, roll: Roll): void {
  for (const [k, v] of Object.entries(roll)) {
    const key = k as keyof Roll;
    out[key] = (out[key] ?? 0) + (v ?? 0);
  }
}

/** A skill's bonus at the stage its points and trait reach. */
function bonusOf(skill: Skill, points: number, trait?: string): Roll {
  return bonusAt(specAt(skill, points, trait).bonus, points);
}

/** What the passive skills a class can learn add, each at its stage and points; one with none adds nothing. */
export function passives(cls: string, invested: Readonly<Record<string, number>>): Roll {
  const out: Partial<Record<keyof Roll, number>> = {};
  for (const skill of learnable(cls)) {
    const points = invested[skill.id] ?? 0;
    if (skill.kind === 'passive' && points >= 1) addTo(out, bonusOf(skill, points));
  }
  return out;
}

/** A buff on, at the points and trait it was used with. */
export interface BuffOn {
  readonly skill: string;
  readonly points: number;
  readonly trait: string;
}

/** What the buffs on add, each at the points and trait it was used with, as the world counts them in the skills' share. */
export function buffsOn(buffs: readonly BuffOn[]): Roll {
  const out: Partial<Record<keyof Roll, number>> = {};
  for (const b of buffs) {
    const skill = skillById(b.skill);
    if (skill) addTo(out, bonusOf(skill, b.points, b.trait));
  }
  return out;
}

/** The skills' share of the stats: the passives and the buffs on. */
export function skillsShare(
  cls: string,
  invested: Readonly<Record<string, number>>,
  buffs: readonly BuffOn[],
): Roll {
  const out: Partial<Record<keyof Roll, number>> = {};
  addTo(out, passives(cls, invested));
  addTo(out, buffsOn(buffs));
  return out;
}

/** The move speed the world gives once one buff ends: the sheet's without the skills' share, plus what the passives and the other buffs on add, held to the limit. */
export function moveSpeedWithout(
  sheet: { readonly moveSpeed: number; readonly skillsMoveSpeed: number },
  cls: string,
  invested: Readonly<Record<string, number>>,
  buffs: readonly BuffOn[],
  buff: string,
): number {
  const others =
    skillsShare(
      cls,
      invested,
      buffs.filter((b) => b.skill !== buff),
    ).moveSpeed ?? 0;
  return moveSpeedWith(sheet.moveSpeed - sheet.skillsMoveSpeed, others);
}

/** A skill's cooldown cut by cooldown speed, in the server's whole ticks: ⌈cooldown × (1 − speed) ÷ tick⌉. */
export function skillCooldownTicks(cooldownMs: number, speed: number, tickMs: number): number {
  return ticksIn(cooldownMs * (1 - speed), tickMs);
}
