// Node runs this file directly (type stripping), so it may use only erasable TypeScript.
import { mkdirSync, readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

import { parse } from 'yaml';

const here = dirname(fileURLToPath(import.meta.url));
const dataDir = join(here, '..', '..', 'server', 'data');
const codesFile = join(here, '..', '..', 'server', 'testdata', 'errors', 'codes.json');
const outDir = join(here, '..', 'src', 'gamedata');

function load(name: string): unknown {
  return parse(readFileSync(join(dataDir, name), 'utf8'));
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function must<T>(value: unknown, check: (v: unknown) => v is T, what: string): T {
  if (!check(value)) {
    throw new Error(`gamedata: ${what} has an unexpected shape`);
  }
  return value;
}

function isArray(value: unknown): value is unknown[] {
  return Array.isArray(value);
}

function isString(value: unknown): value is string {
  return typeof value === 'string';
}

function isNumber(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value);
}

function isBoolean(value: unknown): value is boolean {
  return typeof value === 'boolean';
}

/** "600ms" or "1.5s" to whole milliseconds, as Go's time.Duration parses them. */
function durationMs(value: unknown, what: string): number {
  if (typeof value !== 'string') {
    throw new Error(`gamedata: ${what} must be a duration string like 600ms`);
  }
  const m = /^(\d+(?:\.\d+)?)(ms|s)$/.exec(value);
  if (!m) {
    throw new Error(`gamedata: ${what}: cannot parse duration ${value}`);
  }
  const n = Number(m[1]);
  return m[2] === 's' ? Math.round(n * 1000) : Math.round(n);
}

// movement.yaml: every key, as the sim reads it.
const movement = must(load('movement.yaml'), isRecord, 'movement.yaml');

// exp_curve.yaml
const expCurve = must(load('exp_curve.yaml'), isRecord, 'exp_curve.yaml');

// items.yaml: templates with the stack size the server applies (1 for equipment, 100 otherwise, unless max_stack says so), and the ten equip slots in order.
const itemsFile = must(load('items.yaml'), isRecord, 'items.yaml');
const equipSlots = must(itemsFile.equip_slots, isArray, 'items.yaml equip_slots');
const items = must(itemsFile.items, isArray, 'items.yaml items').map((raw) => {
  const it = must(raw, isRecord, 'an item');
  const tab = it.tab;
  return {
    ...it,
    max_stack: it.max_stack ?? (tab === 'equip' ? 1 : 100),
  };
});

// actions.yaml: the cancel point an action takes unless it names its own, the early-press margin in milliseconds, and energy: the most a player holds and a basic attack's gain.
const actionsFile = must(load('actions.yaml'), isRecord, 'actions.yaml');
const energyFile = must(actionsFile.energy, isRecord, 'actions.yaml energy');
const actions = {
  cancel: must(actionsFile.cancel, isNumber, 'actions.yaml cancel'),
  early_press_ms: durationMs(actionsFile.early_press, 'actions.yaml early_press'),
  energy: {
    max: must(energyFile.max, isNumber, 'actions.yaml energy max'),
    per_attack: must(energyFile.per_attack, isNumber, 'actions.yaml energy per_attack'),
  },
};

/** An action's hit and cancel point as the server resolves them: what it names, else the start and actions.yaml's cancel point. */
function shares(raw: Record<string, unknown>, what: string) {
  return {
    hit: raw.hit === undefined ? 0 : must(raw.hit, isNumber, `${what} hit`),
    cancel:
      raw.cancel === undefined ? actions.cancel : must(raw.cancel, isNumber, `${what} cancel`),
  };
}

// classes.yaml: the tree, root first; lengths become milliseconds, and what a class leaves out is null. Starter items and promotions stay on the server, which shows a trainer's conditions in its dialog.
const classesFile = must(load('classes.yaml'), isRecord, 'classes.yaml');
const classes = must(classesFile.classes, isArray, 'classes.yaml classes').map((raw) => {
  const c = must(raw, isRecord, 'a class');
  const id = must(c.id, isString, 'a class id');
  const { length, hit, cancel, ...shape } = must(c.attack, isRecord, `${id} attack`);
  const look = must(c.look, isRecord, `${id} look`);
  return {
    id,
    name: must(c.name, isString, `${id} name`),
    description: must(c.description, isString, `${id} description`),
    parent: c.parent === undefined ? null : must(c.parent, isString, `${id} parent`),
    look: {
      sprite: must(look.sprite, isString, `${id} look.sprite`),
      tint: look.tint === undefined ? null : must(look.tint, isNumber, `${id} look.tint`),
    },
    attack: {
      ...shape,
      length_ms: durationMs(length, `${id} attack length`),
      ...shares({ hit, cancel }, `${id} attack`),
    },
    base_stats:
      c.base_stats === undefined ? null : must(c.base_stats, isRecord, `${id} base_stats`),
    auto_assign:
      c.auto_assign === undefined ? null : must(c.auto_assign, isRecord, `${id} auto_assign`),
    skills: c.skills === undefined ? [] : must(c.skills, isArray, `${id} skills`),
  };
});

// skills.yaml: each skill with every stage resolved, as the server reads them: a stage changes only what it names. Durations become milliseconds, and the class teaching it comes from classes.yaml.
interface Scaled {
  base: number;
  per_point: number;
}
const scaledOver = (raw: unknown, prev: Scaled, what: string): Scaled => {
  if (raw === undefined) return prev;
  const r = must(raw, isRecord, what);
  return {
    base: r.base === undefined ? prev.base : must(r.base, isNumber, `${what} base`),
    per_point:
      r.per_point === undefined ? prev.per_point : must(r.per_point, isNumber, `${what} per_point`),
  };
};
const teacher = new Map(classes.flatMap((c) => c.skills.map((s) => [String(s), c.id] as const)));
const skillsFile = must(load('skills.yaml'), isRecord, 'skills.yaml');
const skills = must(skillsFile.skills, isArray, 'skills.yaml skills').map((raw) => {
  const s = must(raw, isRecord, 'a skill');
  const id = must(s.id, isString, 'a skill id');
  const kind = must(s.kind, isString, `${id} kind`);
  const zero: Scaled = { base: 0, per_point: 0 };
  const cur = {
    damage: null as string | null,
    shape: null as Record<string, unknown> | null,
    energy: 0,
    cooldown_ms: 0,
    power: zero,
    targets: 1,
    duration_ms: 0,
    bonus: {} as Record<string, Scaled>,
    length_ms: 0,
    hit: 0,
    cancel: kind === 'active' ? actions.cancel : 0,
    cancelable: false,
  };
  const apply = (f: Record<string, unknown>, what: string) => {
    if (f.damage !== undefined) cur.damage = must(f.damage, isString, `${what} damage`);
    if (f.shape !== undefined) cur.shape = { ...must(f.shape, isRecord, `${what} shape`) };
    if (f.energy !== undefined) cur.energy = must(f.energy, isNumber, `${what} energy`);
    if (f.cooldown !== undefined) cur.cooldown_ms = durationMs(f.cooldown, `${what} cooldown`);
    cur.power = scaledOver(f.power, cur.power, `${what} power`);
    if (f.targets !== undefined) cur.targets = must(f.targets, isNumber, `${what} targets`);
    if (f.duration !== undefined) cur.duration_ms = durationMs(f.duration, `${what} duration`);
    if (f.length !== undefined) cur.length_ms = durationMs(f.length, `${what} length`);
    if (f.hit !== undefined) cur.hit = must(f.hit, isNumber, `${what} hit`);
    if (f.cancel !== undefined) cur.cancel = must(f.cancel, isNumber, `${what} cancel`);
    if (f.cancelable !== undefined)
      cur.cancelable = must(f.cancelable, isBoolean, `${what} cancelable`);
    if (f.bonus !== undefined) {
      for (const [stat, v] of Object.entries(must(f.bonus, isRecord, `${what} bonus`))) {
        cur.bonus = {
          ...cur.bonus,
          [stat]: scaledOver(v, cur.bonus[stat] ?? zero, `${what} bonus ${stat}`),
        };
      }
    }
  };
  apply(s, id);
  return {
    id,
    kind,
    effect: must(s.effect, isString, `${id} effect`),
    class: teacher.get(id) ?? null,
    stages: must(s.stages, isArray, `${id} stages`).map((rawStage) => {
      const st = must(rawStage, isRecord, `a ${id} stage`);
      apply(st, `${id} ${String(st.name)}`);
      return {
        at: must(st.at, isNumber, `${id} stage at`),
        name: must(st.name, isString, `${id} stage name`),
        description: must(st.description, isString, `${id} stage description`),
        ...cur,
      };
    }),
  };
});

// mobs.yaml: each mob's resolved attacks, times in ms and unused values 0; an attack's index is MobAttacked's.
const mobsFile = must(load('mobs.yaml'), isRecord, 'mobs.yaml');
const attackTable = must(mobsFile.attacks, isRecord, 'mobs.yaml attacks');
const attackOf = (raw: unknown, what: string) => {
  const use = must(raw, isRecord, `a ${what} attack`);
  const name = must(use.use, isString, `${what} use`);
  const v = { ...must(attackTable[name], isRecord, `attack ${name}`), ...use };
  const time = (key: string) =>
    v[key] === undefined ? 0 : durationMs(v[key], `${what} ${name} ${key}`);
  const value = (key: string) =>
    v[key] === undefined ? 0 : must(v[key], isNumber, `${what} ${name} ${key}`);
  return {
    name,
    kind: must(v.kind, isString, `attack ${name} kind`),
    windup_ms: time('windup'),
    travel_ms: time('travel'),
    recover_ms: time('recover'),
    cooldown_ms: time('cooldown'),
    power: value('power'),
    reach: value('reach'),
    distance: value('distance'),
    spread: value('spread'),
    range: value('range'),
    speed: value('speed'),
    size: value('size'),
  };
};
const mobs = must(mobsFile.mobs, isArray, 'mobs.yaml mobs').map((raw) => {
  const m = must(raw, isRecord, 'a mob');
  const look = must(m.look, isRecord, `${String(m.id)} look`);
  return {
    ...m,
    attacks: must(m.attacks, isArray, `${String(m.id)} attacks`).map((a) =>
      attackOf(a, String(m.id)),
    ),
    elite: m.elite === true,
    boss: m.boss === true,
    super_armor: m.super_armor === true,
    look: {
      sprite: must(look.sprite, isString, `${String(m.id)} look.sprite`),
      tint: look.tint === undefined ? null : must(look.tint, isNumber, `${String(m.id)} look.tint`),
      scale:
        look.scale === undefined ? 1 : must(look.scale, isNumber, `${String(m.id)} look.scale`),
    },
  };
});

// stats.yaml: what each point gives, the curves, the limits and each class's bases, for the character window's preview.
const stats = must(load('stats.yaml'), isRecord, 'stats.yaml');

// loot.yaml: each duration becomes milliseconds, as drop_life_ms.
const lootFile = must(load('loot.yaml'), isRecord, 'loot.yaml');
const loot = Object.fromEntries(
  Object.entries(lootFile).map(([key, value]) => [
    `${key}_ms`,
    durationMs(value, `loot.yaml ${key}`),
  ]),
);

// npcs.yaml, each NPC with the map and point its npcs layer places it at: the bottom centre, which for a point object is the point.
const npcsFile = must(load('npcs.yaml'), isRecord, 'npcs.yaml');
const placed = new Map<string, { map: string; x: number; y: number }>();
const maps = [];
for (const file of readdirSync(join(dataDir, 'maps'))
  .filter((f) => f.endsWith('.tmj'))
  .sort()) {
  const id = file.replace('.tmj', '');
  const tmj = must(load(join('maps', file)), isRecord, file);
  const props: Record<string, unknown> = {};
  for (const raw of must(tmj.properties, isArray, `${file} properties`)) {
    const p = must(raw, isRecord, `a ${file} property`);
    props[String(p.name)] = p.value;
  }
  const kind = props.kind;
  if (kind !== 'town' && kind !== 'hunting') {
    throw new Error(`gamedata: ${file} kind must be town or hunting`);
  }
  const hunting = kind === 'hunting';
  maps.push({
    id,
    name: must(props.name, isString, `${file} name`),
    kind,
    town: hunting ? must(props.town, isString, `${file} town`) : id,
    start: props.start === true,
    ground: must(props.ground, isString, `${file} ground`),
    background: must(props.background, isString, `${file} background`),
    min_level: hunting ? must(props.min_level, isNumber, `${file} min_level`) : null,
    max_level: hunting ? must(props.max_level, isNumber, `${file} max_level`) : null,
  });
  for (const raw of must(tmj.layers, isArray, `${file} layers`)) {
    const layer = must(raw, isRecord, `a ${file} layer`);
    if (layer.name !== 'npcs') continue;
    for (const o of must(layer.objects, isArray, `${file} npcs`)) {
      const { name, x, y, width, height } = must(o, isRecord, `a ${file} npc`);
      placed.set(String(name), {
        map: id,
        x: Number(x) + Number(width ?? 0) / 2,
        y: Number(y) + Number(height ?? 0),
      });
    }
  }
}
const npcs = {
  talk_reach: npcsFile.talk_reach,
  npcs: must(npcsFile.npcs, isArray, 'npcs.yaml npcs').map((raw) => {
    const n = must(raw, isRecord, 'an npc');
    const at = placed.get(String(n.id));
    if (!at) {
      throw new Error(`gamedata: npc ${String(n.id)} stands on no map`);
    }
    return { trainer: null, gives: [], takes: [], lines: [], ...n, ...at };
  }),
};

// quests.yaml: every field present, and each objective as {kind, target, count}, as the server reads them.
const questsFile = must(load('quests.yaml'), isRecord, 'quests.yaml');
const quests = must(questsFile.quests, isArray, 'quests.yaml quests').map((raw) => {
  const q = must(raw, isRecord, 'a quest');
  const rewards = isRecord(q.rewards) ? q.rewards : {};
  return {
    ...q,
    level: q.level ?? 0,
    class: q.class ?? null,
    quests: q.quests ?? [],
    objectives: must(q.objectives, isArray, `${String(q.id)} objectives`).map((o) => {
      const { kill, collect, talk, level, count } = must(
        o,
        isRecord,
        `a ${String(q.id)} objective`,
      );
      if (kill !== undefined) return { kind: 'kill', target: kill, count };
      if (collect !== undefined) return { kind: 'collect', target: collect, count };
      if (talk !== undefined) return { kind: 'talk', target: talk, count: 1 };
      return { kind: 'level', target: null, count: level };
    }),
    rewards: { exp: rewards.exp ?? 0, items: rewards.items ?? [] },
    repeatable: q.repeatable ?? false,
  };
});

mkdirSync(outDir, { recursive: true });
const header =
  '// Generated by scripts/gen-gamedata.ts from ../server/data and the error catalog; do not edit.\n';
// The limits the services enforce and the client shows: quests.yaml's max_active and chat.yaml's line_max.
const chatFile = must(load('chat.yaml'), isRecord, 'chat.yaml');
const limits = {
  max_active_quests: must(questsFile.max_active, isNumber, 'quests.yaml max_active'),
  chat_line_max: must(chatFile.line_max, isNumber, 'chat.yaml line_max'),
};

// The gateway's error catalog, each code by its name: ERR.EQUIP_TAB_FULL reads better than 220005.
const errorCodes = Object.fromEntries(
  must(JSON.parse(readFileSync(codesFile, 'utf8')), isArray, 'codes.json').map((raw) => {
    const { code, name } = must(raw, isRecord, 'a codes.json row');
    return [must(name, isString, 'a code name'), must(code, isNumber, `${String(name)}'s code`)];
  }),
);

const files: Record<string, unknown> = {
  'movement.ts': movement,
  'exp-curve.ts': expCurve,
  'items.ts': items,
  'equip-slots.ts': equipSlots,
  'classes.ts': classes,
  'skills.ts': skills,
  'mobs.ts': mobs,
  'maps.ts': maps,
  'loot.ts': loot,
  'actions.ts': actions,
  'stats.ts': stats,
  'npcs.ts': npcs,
  'quests.ts': quests,
  'limits.ts': limits,
  'error-codes.ts': errorCodes,
};
for (const [name, value] of Object.entries(files)) {
  const exportName = name.replace('.ts', '').replace(/-(\w)/g, (_, c: string) => c.toUpperCase());
  const body = `${header}export const ${exportName} = ${JSON.stringify(value, null, 2)} as const;\n`;
  writeFileSync(join(outDir, name), body);
}
console.log(`gamedata: wrote ${Object.keys(files).length} files to src/gamedata`);
