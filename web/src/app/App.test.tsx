import { act, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { SCENES } from '@/game/keys';
import { REGISTRY } from '@/game/registry';
import type { WorldData } from '@/game/scenes/world';
import { clearToken } from '@/net/token';
import { ERR } from '@/protocol/errors';
import { KickReason } from '@/protocol/messages';
import { chat } from '@/state/chat';
import { combatLog } from '@/state/combat-log';
import { skillBar } from '@/state/skill-bar';
import { ui } from '@/state/ui';
import { fakeFetch, sampleCharacter } from '@/testing/fake-fetch';
import type { FakeFetch, Script } from '@/testing/fake-fetch';
import { FakeGame } from '@/testing/fake-phaser';
import { FakeWebSocket } from '@/testing/fake-socket';
import { resetStores } from '@/testing/reset-stores';
import { user } from '@/testing/user';

import { App } from './App';
import { socketUrl } from './services';

vi.mock('phaser', async () => (await import('@/testing/fake-phaser')).fakePhaser());

const { id, name, level, hp } = sampleCharacter;
const welcome = {
  token: 'tok-rotated',
  character: { id, name, class: sampleCharacter.class, level, hp, maxHp: hp },
  mapId: 'town',
  tick: 1,
  tickRateHz: 30,
};

function fetching(script: Script): FakeFetch {
  const ff = fakeFetch(script);
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
  beforeEach(() => {
    FakeWebSocket.reset();
    vi.stubGlobal('WebSocket', FakeWebSocket);
    FakeGame.reset();
    resetStores();
    parent = document.createElement('div');
    parent.id = 'game';
    document.body.appendChild(parent);
  });
  afterEach(() => {
    clearToken();
    parent.remove();
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
    expect(FakeGame.live()).toHaveLength(1); // the canvas is there behind the login screen
    const game = FakeGame.live()[0]!;

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
    await waitFor(() => expect(game.started).toHaveLength(1));
    const [scene, data] = game.started[0]!;
    const { arriving, visit, ...player } = data as WorldData;
    expect([scene, player, arriving.mapId, arriving.tickRateHz]).toEqual([
      SCENES.World,
      { characterId: 'c-ayla', cls: 'warrior', name: 'Ayla' },
      'town',
      30,
    ]);
    expect(game.registry.get(REGISTRY.visit)).toBe(visit);

    act(() => ui.getState().openWindow('character'));
    ws.push('chatMessage', { sender: 'c-bruno', senderName: 'Bruno', text: 'hi Ayla' });
    act(() => combatLog.getState().questReady('slime_cleanup'));
    expect([chat.getState().lines.length, combatLog.getState().lines.length]).toEqual([1, 1]);
    await user.click(screen.getByRole('button', { name: 'Back to characters' }));
    expect(game.stopped).toEqual([SCENES.World]);
    expect(ui.getState().windows).toEqual([]); // the next Play opens none
    expect([chat.getState().lines, combatLog.getState().lines]).toEqual([[], []]); // the next character starts with a clean chat
    expect(await screen.findByText("alice's characters")).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Log out' }));
    await waitFor(() => expect(screen.getByRole('form', { name: 'Log in' })).toBeInTheDocument());
    expect(ff.of('DELETE', '/api/sessions/current')).toHaveLength(1);
    expect(FakeGame.live()).toHaveLength(1); // the game outlives the session
  });

  it('leaving with a skill bar change still unsaved lists the characters only once it saved', async () => {
    let release: () => void = () => undefined;
    const held = new Promise<void>((resolve) => {
      release = resolve;
    });
    const ff = fakeFetch(({ method, path }) => {
      if (path === '/api/sessions') return { status: 201, body: { token: 'tok-1' } };
      if (path === '/api/characters' && method === 'GET')
        return { status: 200, body: { characters: [sampleCharacter] } };
      if (method === 'PUT') return { status: 200, body: { slots: ['strike', '', '', '', '', ''] } };
      return { status: 404, body: { code: ERR.NO_SUCH_ENDPOINT, message: 'nope' } };
    });
    vi.stubGlobal('fetch', (url: string, init: RequestInit) =>
      init.method === 'PUT' ? held.then(() => ff.fetch(url, init)) : ff.fetch(url, init),
    );
    render(<App />);
    await logIn();
    await user.click(await screen.findByRole('button', { name: 'Play' }));
    const ws = FakeWebSocket.latest();
    ws.open();
    ws.push('welcome', welcome);
    expect(await screen.findByLabelText('Status')).toHaveTextContent('Ayla');

    act(() => skillBar.getState().put(0, 'strike'));
    await user.click(screen.getByRole('button', { name: 'Back to characters' })); // well inside the save's delay
    expect(await screen.findByText("alice's characters")).toBeInTheDocument();
    expect(ff.of('GET', '/api/characters')).toHaveLength(1); // the list waits for the save
    release();
    await waitFor(() => expect(ff.of('GET', '/api/characters')).toHaveLength(2));
    expect(
      ff.calls.filter((c) => c.path.startsWith('/api/characters')).map((c) => c.method),
    ).toEqual(['GET', 'PUT', 'GET']);
  });

  it('a kick while in the world stops the World scene', async () => {
    fetching(({ path }) => {
      if (path === '/api/sessions') return { status: 201, body: { token: 'tok-1' } };
      return { status: 200, body: { characters: [sampleCharacter] } };
    });
    render(<App />);
    const game = FakeGame.live()[0]!;
    await logIn();
    await user.click(await screen.findByRole('button', { name: 'Play' }));
    const ws = FakeWebSocket.latest();
    ws.open();
    ws.push('welcome', welcome);
    await waitFor(() => expect(game.started).toHaveLength(1));
    ws.push('kicked', { reason: KickReason.LOGGED_IN_ELSEWHERE });
    ws.closeFromServer(4003);
    expect(await screen.findByRole('form', { name: 'Log in' })).toBeInTheDocument();
    expect(game.stopped).toEqual([SCENES.World]);
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
      return { status: 200, body: { control_preset: 'classic' } };
    });
    render(<App />);
    await logIn();
    await user.click(await screen.findByRole('button', { name: 'Settings' }));
    expect(screen.getByRole('dialog', { name: 'Settings' })).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Done' }));
    expect(screen.queryByRole('dialog', { name: 'Settings' })).not.toBeInTheDocument();
  });

  it.each([
    { api: '', page: 'http://localhost:5173', want: 'ws://localhost:5173/ws' },
    { api: 'https://play.example', page: 'http://localhost:5173', want: 'wss://play.example/ws' },
  ])('the socket URL for api $api on page $page is $want', ({ api, page, want }) => {
    expect(socketUrl(api, page)).toBe(want);
  });
});
