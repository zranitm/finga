import type Phaser from 'phaser';
import { describe, expect, it, vi } from 'vitest';

import { actions } from '@/gamedata/actions';
import { classes } from '@/gamedata/classes';
import { movement } from '@/gamedata/movement';

import { ANIM, ART_SCALE, TEX, greyKey } from '../keys';
import { greyArt } from '../scenes/grey-art';

import { beatsOf } from './beats';
import { MobView } from './mob';
import { PlayerView, classArt } from './player';

describe('class art', () => {
  it.each([
    { cls: 'warrior', stand: TEX.alienBlueStand, walk: ANIM.alienBlueWalk, tint: null },
    { cls: 'bowman', stand: TEX.alienGreenStand, walk: ANIM.alienGreenWalk, tint: null },
    {
      cls: 'beginner',
      stand: greyKey(TEX.alienBlueStand),
      walk: greyKey(ANIM.alienBlueWalk),
      tint: 0xff6640,
    },
    {
      cls: 'thief',
      stand: greyKey(TEX.alienGreenStand),
      walk: greyKey(ANIM.alienGreenWalk),
      tint: 0x9a70d8,
    },
  ])(
    '$cls draws its own art untinted, or the grey copy under its tint',
    ({ cls, stand, walk, tint }) => {
      expect(classArt(cls)).toMatchObject({ stand, walk, tint });
    },
  );

  it('a class from a newer server is magenta', () => {
    expect(classArt('paladin').tint).toBe(0xff00ff);
  });

  it('every class in the data gets textures and animations the preloader makes', () => {
    const grey = greyArt();
    const textures = new Set<string>([...Object.values(TEX), ...grey.textures.map(greyKey)]);
    const anims = new Set<string>([...Object.values(ANIM), ...grey.animations.map((a) => a.key)]);
    for (const cls of [...classes.map((c) => c.id), 'paladin']) {
      const art = classArt(cls);
      for (const t of [art.stand, art.jump, art.climb, art.hit]) {
        expect(textures, cls).toContain(t);
      }
      expect(anims, cls).toContain(art.walk);
      expect(anims, cls).toContain(art.climbAnim);
    }
  });
});

describe('PlayerView', () => {
  const scene = () => {
    const sprite = {
      x: 300,
      y: 608,
      setOrigin: vi.fn(() => sprite),
      setScale: vi.fn(() => sprite),
      setTint: vi.fn(() => sprite),
      clearTint: vi.fn(() => sprite),
      setTexture: vi.fn(() => sprite),
      setTintMode: vi.fn(() => sprite),
      stop: vi.fn(() => sprite),
      setDepth: vi.fn(() => sprite),
    };
    const label = {
      visible: true,
      width: 40,
      height: 14,
      setOrigin: () => label,
      setDepth: () => label,
    };
    return {
      sprite,
      scene: { add: { sprite: () => sprite, text: () => label } } as unknown as Phaser.Scene,
    };
  };

  it('a new class swaps the art and its tint; the same class changes nothing', () => {
    const { sprite, scene: s } = scene();
    const view = new PlayerView(s, 'beginner', 'Ayla');
    expect(sprite.setTint).toHaveBeenLastCalledWith(0xff6640);
    view.setClass('warrior');
    expect(sprite.setTexture).toHaveBeenLastCalledWith(TEX.alienBlueStand);
    expect(sprite.clearTint).toHaveBeenCalledOnce();
    view.setClass('thief');
    expect(sprite.setTexture).toHaveBeenLastCalledWith(greyKey(TEX.alienGreenStand));
    expect(sprite.setTint).toHaveBeenLastCalledWith(0x9a70d8);
    sprite.setTexture.mockClear();
    view.setClass('thief');
    expect(sprite.setTexture).not.toHaveBeenCalled();
  });

  it("a promotion's new look leaves the name tag where it was", () => {
    const view = new PlayerView(scene().scene, 'beginner', 'Ayla');
    const before = view.nameTag();
    view.setClass('warrior');
    expect(before).not.toBeNull();
    expect(view.nameTag()).toEqual(before);
  });

  it('draws over a boss made after it, name tag and all, so one fighting inside it still shows', () => {
    const made: { kind: string; depth: number }[] = [];
    const add = new Proxy(
      {},
      {
        get: (_t, kind) => () => {
          const o = { kind: String(kind), depth: 0 };
          made.push(o);
          const chain: unknown = new Proxy(() => undefined, {
            get: (_c, key) =>
              key === 'then'
                ? undefined
                : key === 'setDepth'
                  ? (d: number) => ((o.depth = d), chain)
                  : () => chain,
          });
          return chain;
        },
      },
    );
    const s = { add } as unknown as Phaser.Scene;
    new PlayerView(s, 'warrior', 'Ayla');
    new MobView(s, 'king_slime');
    const [sprite, tag, boss] = made;
    expect([sprite?.kind, tag?.kind, boss?.kind]).toEqual(['sprite', 'text', 'sprite']);
    expect(sprite?.depth).toBeGreaterThan(boss?.depth ?? 0);
    expect(tag?.depth).toBeGreaterThan(boss?.depth ?? 0);
  });
});

