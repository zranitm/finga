import { readFileSync } from 'node:fs';
import path from 'node:path';

import { create } from '@bufbuild/protobuf';
import { act, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import {
  FieldBossSchema,
  PartyStateSchema,
  PartyViewSchema,
  WelcomeSchema,
} from '@/protocol/messages';
import { character } from '@/state/character';
import { fieldBoss } from '@/state/field-boss';
import { party } from '@/state/party';
import { target } from '@/state/target';
import { resetStores } from '@/testing/reset-stores';

import { BossBar } from './BossBar';
import { Hud } from './Hud';
import { PartyPanel } from './PartyPanel';
import { TopBars } from './TargetFrame';

describe('the HUD’s panels', () => {
  let style: HTMLStyleElement;
  beforeEach(() => {
    resetStores();
    style = document.createElement('style');
    style.textContent = readFileSync(path.resolve(process.cwd(), 'src/styles/global.css'), 'utf8');
    document.head.append(style);
  });
  afterEach(() => {
    style.remove();
  });

  it('are opaque, so a damage number or a mob behind them doesn’t read through: the status frame, the tray, the party rows, the boss bar and the target window', () => {
    act(() => {
      character.getState().applyWelcome(
        create(WelcomeSchema, {
          token: 'tok',
          character: {
            id: 'c-ayla',
            name: 'Ayla',
            class: 'warrior',
            level: 1,
            hp: 150,
            maxHp: 204,
          },
          mapId: 'forest',
          tick: 1,
        }),
      );
      party.getState().applyPartyState(
        create(PartyStateSchema, {
          party: create(PartyViewSchema, {
            leader: 'c-ayla',
            members: [
              { characterId: 'c-robin', name: 'Robin', level: 12, hp: 40, maxHp: 50, online: true },
            ],
          }),
        }),
      );
      fieldBoss
        .getState()
        .applyFieldBoss(create(FieldBossSchema, { mobId: 9, type: 'king_slime' }));
      fieldBoss.getState().setHp({ hp: 3100, maxHp: 4530 });
      target.getState().show({ kind: 'mob', ref: 'm:4', type: 'green_slime', hp: 34 });
    });
    render(
      <>
        <Hud />
        <PartyPanel />
        <BossBar />
        <TopBars />
      </>,
    );
    const panels = [
      screen.getByRole('group', { name: 'Status' }),
      screen.getByRole('list', { name: 'Skill bar' }).closest('.hud-tray')!,
      screen.getByRole('group', { name: 'Party member Robin' }),
      screen.getByLabelText('Field boss'),
      screen.getByRole('group', { name: 'Target: Green Slime, enemy' }),
    ];
    // jsdom resolves no var(), so this reads the stylesheet's rules for each panel: every background set on it is the panel's own colour, which is opaque
    const rules = [...style.sheet!.cssRules].filter((r) => r instanceof CSSStyleRule);
    const backgrounds = (e: Element) =>
      rules
        .filter((r) => e.matches(r.selectorText))
        .flatMap((r) =>
          ['background', 'background-color']
            .map((k) => r.style.getPropertyValue(k))
            .filter(Boolean),
        );
    expect(panels.map(backgrounds)).toEqual(Array(5).fill(['var(--panel)']));
    const root = rules.find((r) => r.selectorText === ':root')!;
    expect(root.style.getPropertyValue('--panel').trim()).toMatch(/^#[0-9a-f]{6}$/);
  });
});
