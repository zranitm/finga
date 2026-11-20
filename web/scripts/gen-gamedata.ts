// Node runs this file directly (type stripping), so it may use only erasable TypeScript.
import { existsSync, mkdirSync, readFileSync, readdirSync, writeFileSync } from 'node:fs';
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

/** "600ms", "1.5s" or "2m" to whole milliseconds, as Go's time.Duration parses them. */
function durationMs(value: unknown, what: string): number {
  if (typeof value !== 'string') {
    throw new Error(`gamedata: ${what} must be a duration string like 600ms`);
  }
  const m = /^(\d+(?:\.\d+)?)(ms|s|m)$/.exec(value);
  if (!m) {
    throw new Error(`gamedata: ${what}: cannot parse duration ${value}`);
  }
  return Math.round(Number(m[1]) * { ms: 1, s: 1000, m: 60_000 }[m[2] as 'ms' | 's' | 'm']);
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

// actions.yaml: the cancel and ready points an action takes unless it names its own; energy: the most a player holds and what a basic attack and a block give; a block's arc; and how long a fight event keeps a player in combat.
const actionsFile = must(load('actions.yaml'), isRecord, 'actions.yaml');
const energyFile = must(actionsFile.energy, isRecord, 'actions.yaml energy');
const actions = {
  cancel: must(actionsFile.cancel, isNumber, 'actions.yaml cancel'),
  ready: must(actionsFile.ready, isNumber, 'actions.yaml ready'),
  energy: {
    max: must(energyFile.max, isNumber, 'actions.yaml energy max'),
    per_attack: must(energyFile.per_attack, isNumber, 'actions.yaml energy per_attack'),
    per_block: must(energyFile.per_block, isNumber, 'actions.yaml energy per_block'),
  },
  block: {
    arc: must(
      must(actionsFile.block, isRecord, 'actions.yaml block').arc,
      isNumber,
      'actions.yaml block arc',
    ),
  },
  combat_ms: durationMs(actionsFile.combat, 'actions.yaml combat'),
};

/** An action's hit, cancel and ready points as the server resolves them: what it names, else the start and actions.yaml's points. */
function shares(raw: Record<string, unknown>, what: string) {
  return {
    hit: raw.hit === undefined ? 0 : must(raw.hit, isNumber, `${what} hit`),
    cancel:
      raw.cancel === undefined ? actions.cancel : must(raw.cancel, isNumber, `${what} cancel`),
    ready: raw.ready === undefined ? actions.ready : must(raw.ready, isNumber, `${what} ready`),
  };
}

// classes.yaml: the tree, root first, each class with its own chains; durations become milliseconds, distances stay in metres, and what a class leaves out is null. Starter items and promotions stay on the server, which shows a trainer's conditions in its dialog.
const classesFile = must(load('classes.yaml'), isRecord, 'classes.yaml');
const classes = must(classesFile.classes, isArray, 'classes.yaml classes').map((raw) => {
  const c = must(raw, isRecord, 'a class');
  const id = must(c.id, isString, 'a class id');
  const { length, hit, cancel, ready, ...shape } = must(c.attack, isRecord, `${id} attack`);
  const look = must(c.look, isRecord, `${id} look`);
  return {
    id,
    name: must(c.name, isString, `${id} name`),
    description: must(c.description, isString, `${id} description`),
    parent: c.parent === undefined ? null : must(c.parent, isString, `${id} parent`),
    look: {
      model: must(look.model, isString, `${id} look.model`),
      tint: look.tint === undefined ? null : must(look.tint, isNumber, `${id} look.tint`),
    },
    attack: {
      ...shape,
      length_ms: durationMs(length, `${id} attack length`),
      ...shares({ hit, cancel, ready }, `${id} attack`),
    },
    base_stats:
      c.base_stats === undefined ? null : must(c.base_stats, isRecord, `${id} base_stats`),
    auto_assign:
      c.auto_assign === undefined ? null : must(c.auto_assign, isRecord, `${id} auto_assign`),
    skills: c.skills === undefined ? [] : must(c.skills, isArray, `${id} skills`),
    chains: (c.chains === undefined ? [] : must(c.chains, isArray, `${id} chains`)).map(
      (rawChain) => {
        const chain = must(rawChain, isRecord, `${id} a chain`);
        const root = must(chain.root, isString, `${id} a chain's root`);
        return {
          root,
          follow: must(chain.follow, isArray, `${id} chain ${root}`).map((rawFollow) => {
            const f = must(rawFollow, isRecord, `${id} chain ${root}: a follow-up`);
            const skill = must(f.skill, isString, `${id} chain ${root}: a follow-up's skill`);
            const when = must(f.when, isRecord, `${id} chain ${root}: ${skill}'s when`);
            const what = `${id} chain ${root}: ${skill}`;
            return {
              skill,
              when: {
                after_ms: when.after === undefined ? null : durationMs(when.after, `${what} after`),
                controlled:
                  when.controlled === undefined
                    ? null
                    : must(when.controlled, isArray, `${what} controlled`).map((id) =>
                        must(id, isString, `${what} a controlled status`),
                      ),
                far: when.far === undefined ? null : must(when.far, isNumber, `${what} far`),
                dodged_ms:
                  when.dodged === undefined ? null : durationMs(when.dodged, `${what} dodged`),
                blocked_ms:
                  when.blocked === undefined ? null : durationMs(when.blocked, `${what} blocked`),
                controlled_self:
                  when.controlled_self === undefined
                    ? false
                    : must(when.controlled_self, isBoolean, `${what} controlled_self`),
              },
            };
          }),
        };
      },
    ),
  };
});

// skills.yaml: each skill with every stage resolved, as the server reads them: a stage changes only what it names. Durations become milliseconds, and the class teaching it comes from classes.yaml.
interface Scaled {
  base: number;
  per_point: number;
}
interface Move {
  kind: string;
  distance: number;
  time_ms: number;
  delay_ms: number;
  toward: string;
  stop: boolean;
  through: boolean;
}
interface Inflict {
  status: string;
  time_ms: number;
  power: Scaled;
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
    icon: '',
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
    ready: kind === 'active' ? actions.ready : 0,
    cancelable: false,
    move: null as Move | null,
    dodge_ms: 0,
    block_ms: 0,
    status: null as Inflict | null,
    push: 0,
    hits: null as { count: number; every_ms: number } | null,
    reach_party: false,
    on_hit: null as Inflict | null,
    shield: null as { power: Scaled } | null,
  };
  // move, status and on_hit change only what they name, but a new kind of move, or a new status, starts afresh, as the server's loader resolves them.
  const moveOver = (raw: Record<string, unknown>, was: Move | null, what: string): Move => {
    const kind = raw.kind === undefined ? was?.kind : must(raw.kind, isString, `${what} kind`);
    const m: Move =
      was && kind === was.kind
        ? was
        : {
            kind: kind ?? '',
            distance: 0,
            time_ms: 0,
            delay_ms: 0,
            toward: '',
            stop: kind !== 'blink',
            through: false,
          };
    const through =
      raw.through === undefined ? m.through : must(raw.through, isBoolean, `${what} through`);
    return {
      ...m,
      ...(raw.distance !== undefined && {
        distance: must(raw.distance, isNumber, `${what} distance`),
      }),
      ...(raw.time !== undefined && { time_ms: durationMs(raw.time, `${what} time`) }),
      ...(raw.delay !== undefined && { delay_ms: durationMs(raw.delay, `${what} delay`) }),
      ...(raw.toward !== undefined && { toward: must(raw.toward, isString, `${what} toward`) }),
      ...(raw.stop !== undefined && { stop: must(raw.stop, isBoolean, `${what} stop`) }),
      // passing through mobs, it can't stop at one, as the server's loader resolves it
      ...(through && { stop: false, through }),
    };
  };
  // A status names its status by id, an on_hit by status, as statuses.md gives their keys.
  const inflictOver = (
    raw: Record<string, unknown>,
    key: 'id' | 'status',
    was: Inflict | null,
    what: string,
  ): Inflict => {
    const status =
      raw[key] === undefined ? was?.status : must(raw[key], isString, `${what} ${key}`);
    const i: Inflict =
      was && status === was.status ? was : { status: status ?? '', time_ms: 0, power: zero };
    return {
      ...i,
      ...(raw.time !== undefined && { time_ms: durationMs(raw.time, `${what} time`) }),
      power: scaledOver(raw.power, i.power, `${what} power`),
    };
  };
  const apply = (f: Record<string, unknown>, what: string) => {
    if (f.icon !== undefined) cur.icon = must(f.icon, isString, `${what} icon`);
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
    if (f.ready !== undefined) cur.ready = must(f.ready, isNumber, `${what} ready`);
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
    if (f.move !== undefined) {
      cur.move = moveOver(must(f.move, isRecord, `${what} move`), cur.move, `${what} move`);
    }
    if (f.dodge !== undefined) cur.dodge_ms = durationMs(f.dodge, `${what} dodge`);
    if (f.block !== undefined) cur.block_ms = durationMs(f.block, `${what} block`);
    if (f.status !== undefined) {
      const raw = must(f.status, isRecord, `${what} status`);
      cur.status = inflictOver(raw, 'id', cur.status, `${what} status`);
    }
    if (f.push !== undefined) {
      const raw = must(f.push, isRecord, `${what} push`);
      cur.push = must(raw.distance, isNumber, `${what} push distance`);
    }
    if (f.hits !== undefined) {
      const h = must(f.hits, isRecord, `${what} hits`);
      cur.hits = {
        count:
          h.count === undefined
            ? (cur.hits?.count ?? 0)
            : must(h.count, isNumber, `${what} hits count`),
        every_ms:
          h.every === undefined
            ? (cur.hits?.every_ms ?? 0)
            : durationMs(h.every, `${what} hits every`),
      };
    }
    if (f.reach_party !== undefined) {
      cur.reach_party = must(f.reach_party, isBoolean, `${what} reach_party`);
    }
    if (f.on_hit !== undefined) {
      const raw = must(f.on_hit, isRecord, `${what} on_hit`);
      cur.on_hit = inflictOver(raw, 'status', cur.on_hit, `${what} on_hit`);
    }
    if (f.shield !== undefined) {
      const sh = must(f.shield, isRecord, `${what} shield`);
      cur.shield = {
        power: scaledOver(sh.power, cur.shield?.power ?? zero, `${what} shield power`),
      };
    }
  };
  apply(s, id);
  const stages = must(s.stages, isArray, `${id} stages`).map((rawStage) => {
    const st = must(rawStage, isRecord, `a ${id} stage`);
    apply(st, `${id} ${String(st.name)}`);
    return {
      at: must(st.at, isNumber, `${id} stage at`),
      name: must(st.name, isString, `${id} stage name`),
      description: must(st.description, isString, `${id} stage description`),
      ...cur,
    };
  });
  // The default changes nothing; the extra's fields go on top of the last stage, whose at, name and description it keeps, as the server's loader resolves it.
  const last = stages[stages.length - 1]!;
  const traits = (s.traits === undefined ? [] : must(s.traits, isArray, `${id} traits`)).map(
    (rawTrait, k) => {
      const t = must(rawTrait, isRecord, `a ${id} trait`);
      const trait = {
        id: must(t.id, isString, `a ${id} trait id`),
        name: must(t.name, isString, `${id} trait name`),
        description: must(t.description, isString, `${id} trait description`),
      };
      if (k === 0) return { ...trait, stage: null };
      apply(t, `${id} ${trait.id}`);
      return { ...trait, stage: { ...last, ...cur } };
    },
  );
  return {
    id,
    kind,
    effect: must(s.effect, isString, `${id} effect`),
    class: teacher.get(id) ?? null,
    level: s.level === undefined ? 1 : must(s.level, isNumber, `${id} level`),
    breaks_control:
      s.breaks_control === undefined
        ? false
        : must(s.breaks_control, isBoolean, `${id} breaks_control`),
    granted: s.granted === undefined ? false : must(s.granted, isBoolean, `${id} granted`),
    stages,
    traits,
  };
});

