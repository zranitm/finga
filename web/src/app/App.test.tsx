import { create } from '@bufbuild/protobuf';
import { act, cleanup, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { Arrival } from '@/game/systems/arrival';
import { run } from '@/game/world3d/run';
import { GameSocket } from '@/net/socket';
import { clearToken } from '@/net/token';
import { ERR } from '@/protocol/errors';
import {
  FieldBossSchema,
  KickReason,
  MatchEndedSchema,
  MatchOutcome,
  MatchPhase,
  MatchStateSchema,
  PartyStateSchema,
  PvpOfferSchema,
  PvpQueuedSchema,
} from '@/protocol/messages';
import { arenaBuild } from '@/state/arena-build';
import { character } from '@/state/character';
import { chat } from '@/state/chat';
import { combatLog } from '@/state/combat-log';
import { fieldBoss } from '@/state/field-boss';
import { party } from '@/state/party';
import { pvp } from '@/state/pvp';
import { settings } from '@/state/settings';
import { skillBar } from '@/state/skill-bar';
import { statuses } from '@/state/statuses';
import { target } from '@/state/target';
import { teleport } from '@/state/teleport';
import { ui } from '@/state/ui';
import { wardrobe } from '@/state/wardrobe';
import { fakeFetch, sampleCharacter } from '@/testing/fake-fetch';
import type { FakeFetch, Script } from '@/testing/fake-fetch';
import { FakeWebSocket } from '@/testing/fake-socket';
import { pointerReaches } from '@/testing/pointer';
import { fakePortrait } from '@/testing/portrait';
import { resetStores } from '@/testing/reset-stores';
import { user } from '@/testing/user';

import { App } from './App';
import { socketUrl } from './services';
import type { Services } from './services';

vi.mock('@/game/world3d/run', () => ({ run: vi.fn() }));
const made = vi.hoisted(() => new Set<Services>()); // each App's, whose socket a test may leave mid-join or in the world
vi.mock('./services', async (importOriginal) => {
  const real = await importOriginal<{ createServices: () => Services }>();
  return {
    ...real,
    createServices: () => {
      const s = real.createServices();
      made.add(s);
      return s;
    },
  };
});

const { id, name, level, hp } = sampleCharacter;
const welcome = {
  token: 'tok-rotated',
  character: { id, name, class: sampleCharacter.class, level, hp, maxHp: hp },
  mapId: 'town',
  tick: 1,
  tickRateHz: 30,
};

/** The server: the script, a fresh PvP profile for any character, and the account's wardrobe, which the world reads as it's entered. */
function fetching(script: Script): FakeFetch {
  const ff = fakeFetch((call) =>
    call.method === 'GET' && /^\/api\/characters\/[^/]+\/pvp$/.test(call.path)
      ? { status: 200, body: { standings: [], build: { skill_bar: [], traits: {} } } }
      : call.method === 'GET' && call.path === '/api/wardrobe'
        ? {
            status: 200,
            body: {
              pieces: [{ id: 'knight_plate', cause: 'item', added_at: '2026-11-25T00:00:00Z' }],
            },
          }
        : script(call),
  );
  vi.stubGlobal('fetch', ff.fetch);
  return ff;
}

/** Logs in as alice, each field filled as a paste would: LoginScreen's tests type them a key at a time. */
async function logIn(): Promise<void> {
  for (const [label, text] of [
    ['Username', 'alice'],
    ['Password', 'correct horse'],
  ] as const) {
    await user.click(screen.getByLabelText(label));
    await user.paste(text);
  }
  await user.click(screen.getByRole('button', { name: 'Log in' }));
}

describe('App', () => {
  let parent: HTMLDivElement;
  const close = vi.fn<() => void>(); // what run hands back, which takes the stage down
  beforeEach(() => {
    FakeWebSocket.reset();
    vi.stubGlobal('WebSocket', FakeWebSocket);
    close.mockReset();
    vi.mocked(run).mockReset();
    vi.mocked(run).mockReturnValue({ close, portrait: () => fakePortrait() });
    resetStores();
    parent = document.createElement('div');
    parent.id = 'game';
    document.body.appendChild(parent);
  });
  afterEach(() => {
    for (const s of made) {
      s.socket.close(); // as leaving does: no Welcome wait, ping or retry outlives the test to log into another's console spy
    }
    made.clear();
    clearToken();
    parent.remove();
  });

  it('lets the pointer reach whatever in the world’s overlay a player clicks or hovers, and nothing else, so the rest passes it through to the world', async () => {
    fetching(({ path }) => {
      if (path === '/api/sessions') return { status: 201, body: { token: 'tok-1' } };
      if (path === '/api/settings') return { status: 200, body: {} };
      return { status: 200, body: { characters: [sampleCharacter] } };
    });
    const overlay = document.createElement('div');
    overlay.id = 'ui';
    document.body.appendChild(overlay);
    render(<App />, { container: overlay });
    await logIn();
    await user.click(await screen.findByRole('button', { name: 'Play' }));
    const ws = FakeWebSocket.latest();
    ws.open();
    ws.push('welcome', welcome);
    await screen.findByLabelText('Status');
    const unreachable = () =>
      Array.from(
        overlay.querySelectorAll(
          'button, a[href], input, select, textarea, [role="button"], [title], [tabindex]',
        ),
      )
        .filter((el) => !pointerReaches(el))
        .map((el) => el.outerHTML.slice(0, 120));
    const now = performance.now();
    act(() => {
      const me = [{ id: 'stun', from: now, until: now + 5000 }];
      statuses.getState().show({ me, target: me, party: { 'c-bruno': me } });
      settings.getState().setWalk(true);
      pvp.getState().applyQueued(create(PvpQueuedSchema, { mode: 'duel' }));
      pvp
        .getState()
        .applyOffer(
          create(PvpOfferSchema, { offer: 'of-1', mode: 'duel', bots: 1, untilMs: 10_000 }),
        );
      party.getState().applyPartyState(
        create(PartyStateSchema, {
          party: {
            leader: id,
            members: [
              { characterId: id, name, level, class: 'warrior', hp, maxHp: hp, online: true },
              {
                characterId: 'c-bruno',
                name: 'Bruno',
                level: 3,
                class: 'thief',
                hp: 40,
                maxHp: 50,
                online: true,
              },
            ],
          },
        }),
      );
      target.getState().show({ kind: 'mob', ref: 'm:7', type: 'green_slime', hp: 20 });
      fieldBoss
        .getState()
        .applyFieldBoss(create(FieldBossSchema, { mobId: 9, type: 'king_slime' }));
      fieldBoss.getState().setHp({ hp: 3100, maxHp: 4260 });
      teleport.getState().start({ map: 'forest' });
      character.getState().setDead(true);
    });
    for (const shown of ['Leave the queue', 'Walk mode', 'Stun']) {
      expect(overlay.querySelector(`[aria-label="${shown}"]`), shown).not.toBeNull();
    }
    expect(within(overlay).getAllByText('Bruno').length).toBeGreaterThan(0);
    expect(within(overlay).getByText('Green Slime')).toBeInTheDocument();
    expect(within(overlay).getByRole('button', { name: 'Accept' })).toBeInTheDocument();
    expect(unreachable()).toEqual([]);
    act(() => {
      pvp.getState().applyMatchState(
        create(MatchStateSchema, {
          match: 'mt-1',
          mode: 'duel',
          phase: MatchPhase.RESULT,
          wins: [2, 1],
          sides: [
            { fighters: [{ id, name }] },
            { fighters: [{ id: 'b-1', name: 'Ash', bot: true }] },
          ],
        }),
        0,
      );
      pvp
        .getState()
        .applyMatchEnded(create(MatchEndedSchema, { result: MatchOutcome.WIN, wins: [2, 1] }), id);
    });
    expect(screen.getByRole('dialog', { name: 'Match result' })).toBeInTheDocument();
    expect(unreachable()).toEqual([]);
    for (const through of ['.hud', '.unit-frame', '.minimap', '.top-bars', '.match-count']) {
      const el = overlay.querySelector(through);
      expect(el && pointerReaches(el), through).not.toBe(true); // the world takes the wheel through them
    }
    overlay.remove();
  });

  it('flows from login to the characters screen into the world and back out, driving the game', async () => {
    const ff = fetching(({ method, path }) => {
      if (path === '/api/sessions') return { status: 201, body: { token: 'tok-1' } };
      if (path === '/api/characters' && method === 'GET')
        return { status: 200, body: { characters: [sampleCharacter] } };
      if (path === '/api/sessions/current') return { status: 204 };
      return { status: 404, body: { code: ERR.NO_SUCH_ENDPOINT, message: 'nope' } };
    });
    render(<App />);
    await logIn();
    expect(await screen.findByText("alice's characters")).toBeInTheDocument();
    expect(await screen.findByText('Ayla, level 1 Warrior')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Play' }));
    expect(await screen.findByRole('button', { name: 'Back to characters' })).toBeInTheDocument();
    const ws = FakeWebSocket.latest();
    expect(ws.url).toBe(`ws://${location.host}/ws`); // the page's origin, as VITE_API_ORIGIN is unset
    ws.open();
    expect(ws.frames()[0]?.d).toMatchObject({ token: 'tok-1', characterId: 'c-ayla' });
    ws.push('welcome', welcome);
    expect(await screen.findByLabelText('Status')).toHaveTextContent('Ayla');
    expect(screen.getByText('Lv. 1')).toBeInTheDocument();
    expect(screen.getByRole('meter', { name: 'HP' })).toHaveAttribute('aria-valuenow', '204');
    expect(screen.queryByRole('status')).not.toBeInTheDocument(); // connected: no notice
    await waitFor(() => expect(run).toHaveBeenCalledOnce());
    expect(run).toHaveBeenCalledWith(
      parent,
      { mapId: 'town', tickRateHz: 30, characterId: 'c-ayla', cls: 'warrior', name: 'Ayla' },
      expect.any(Arrival), // holding the map's frames till the world is built
      expect.any(GameSocket),
    ); // drawn in #game

    await waitFor(() => expect(wardrobe.getState().collected).toEqual(new Set(['knight_plate']))); // for the inventory's Add
    act(() => ui.getState().openWindow('character'));
    ws.push('chatMessage', { sender: 'c-bruno', senderName: 'Bruno', text: 'hi Ayla' });
    act(() => combatLog.getState().questReady('slime_cleanup'));
    expect([chat.getState().lines.length, combatLog.getState().lines.length]).toEqual([1, 1]);
    await user.click(screen.getByRole('button', { name: 'Back to characters' }));
    await waitFor(() => expect(close).toHaveBeenCalledOnce());
    expect(ui.getState().windows).toEqual([]); // the next Play opens none
    expect([chat.getState().lines, combatLog.getState().lines]).toEqual([[], []]); // the next character starts with a clean chat
    expect(wardrobe.getState().collected).toBeNull();
    expect(await screen.findByText("alice's characters")).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Log out' }));
    await waitFor(() => expect(screen.getByRole('form', { name: 'Log in' })).toBeInTheDocument());
    expect(ff.of('DELETE', '/api/sessions/current')).toHaveLength(1);
    expect(run).toHaveBeenCalledOnce(); // logging out enters no world
  });

  it('leaving with a skill bar or arena build change still unsaved lists the characters only once both saved', async () => {
    let release: () => void = () => undefined;
    const held = new Promise<void>((resolve) => {
      release = resolve;
    });
    const ff = fakeFetch(({ method, path }) => {
      if (path === '/api/sessions') return { status: 201, body: { token: 'tok-1' } };
      if (path === '/api/characters' && method === 'GET')
        return { status: 200, body: { characters: [sampleCharacter] } };
      if (method === 'PUT')
        return { status: 200, body: { slots: ['strike', '', '', '', '', '', '', ''] } };
      return { status: 404, body: { code: ERR.NO_SUCH_ENDPOINT, message: 'nope' } };
    });
    let arenaRelease: () => void = () => undefined;
    const arenaHeld = new Promise<void>((resolve) => {
      arenaRelease = resolve;
    });
    vi.stubGlobal('fetch', (url: string, init: RequestInit) =>
      init.method !== 'PUT'
        ? ff.fetch(url, init)
        : url.endsWith('/pvp-build')
          ? arenaHeld.then(() => ff.fetch(url, init))
          : held.then(() => ff.fetch(url, init)),
    );
    render(<App />);
    await logIn();
    await user.click(await screen.findByRole('button', { name: 'Play' }));
    const ws = FakeWebSocket.latest();
    ws.open();
    ws.push('welcome', welcome);
    expect(await screen.findByLabelText('Status')).toHaveTextContent('Ayla');

    act(() => skillBar.getState().put(0, 'strike'));
    act(() => arenaBuild.getState().put(0, 'taunt'));
    await user.click(screen.getByRole('button', { name: 'Back to characters' })); // well inside the saves' delay
    expect(await screen.findByText("alice's characters")).toBeInTheDocument();
    expect(ff.of('GET', '/api/characters')).toHaveLength(1); // the list waits for the saves
    arenaRelease(); // the last save handed over, first to land
    await waitFor(() => expect(ff.of('PUT', '/api/characters/c-ayla/pvp-build')).toHaveLength(1));
    expect(ff.of('GET', '/api/characters')).toHaveLength(1); // and for the skill bar's too
    release();
    await waitFor(() => expect(ff.of('GET', '/api/characters')).toHaveLength(2));
    expect(
      ff.calls
        .filter(
          (c) =>
            (c.path.startsWith('/api/characters') && c.method !== 'GET') ||
            c.path === '/api/characters',
        )
        .map((c) => `${c.method} ${c.path}`),
    ).toEqual([
      'GET /api/characters',
      'PUT /api/characters/c-ayla/pvp-build',
      'PUT /api/characters/c-ayla/skill-bar',
      'GET /api/characters',
    ]);
  });

  it('a character played next starts from their own arena build, never the one played before', async () => {
    fetching(({ method, path }) => {
      if (path === '/api/sessions') return { status: 201, body: { token: 'tok-1' } };
      if (method === 'PUT') return { status: 200, body: {} };
      return { status: 200, body: { characters: [sampleCharacter] } };
    });
    render(<App />);
    await logIn();
    for (const play of [1, 2]) {
      await user.click(await screen.findByRole('button', { name: 'Play' }));
      const ws = FakeWebSocket.latest();
      ws.open();
      ws.push('welcome', welcome);
      await waitFor(() =>
        expect(arenaBuild.getState().bar).toEqual(['', '', '', '', '', '', '', '']),
      ); // the profile's
      expect(arenaBuild.getState().edits).toBe(0);
      if (play === 1) {
        act(() => arenaBuild.getState().put(0, 'taunt'));
        await user.click(screen.getByRole('button', { name: 'Back to characters' }));
      }
    }
  });

  it('a call the rate limit refuses in the world says so in a toast, though its caller keeps quiet', async () => {
    vi.stubGlobal(
      'fetch',
      fakeFetch(({ path }) => {
        if (path === '/api/sessions') return { status: 201, body: { token: 'tok-1' } };
        if (path.endsWith('/pvp')) return { status: 429, body: { code: ERR.RATE_LIMITED } };
        if (path === '/api/wardrobe') return { status: 200, body: { pieces: [] } };
        return { status: 200, body: { characters: [sampleCharacter] } };
      }).fetch,
    );
    render(<App />);
    await logIn();
    await user.click(await screen.findByRole('button', { name: 'Play' }));
    const ws = FakeWebSocket.latest();
    ws.open();
    ws.push('welcome', welcome);
    expect(await screen.findByRole('button', { name: 'Too fast. Wait a moment.' })).toBeVisible();
  });

  it('a player entering with walk mode saved is told once that walk mode is on, and with sprint saved is told nothing', async () => {
    const enter = async (walk: boolean) => {
      fetching(({ path }) => {
        if (path === '/api/sessions') return { status: 201, body: { token: 'tok-1' } };
        if (path === '/api/settings') return { status: 200, body: { walk } };
        return { status: 200, body: { characters: [sampleCharacter] } };
      });
      render(<App />);
      await logIn();
      await user.click(await screen.findByRole('button', { name: 'Play' }));
      const ws = FakeWebSocket.latest();
      ws.open();
      ws.push('welcome', welcome);
      expect(await screen.findByLabelText('Status')).toHaveTextContent('Ayla');
    };
    await enter(false);
    await act(() => Promise.resolve());
    expect(screen.queryByRole('button', { name: /Walk mode: no sprint/ })).toBeNull();
    cleanup();
    resetStores();
    await enter(true);
    expect(await screen.findAllByRole('button', { name: /Walk mode: no sprint/ })).toHaveLength(1);
    act(() => settings.getState().setZoom(0.5)); // a map change or a later setting never tells again
    await act(() => Promise.resolve());
    expect(screen.getAllByRole('button', { name: /Walk mode: no sprint/ })).toHaveLength(1);
  });

  it('a zoom changed in the world is saved as the account’s, at the latest as the player leaves', async () => {
    const ff = fetching(({ path }) => {
      if (path === '/api/sessions') return { status: 201, body: { token: 'tok-1' } };
      if (path === '/api/settings') return { status: 200, body: {} };
      return { status: 200, body: { characters: [sampleCharacter] } };
    });
    render(<App />);
    await logIn();
    await user.click(await screen.findByRole('button', { name: 'Play' }));
    const ws = FakeWebSocket.latest();
    ws.open();
    ws.push('welcome', welcome);
    expect(await screen.findByLabelText('Status')).toHaveTextContent('Ayla');
    act(() => settings.getState().setZoom(0.75));
    await user.click(screen.getByRole('button', { name: 'Back to characters' })); // well inside the save's delay
    await waitFor(() =>
      expect(ff.of('PUT', '/api/settings').map((c) => (c.body as { zoom?: number }).zoom)).toEqual([
        0.75,
      ]),
    );
  });

  it('a kick while in the world leaves it', async () => {
    fetching(({ path }) => {
      if (path === '/api/sessions') return { status: 201, body: { token: 'tok-1' } };
      return { status: 200, body: { characters: [sampleCharacter] } };
    });
    render(<App />);
    await logIn();
    await user.click(await screen.findByRole('button', { name: 'Play' }));
    const ws = FakeWebSocket.latest();
    ws.open();
    ws.push('welcome', welcome);
    await waitFor(() => expect(run).toHaveBeenCalledOnce());
    ws.push('kicked', { reason: KickReason.LOGGED_IN_ELSEWHERE });
    ws.closeFromServer(4003);
    expect(await screen.findByRole('form', { name: 'Log in' })).toBeInTheDocument();
    await waitFor(() => expect(close).toHaveBeenCalledOnce());
    expect(screen.getByText(/logged in somewhere else/i)).toBeInTheDocument();
  });

  it('a connection that closes before Welcome says the world could not be entered, in the client’s words', async () => {
    fetching(({ path }) => {
      if (path === '/api/sessions') return { status: 201, body: { token: 'tok-1' } };
      return { status: 200, body: { characters: [sampleCharacter] } };
    });
    render(<App />);
    await logIn();
    await user.click(await screen.findByRole('button', { name: 'Play' }));
    const ws = FakeWebSocket.latest();
    ws.open();
    ws.closeFromServer(1006);
    const dialog = await screen.findByRole('dialog', { name: 'Could not enter the world' });
    expect(dialog).toHaveTextContent('Try again in a moment.');
    expect(dialog).not.toHaveTextContent(/Welcome|1006/);
  });

  it('a failed join closes the socket, so it never retries and enters the world unseen', async () => {
    fetching(({ path }) => {
      if (path === '/api/sessions') return { status: 201, body: { token: 'tok-1' } };
      return { status: 200, body: { characters: [sampleCharacter] } };
    });
    render(<App />);
    await logIn();
    await user.click(await screen.findByRole('button', { name: 'Play' }));
    const random = vi.spyOn(Math, 'random').mockReturnValue(0); // a retry would wait 0 ms
    try {
      const ws = FakeWebSocket.latest();
      ws.open();
      ws.closeFromServer(1006);
      await screen.findByRole('dialog', { name: 'Could not enter the world' });
      await new Promise((resolve) => setTimeout(resolve, 0)); // queued after the 0 ms a retry would wait, whose socket opens at once
      expect(FakeWebSocket.instances).toEqual([ws]);
    } finally {
      random.mockRestore();
    }
  });

  it('opens settings from the characters screen', async () => {
    fetching(({ path }) => {
      if (path === '/api/sessions') return { status: 201, body: { token: 'tok-1' } };
      if (path === '/api/characters') return { status: 200, body: { characters: [] } };
      return { status: 200, body: {} };
    });
    render(<App />);
    await logIn();
    await user.click(await screen.findByRole('button', { name: 'Settings' }));
    expect(screen.getByRole('dialog', { name: 'Settings' })).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Done' }));
    expect(screen.queryByRole('dialog', { name: 'Settings' })).not.toBeInTheDocument();
  });

  it('Esc closes Settings after Key bindings, after Back, and while Reset asks, though the button clicked went away and took the focus with it', async () => {
    fetching(({ path }) => {
      if (path === '/api/sessions') return { status: 201, body: { token: 'tok-1' } };
      if (path === '/api/characters')
        return { status: 200, body: { characters: [sampleCharacter] } };
      if (path === '/api/settings') return { status: 200, body: { keys: { attack: 'KeyB' } } };
      return { status: 404, body: { code: ERR.NO_SUCH_ENDPOINT, message: 'nope' } };
    });
    render(<App />);
    await logIn();
    await user.click(await screen.findByRole('button', { name: 'Play' }));
    const dialog = async () => {
      await user.click(await screen.findByRole('button', { name: 'Settings' }));
      return screen.getByRole('dialog', { name: 'Settings' });
    };
    const escapes = async () => {
      expect(document.activeElement).toBe(document.body); // as the live pass found it
      await user.keyboard('{Escape}');
      return screen.queryByRole('dialog', { name: 'Settings' }) === null;
    };
    await user.click(within(await dialog()).getByRole('button', { name: 'Key bindings' }));
    expect(await escapes()).toBe(true);
    let open = await dialog();
    await user.click(within(open).getByRole('button', { name: 'Key bindings' }));
    await user.click(within(open).getByRole('button', { name: 'Back' }));
    expect(await escapes()).toBe(true);
    open = await dialog();
    await user.click(within(open).getByRole('button', { name: 'Key bindings' }));
    await user.click(within(open).getByRole('button', { name: 'Reset to default' }));
    expect(within(open).getByText('Put every key back on its default?')).toBeInTheDocument();
    expect(await escapes()).toBe(true);
  });

  it('the account’s keys arrive at login, and Settings opens over the world too, its key bindings among them', async () => {
    fetching(({ path }) => {
      if (path === '/api/sessions') return { status: 201, body: { token: 'tok-1' } };
      if (path === '/api/characters')
        return { status: 200, body: { characters: [sampleCharacter] } };
      if (path === '/api/settings') return { status: 200, body: { keys: { attack: 'KeyB' } } };
      return { status: 404, body: { code: ERR.NO_SUCH_ENDPOINT, message: 'nope' } };
    });
    render(<App />);
    await logIn();
    await waitFor(() => expect(settings.getState().keys).toEqual({ attack: 'KeyB' }));
    await user.click(await screen.findByRole('button', { name: 'Play' }));
    await user.click(await screen.findByRole('button', { name: 'Settings' }));
    const dialog = screen.getByRole('dialog', { name: 'Settings' });
    await user.click(within(dialog).getByRole('button', { name: 'Key bindings' }));
    expect(within(dialog).getByRole('button', { name: 'Attack: B' })).toBeInTheDocument();
    await user.click(within(dialog).getByRole('button', { name: 'Back' }));
    await user.click(within(dialog).getByRole('button', { name: 'Done' }));
    expect(screen.queryByRole('dialog', { name: 'Settings' })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Back to characters' })).toBeInTheDocument();
  });

  it('renders on Chrome, whose navigator.keyboard has getLayoutMap and no listeners', async () => {
    vi.stubGlobal('navigator', {
      ...navigator,
      keyboard: { getLayoutMap: () => Promise.resolve(new Map([['KeyQ', 'q']])) },
    });
    render(<App />);
    expect(await screen.findByRole('form', { name: 'Log in' })).toBeInTheDocument();
  });

  it.each([
    { api: '', page: 'http://localhost:5173', want: 'ws://localhost:5173/ws' },
    { api: 'https://play.example', page: 'http://localhost:5173', want: 'wss://play.example/ws' },
  ])('the socket URL for api $api on page $page is $want', ({ api, page, want }) => {
    expect(socketUrl(api, page)).toBe(want);
  });
});
