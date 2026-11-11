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

// classes.yaml: cooldowns become milliseconds.
const classesFile = must(load('classes.yaml'), isRecord, 'classes.yaml');
const classes = must(classesFile.classes, isArray, 'classes.yaml classes').map((raw) => {
  const c = must(raw, isRecord, 'a class');
  const attack = must(c.attack, isRecord, `${String(c.id)} attack`);
  const { cooldown, ...shape } = attack;
  return {
    ...c,
    attack: { ...shape, cooldown_ms: durationMs(cooldown, `${String(c.id)} cooldown`) },
  };
});

// mobs.yaml
const mobsFile = must(load('mobs.yaml'), isRecord, 'mobs.yaml');
const mobs = must(mobsFile.mobs, isArray, 'mobs.yaml mobs').map((raw) => {
  const m = must(raw, isRecord, 'a mob');
  const look = must(m.look, isRecord, `${String(m.id)} look`);
  return {
    ...m,
    elite: m.elite === true,
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
  'mobs.ts': mobs,
  'maps.ts': maps,
  'loot.ts': loot,
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