// mobs.yaml: each mob's resolved attacks, times in ms, distances in metres and unused values 0, and what each inflicts or null; an attack's index is MobAttacked's.
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
  const status = v.status === undefined ? null : must(v.status, isRecord, `${what} ${name} status`);
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
    // a boss's attack a block doesn't stop: its area shows red, a blockable one's yellow
    unblockable:
      v.unblockable === undefined
        ? false
        : must(v.unblockable, isBoolean, `${what} ${name} unblockable`),
    status: status && {
      status: must(status.id, isString, `${what} ${name} status id`),
      time_ms:
        status.time === undefined ? 0 : durationMs(status.time, `${what} ${name} status time`),
      power:
        status.power === undefined
          ? 0
          : must(status.power, isNumber, `${what} ${name} status power`),
    },
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
      model: must(look.model, isString, `${String(m.id)} look.model`),
      tint: look.tint === undefined ? null : must(look.tint, isNumber, `${String(m.id)} look.tint`),
      scale:
        look.scale === undefined ? 1 : must(look.scale, isNumber, `${String(m.id)} look.scale`),
    },
  };
});

// statuses.yaml: each status as the world runs it, its factors 1 when unnamed, and damage over time's tick in ms, 0 for others.
const statusesFile = must(load('statuses.yaml'), isRecord, 'statuses.yaml');
const statuses = must(statusesFile.statuses, isArray, 'statuses.yaml statuses').map((raw) => {
  const s = must(raw, isRecord, 'a status');
  const id = must(s.id, isString, 'a status id');
  const factor = (key: string) =>
    s[key] === undefined ? 1 : must(s[key], isNumber, `status ${id} ${key}`);
  return {
    id,
    group: must(s.group, isString, `status ${id} group`),
    blocks:
      s.blocks === undefined
        ? []
        : must(s.blocks, isArray, `status ${id} blocks`).map((b) =>
            must(b, isString, `status ${id} block`),
          ),
    move_speed: factor('move_speed'),
    accuracy: factor('accuracy'),
    attack: factor('attack'),
    physical_defense: factor('physical_defense'),
    magic_defense: factor('magic_defense'),
    shatter: s.shatter === undefined ? 0 : must(s.shatter, isNumber, `status ${id} shatter`),
    wakes: s.wakes === true,
    every_ms: s.every === undefined ? 0 : durationMs(s.every, `status ${id} every`),
    moving: factor('moving'),
  };
});

