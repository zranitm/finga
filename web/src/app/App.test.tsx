import { act, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { SCENES } from '@/game/keys';
import { GameHandle } from '@/game/main';
import { REGISTRY } from '@/game/registry';
import type { WorldData } from '@/game/scenes/world';
import { createRestClient } from '@/net/rest';
import { GameSocket } from '@/net/socket';
import { clearToken } from '@/net/token';
import { ERR } from '@/protocol/errors';
import { KickReason } from '@/protocol/messages';
import { chat } from '@/state/chat';
import { combatLog } from '@/state/combat-log';
import { ui } from '@/state/ui';
import { fakeFetch, sampleCharacter } from '@/testing/fake-fetch';
import type { Script } from '@/testing/fake-fetch';
import { FakeGame } from '@/testing/fake-phaser';
import { FakeWebSocket, fakeWebSocketClass } from '@/testing/fake-socket';
import { resetStores } from '@/testing/reset-stores';

import { App } from './App';
import { socketUrl } from './services';

vi.mock('phaser', async () => (await import('@/testing/fake-phaser')).fakePhaser());

const { id, name, level, hp, mp } = sampleCharacter;
const welcome = {
  token: 'tok-rotated',
  character: { id, name, class: sampleCharacter.class, level, hp, maxHp: hp, mp, maxMp: mp },
  mapId: 'town',
  tick: 1,
  tickRateHz: 30,
};

function services(script: Script) {
  const ff = fakeFetch(script);
  const socket = new GameSocket({ url: 'ws://game.test/ws', WebSocket: fakeWebSocketClass });
  return {
    ff,
    services: {
      rest: createRestClient({ origin: '', fetch: ff.fetch }),
      socket,
      game: new GameHandle({ socket }),
    },
  };
}

describe('App', () => {
  let parent: HTMLDivElement;
  beforeEach(() => {
    FakeWebSocket.reset();
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
    const { ff, services: svc } = services(({ method, path }) => {
      if (path === '/api/sessions') return { status: 201, body: { token: 'tok-1' } };
      if (path === '/api/characters' && method === 'GET')
        return { status: 200, body: { characters: [sampleCharacter] } };
      if (path === '/api/sessions/current') return { status: 204 };
      return { status: 404, body: { code: ERR.NO_SUCH_ENDPOINT, message: 'nope' } };
    });
    render(<App services={svc} />);
    expect(FakeGame.live()).toHaveLength(1); // the canvas is there behind the login screen
    const game = FakeGame.live()[0]!;

    await userEvent.type(screen.getByLabelText('Username'), 'alice');
    await userEvent.type(screen.getByLabelText('Password'), 'correct horse');
    await userEvent.click(screen.getByRole('button', { name: 'Log in' }));
    expect(await screen.findByText("alice's characters")).toBeInTheDocument();
    expect(await screen.findByText('Ayla, level 1 warrior')).toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: 'Play' }));
    expect(await screen.findByRole('button', { name: 'Back to characters' })).toBeInTheDocument();
    const ws = FakeWebSocket.latest();
    expect(ws.url).toBe('ws://game.test/ws');
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
    await userEvent.click(screen.getByRole('button', { name: 'Back to characters' }));
    expect(game.stopped).toEqual([SCENES.World]);
    expect(ui.getState().windows).toEqual([]); // the next Play opens none
    expect([chat.getState().lines, combatLog.getState().lines]).toEqual([[], []]); // the next character starts with a clean chat
    expect(await screen.findByText("alice's characters")).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Log out' }));
    await waitFor(() => expect(screen.getByRole('form', { name: 'Log in' })).toBeInTheDocument());
    expect(ff.of('DELETE', '/api/sessions/current')).toHaveLength(1);
    expect(FakeGame.live()).toHaveLength(1); // the game outlives the session
  });

  it('a kick while in the world stops the World scene', async () => {
    const { services: svc } = services(({ path }) => {
      if (path === '/api/sessions') return { status: 201, body: { token: 'tok-1' } };
      return { status: 200, body: { characters: [sampleCharacter] } };
    });
    render(<App services={svc} />);
    const game = FakeGame.live()[0]!;
    await userEvent.type(screen.getByLabelText('Username'), 'alice');
    await userEvent.type(screen.getByLabelText('Password'), 'correct horse');
    await userEvent.click(screen.getByRole('button', { name: 'Log in' }));
    await userEvent.click(await screen.findByRole('button', { name: 'Play' }));
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
    const { services: svc } = services(({ path }) => {
      if (path === '/api/sessions') return { status: 201, body: { token: 'tok-1' } };
      return { status: 200, body: { characters: [sampleCharacter] } };
    });
    render(<App services={svc} />);
    await userEvent.type(screen.getByLabelText('Username'), 'alice');
    await userEvent.type(screen.getByLabelText('Password'), 'correct horse');
    await userEvent.click(screen.getByRole('button', { name: 'Log in' }));
    await userEvent.click(await screen.findByRole('button', { name: 'Play' }));
    const ws = FakeWebSocket.latest();
    ws.open();
    ws.closeFromServer(1006);
    const dialog = await screen.findByRole('dialog', { name: 'Could not enter the world' });
    expect(dialog).toHaveTextContent('Try again in a moment.');
    expect(dialog).not.toHaveTextContent(/Welcome|1006/);
  });

  it('opens settings from the characters screen', async () => {
    const { services: svc } = services(({ path }) => {
      if (path === '/api/sessions') return { status: 201, body: { token: 'tok-1' } };
      if (path === '/api/characters') return { status: 200, body: { characters: [] } };
      return { status: 200, body: { control_preset: 'classic' } };
    });
    render(<App services={svc} />);
    await userEvent.type(screen.getByLabelText('Username'), 'alice');
    await userEvent.type(screen.getByLabelText('Password'), 'correct horse');
    await userEvent.click(screen.getByRole('button', { name: 'Log in' }));
    await userEvent.click(await screen.findByRole('button', { name: 'Settings' }));
    expect(screen.getByRole('dialog', { name: 'Settings' })).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Done' }));
    expect(screen.queryByRole('dialog', { name: 'Settings' })).not.toBeInTheDocument();
  });

  it.each([
    { api: '', page: 'http://localhost:5173', want: 'ws://localhost:5173/ws' },
    { api: 'https://play.example', page: 'http://localhost:5173', want: 'wss://play.example/ws' },
  ])('the socket URL for api $api on page $page is $want', ({ api, page, want }) => {
    expect(socketUrl(api, page)).toBe(want);
  });
});