describe('PlayerView beats', () => {
  /** A sprite that keeps what was last drawn, and a scene that counts the shapes drawn beside it. */
  const drawn = () => {
    const last = { x: 0, angle: 0, scaleX: 0, scaleY: 0, texture: '', tint: -1, mode: 0 };
    const anims = { timeScale: 1 };
    const sprite: unknown = new Proxy(
      {},
      {
        get: (_t, key) => {
          switch (key) {
            case 'anims':
              return anims;
            case 'setPosition':
              return (x: number) => ((last.x = x), sprite);
            case 'setAngle':
              return (a: number) => ((last.angle = a), sprite);
            case 'setScale':
              return (x: number, y = x) => ((last.scaleX = x), (last.scaleY = y), sprite);
            case 'setTexture':
              return (t: string) => ((last.texture = t), sprite);
            case 'setTint':
              return (c: number) => ((last.tint = c), sprite);
            case 'clearTint':
              return () => ((last.tint = -1), sprite);
            case 'setTintMode':
              return (m: number) => ((last.mode = m), sprite);
            default:
              return () => sprite;
          }
        },
      },
    );
    const shapes: unknown[] = [];
    const any: unknown = new Proxy(() => undefined, {
      get: (_t, key) => (key === 'then' ? undefined : () => any),
    });
    const scene = {
      add: {
        sprite: () => sprite,
        text: () => any,
        circle: (...args: unknown[]) => (shapes.push(args), any),
        graphics: (...args: unknown[]) => (shapes.push(args), any),
      },
      tweens: { add: () => any },
    } as unknown as Phaser.Scene;
    return { last, anims, shapes, view: new PlayerView(scene, 'warrior', 'Ayla') };
  };
  const swing = { kind: 'attack', ticks: 21, hit: 0, cancel: 0.85, cancelable: true } as const;
  const at = (x: number, facing: 1 | -1 = 1) => ({
    x,
    y: 608,
    facing,
    on_ground: true,
    climbing: false,
    vx: 0,
    vy: 0,
  });

  it('the strike leans forward, lunges the drawing only, stretches and shows the hit frame; ready at the cancel point', () => {
    const { last, view } = drawn();
    const b = beatsOf(swing, 1000, 50);
    view.act(b);
    view.apply(at(300), 1000);
    expect(last).toMatchObject({ x: 308, angle: 15, texture: TEX.alienBlueHit });
    expect(last.scaleX).toBeGreaterThan(ART_SCALE);
    expect(last.scaleY).toBeLessThan(ART_SCALE);
    expect(view.feet).toEqual({ x: 300, y: 608 });
    view.act(b); // the other way, from its start
    view.apply(at(300, -1), 1000);
    expect(last).toMatchObject({ x: 292, angle: -15 });
    view.act(b);
    view.apply(at(300), 1000);
    view.apply(at(300), b.strikeEnd + 1);
    expect(last.texture).toBe(TEX.alienBlueStand);
    expect(last.angle).toBeGreaterThan(0);
    expect(last.scaleX).toBe(ART_SCALE);
    view.apply(at(300), b.cancelAt);
    expect(last).toMatchObject({ x: 300, angle: 0, texture: TEX.alienBlueStand });
  });

  it("the player's own hit holds the pose 50 ms, then catches up: ready lands on the cancel point all the same", () => {
    const held = drawn();
    const free = drawn();
    const b = beatsOf(swing, 1000, 50);
    held.view.act(b);
    free.view.act(b);
    const hit = b.strikeEnd + 20; // the follow-through, easing toward ready
    expect(held.view.hitStop(hit)).toEqual({ from: hit, until: hit + 50, cancelAt: b.cancelAt });
    held.view.apply(at(300), hit);
    const frozen = { x: held.last.x, angle: held.last.angle };
    held.view.apply(at(300), hit + 49);
    free.view.apply(at(300), hit + 49);
    expect({ x: held.last.x, angle: held.last.angle }).toEqual(frozen);
    expect(free.last.angle).toBeLessThan(frozen.angle);
    held.view.apply(at(300), b.cancelAt - 1);
    expect(held.last.angle).toBeGreaterThan(0); // not ready a moment early
    held.view.apply(at(300), b.cancelAt);
    expect(held.last).toMatchObject({ x: 300, angle: 0 }); // nor late
  });

  it('a hit-stop needs an action to hold, and the next action starts free of it', () => {
    const { last, view } = drawn();
    expect(view.hitStop(1000)).toBeNull();
    view.act(beatsOf(swing, 1000, 50));
    view.hitStop(1010);
    const next = beatsOf(swing, 1020, 50);
    view.act(next);
    view.apply(at(300), next.strikeEnd + 20);
    const after = last.angle;
    const free = drawn();
    free.view.act(next);
    free.view.apply(at(300), next.strikeEnd + 20);
    expect(after).toBe(free.last.angle);
  });

  it("a skill flashes the caster in its colour at the strike, then gives back the class's tint", () => {
    const { last, view } = drawn();
    view.act(beatsOf({ ...swing, kind: 'skill' }, 1000, 50), 0xb070f0);
    view.apply(at(300), 1050);
    expect(last).toMatchObject({ tint: 0xb070f0, mode: 1 });
    view.apply(at(300), 1200);
    expect(last).toMatchObject({ tint: -1, mode: 0 }); // the Warrior's own art takes no tint
  });

  it('nothing flashes or bursts as the cancel point nears: the follow-through easing into ready is the only tell', () => {
    const { last, shapes, view } = drawn();
    const b = beatsOf(swing, 1000, 50);
    view.act(b);
    const before = b.cancelAt - actions.early_press_ms;
    for (const now of [before, before + 20, before + 60]) {
      view.apply(at(300), now);
      expect(last).toMatchObject({ tint: -1, mode: 0 });
      expect(last.angle).toBeGreaterThan(0);
    }
    expect(shapes).toHaveLength(0);
  });

  it.each([0, 200, 350])(
    'turning %i ms into the follow-through eases the pose out: the lunge never jumps to the other side, and no frame moves the drawing more than a walk step and a little',
    (after) => {
      const { last, view } = drawn();
      const b = beatsOf(swing, 1000, 50);
      const frame = 1000 / 60;
      const step = movement.walk_speed / 60;
      view.act(b);
      let x = 300;
      let now = b.strikeEnd + after;
      view.apply(at(x), now);
      expect(last.x - x).toBeGreaterThan(0); // still lunging forward
      let was = { x: last.x, angle: last.angle };
      for (let i = 0; i < 12; i++) {
        x -= step;
        now += frame;
        view.apply({ ...at(x, -1), vx: -movement.walk_speed }, now);
        expect(Math.abs(last.x - was.x)).toBeLessThanOrEqual(step + 1.5);
        expect(Math.abs(last.angle - was.angle)).toBeLessThanOrEqual(3);
        expect(last.x - x).toBeGreaterThanOrEqual(0); // easing back from the old side, never thrown past the feet
        was = { x: last.x, angle: last.angle };
      }
      expect(last).toMatchObject({ x, angle: 0 }); // settled within the 12 frames
      view.apply(at(x), now + frame);
      expect(last).toMatchObject({ x, angle: 0 }); // turning back draws no pose again
    },
  );

  it('a skill walks at the casting pace with a slight recovery lean to its end; an attack ends both at once, and so does a refusal', () => {
    const { last, anims, view } = drawn();
    const strike = { kind: 'skill', ticks: 30, hit: 0, cancel: 0.3, cancelable: true } as const;
    const walking = { ...at(300), vx: 80 };
    const b = beatsOf(strike, 1000, 50);
    view.act(b);
    view.apply(walking, 1100);
    expect(anims.timeScale).toBe(movement.casting_speed);
    view.apply(walking, 2000); // past the cancel point at 1450, before the end at 2500
    expect(anims.timeScale).toBe(movement.casting_speed);
    expect(last.angle).toBeCloseTo(15 * 0.25);
    view.act(beatsOf(swing, 2000, 50)); // an attack cuts Strike short
    view.apply(walking, 2000);
    expect([anims.timeScale, last.angle, last.texture]).toEqual([1, 15, TEX.alienBlueHit]);

    const refused = beatsOf(strike, 3000, 50);
    view.act(refused);
    view.forget(b); // an older action's refusal leaves this one alone
    view.apply(walking, 3100);
    expect(anims.timeScale).toBe(movement.casting_speed);
    view.forget(refused);
    view.apply(walking, 3200);
    expect([anims.timeScale, last.angle]).toEqual([1, 0]);
  });
});