// stats.yaml: what each point gives, the curves, the limits and each class's bases, for the character window's preview.
const stats = must(load('stats.yaml'), isRecord, 'stats.yaml');

// loot.yaml: how near a pouch opens, and each life in milliseconds.
const lootFile = must(load('loot.yaml'), isRecord, 'loot.yaml');
const loot = {
  reach: must(lootFile.reach, isNumber, 'loot.yaml reach'),
  pouch_life_ms: durationMs(lootFile.pouch_life, 'loot.yaml pouch_life'),
  box_life_ms: durationMs(lootFile.box_life, 'loot.yaml box_life'),
};

// npcs.yaml, each NPC with the map that places it: at x east and z north in metres, facing its yaw, radians from north toward east.
const npcsFile = must(load('npcs.yaml'), isRecord, 'npcs.yaml');
const placed = new Map<string, { map: string; x: number; z: number; yaw: number }>();
// world.yaml: each map's place on the world chart, x across and y down, 0 to 100; and a teleport's channel.
const worldFile = must(load('world.yaml'), isRecord, 'world.yaml');
const places = must(worldFile.places, isRecord, 'world.yaml places');

/** An [x, z] or [x, y] pair of numbers. */
function pair(raw: unknown, what: string): [number, number] {
  const p = must(raw, isArray, what);
  return [must(p[0], isNumber, what), must(p[1], isNumber, what)];
}

// maps/*.yaml: each map's banner, kind, town, levels, art keys, spawns, portals, boss spot and chart place, none for a match's map; its geometry comes from Go (npm run maps).
const maps = [];
const links = new Map<string, readonly [string, string]>(); // each pair of maps a portal joins, once
for (const file of readdirSync(join(dataDir, 'maps'))
  .filter((f) => f.endsWith('.yaml'))
  .sort()) {
  const id = file.replace('.yaml', '');
  const m = must(load(join('maps', file)), isRecord, file);
  const kind = m.kind;
  if (kind !== 'town' && kind !== 'hunting' && kind !== 'pvp') {
    throw new Error(`gamedata: ${file} kind must be town, hunting or pvp`);
  }
  const hunting = kind === 'hunting';
  const pvp = kind === 'pvp';
  const look = must(m.look, isRecord, `${file} look`);
  const levels = hunting ? must(m.levels, isArray, `${file} levels`) : [];
  maps.push({
    id,
    name: must(m.name, isString, `${file} name`),
    kind,
    town: hunting ? must(m.town, isString, `${file} town`) : pvp ? null : id,
    start: m.start === true,
    ground: must(look.ground, isString, `${file} look.ground`),
    sky: must(look.sky, isString, `${file} look.sky`),
    min_level: hunting ? must(levels[0], isNumber, `${file} levels`) : null,
    max_level: hunting ? must(levels[1], isNumber, `${file} levels`) : null,
    spawns: (m.spawns === undefined ? [] : must(m.spawns, isArray, `${file} spawns`)).map((raw) => {
      const sp = must(raw, isRecord, `a ${file} spawn`);
      const [x, z] = pair(sp.at, `${file} spawn ${String(sp.name)} at`);
      return {
        mob: must(sp.mob, isString, `${file} spawn ${String(sp.name)} mob`),
        x,
        z,
        radius: must(sp.radius, isNumber, `${file} spawn ${String(sp.name)} radius`),
      };
    }),
    portals: (m.portals === undefined ? [] : must(m.portals, isArray, `${file} portals`)).map(
      (raw) => {
        const pt = must(raw, isRecord, `a ${file} portal`);
        const name = must(pt.name, isString, `a ${file} portal name`);
        const [x, z] = pair(pt.at, `${file} portal ${name} at`);
        const to = must(
          must(pt.to, isRecord, `${file} portal ${name} to`).map,
          isString,
          `${file} portal ${name} to map`,
        );
        const link = [id, to].sort() as [string, string];
        links.set(link.join(' '), link);
        return {
          name,
          x,
          z,
          radius: must(pt.radius, isNumber, `${file} portal ${name} radius`),
          to,
        };
      },
    ),
    boss: (() => {
      if (m.boss === undefined) {
        return null;
      }
      const b = must(m.boss, isRecord, `${file} boss`);
      const [x, z] = pair(b.at, `${file} boss at`);
      return {
        mob: must(b.mob, isString, `${file} boss mob`),
        x,
        z,
        radius: must(b.radius, isNumber, `${file} boss radius`),
      };
    })(),
    place: pvp ? null : pair(places[id], `world.yaml place of ${id}`),
  });
  placeNpcs(file, id, m.npcs);
}
// regions/*.yaml: each region on the map list as a map of kind region, its own town and on no chart, so no link either: its name and sky, its camps as spawns, its gates as portals; its bake draws its ground (large-regions.md section 4).
const regionsDir = join(dataDir, 'regions');
for (const file of existsSync(regionsDir)
  ? readdirSync(regionsDir)
      .filter((f) => f.endsWith('.yaml'))
      .sort()
  : []) {
  const id = file.replace('.yaml', '');
  const r = must(load(join('regions', file)), isRecord, file);
  maps.push({
    id,
    name: must(r.name, isString, `${file} name`),
    kind: 'region',
    town: id,
    start: false,
    ground: null,
    sky: must(must(r.look, isRecord, `${file} look`).sky, isString, `${file} look.sky`),
    min_level: null,
    max_level: null,
    spawns: (r.camps === undefined ? [] : must(r.camps, isArray, `${file} camps`)).map((raw) => {
      const c = must(raw, isRecord, `a ${file} camp`);
      const [x, z] = pair(c.at, `${file} camp ${String(c.name)} at`);
      return {
        mob: must(c.mob, isString, `${file} camp ${String(c.name)} mob`),
        x,
        z,
        radius: must(c.radius, isNumber, `${file} camp ${String(c.name)} radius`),
      };
    }),
    portals: (r.gates === undefined ? [] : must(r.gates, isArray, `${file} gates`)).map((raw) => {
      const g = must(raw, isRecord, `a ${file} gate`);
      const name = must(g.name, isString, `a ${file} gate name`);
      const [x, z] = pair(g.at, `${file} gate ${name} at`);
      return {
        name,
        x,
        z,
        radius: must(g.radius, isNumber, `${file} gate ${name} radius`),
        to: must(
          must(g.to, isRecord, `${file} gate ${name} to`).region,
          isString,
          `${file} gate ${name} to region`,
        ),
      };
    }),
    boss: null,
    place: null,
  });
  placeNpcs(file, id, r.npcs);
}

/** Places each NPC that file, of map or region id, stands: at x east and z north, facing its yaw in radians. */
function placeNpcs(file: string, id: string, raw: unknown) {
  for (const entry of raw === undefined ? [] : must(raw, isArray, `${file} npcs`)) {
    const n = must(entry, isRecord, `a ${file} npc`);
    const at = must(n.at, isArray, `${file} npc ${String(n.id)} at`);
    placed.set(must(n.id, isString, `a ${file} npc id`), {
      map: id,
      x: must(at[0], isNumber, `${file} npc ${String(n.id)} x`),
      z: must(at[1], isNumber, `${file} npc ${String(n.id)} z`),
      yaw:
        ((n.yaw === undefined ? 0 : must(n.yaw, isNumber, `${file} npc ${String(n.id)} yaw`)) *
          Math.PI) /
        180,
    });
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

// kit.yaml: each ground's colour as 0xrrggbb, which the 3D ground and the map image paint with; the region bakes carry the same palette.
const kitFile = must(load('kit.yaml'), isRecord, 'kit.yaml');
const grounds = Object.fromEntries(
  Object.entries(must(kitFile.grounds, isRecord, 'kit.yaml grounds')).map(([name, rgb]) => {
    const hex = must(rgb, isString, `kit.yaml ground ${name}`);
    if (!/^#[0-9a-f]{6}$/.test(hex)) {
      throw new Error(`gamedata: kit.yaml ground ${name} isn't #rrggbb`);
    }
    return [name, Number.parseInt(hex.slice(1), 16)];
  }),
);

mkdirSync(outDir, { recursive: true });
// pvp.yaml: each mode the queue takes, with its name, side size, whether it's rated and the rounds a match is best of, none for a battle; the count into a round, which holds everyone still, as the client predicts; and the kit's level. The queue's, rating's and bots' constants stay with the server (pvp.md section 3).
const pvpFile = must(load('pvp.yaml'), isRecord, 'pvp.yaml');
const pvp = {
  modes: Object.entries(must(pvpFile.modes, isRecord, 'pvp.yaml modes')).map(([id, raw]) => {
    const m = must(raw, isRecord, `pvp.yaml mode ${id}`);
    const rounds =
      m.rounds === undefined ? null : must(m.rounds, isRecord, `pvp.yaml mode ${id} rounds`);
    return {
      id,
      name: must(m.name, isString, `pvp.yaml mode ${id} name`),
      team: must(m.team, isNumber, `pvp.yaml mode ${id} team`),
      rated: must(m.rated, isBoolean, `pvp.yaml mode ${id} rated`),
      best_of: rounds ? must(rounds.best_of, isNumber, `pvp.yaml mode ${id} best_of`) : null,
    };
  }),
  countdown_ms: durationMs(
    must(pvpFile.rounds, isRecord, 'pvp.yaml rounds').countdown,
    'pvp.yaml rounds countdown',
  ),
  kit_level: must(
    must(pvpFile.kit, isRecord, 'pvp.yaml kit').level,
    isNumber,
    'pvp.yaml kit level',
  ),
};

// outfits.yaml: each wardrobe piece's id, slot and the client look it's drawn with (outfits.md section 3); its name, stars and sources come with the wardrobe window.
const outfitsFile = must(load('outfits.yaml'), isRecord, 'outfits.yaml');
const outfits = {
  pieces: must(outfitsFile.pieces, isArray, 'outfits.yaml pieces').map((raw) => {
    const p = must(raw, isRecord, 'an outfits.yaml piece');
    const id = must(p.id, isString, 'a piece id');
    return {
      id,
      slot: must(p.slot, isString, `piece ${id} slot`),
      look: must(p.look, isString, `piece ${id} look`),
    };
  }),
};

const header =
  '// Generated by scripts/gen-gamedata.ts from ../server/data and the error catalog; do not edit.\n';
// The limits the services enforce and the client shows: quests.yaml's max_active and chat.yaml's line_max.
const chatFile = must(load('chat.yaml'), isRecord, 'chat.yaml');
const limits = {
  max_active_quests: must(questsFile.max_active, isNumber, 'quests.yaml max_active'),
  chat_line_max: must(chatFile.line_max, isNumber, 'chat.yaml line_max'),
  teleport_channel_ms: durationMs(
    must(worldFile.teleport, isRecord, 'world.yaml teleport').channel,
    'world.yaml teleport channel',
  ),
};

// world.yaml's interest block, from which the client takes the distance the server always sends round the player (large-regions.md section 5).
const interestFile = must(worldFile.interest, isRecord, 'world.yaml interest');
const interest = {
  cell: must(interestFile.cell, isNumber, 'world.yaml interest cell'),
  behind: must(interestFile.behind, isNumber, 'world.yaml interest behind'),
  ahead: must(interestFile.ahead, isNumber, 'world.yaml interest ahead'),
  side: must(interestFile.side, isNumber, 'world.yaml interest side'),
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
  'statuses.ts': statuses,
  'mobs.ts': mobs,
  'maps.ts': maps,
  'map-links.ts': [...links.values()].sort((a, b) => a.join(' ').localeCompare(b.join(' '))),
  'loot.ts': loot,
  'actions.ts': actions,
  'stats.ts': stats,
  'npcs.ts': npcs,
  'quests.ts': quests,
  'limits.ts': limits,
  'interest.ts': interest,
  'pvp.ts': pvp,
  'outfits.ts': outfits,
  'error-codes.ts': errorCodes,
  'grounds.ts': grounds,
};
for (const [name, value] of Object.entries(files)) {
  const exportName = name.replace('.ts', '').replace(/-(\w)/g, (_, c: string) => c.toUpperCase());
  const body = `${header}export const ${exportName} = ${JSON.stringify(value, null, 2)} as const;\n`;
  writeFileSync(join(outDir, name), body);
}
console.log(`gamedata: wrote ${Object.keys(files).length} files to src/gamedata`);
