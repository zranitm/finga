import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { SCENES } from '@/game/keys';
import { GameHandle } from '@/game/main';
import { createRestClient } from '@/net/rest';
import { GameSocket } from '@/net/socket';
import { clearToken } from '@/net/token';
import { fakeFetch, sampleCharacter } from '@/testing/fake-fetch';
import type { Script } from '@/testing/fake-fetch';
import { FakeGame } from '@/testing/fake-phaser';
import { FakeWebSocket, fakeWebSocketClass } from '@/testing/fake-socket';
import { resetStores } from '@/testing/reset-stores';

import { App } from './App';
import { socketUrl } from './services';

vi.mock('phaser', async () => (await import('@/testing/fake-phaser')).fakePhaser());

const welcome = {
  token: 'tok-rotated',
  character: { ...sampleCharacter, max_hp: 204, max_mp: 28 },
  inventory: [],
  map_id: 'town',
  tick: 1,
  server_time: 0,
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
      return { status: 404, body: { code: 'not_found', message: 'nope' } };
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
    expect(await screen.findByText('Entering the world…')).toBeInTheDocument();
    const ws = FakeWebSocket.latest();
    expect(ws.url).toBe('ws://game.test/ws');
    ws.open();
    expect(ws.frames()[0]?.d).toMatchObject({ token: 'tok-1', character_id: 'c-ayla' });
    ws.push({ t: 'Welcome', d: welcome });
    expect(await screen.findByText('Ayla, level 1')).toBeInTheDocument();
    expect(screen.getByText('Connection: connected')).toBeInTheDocument();
    await waitFor(() =>
      expect(game.started).toEqual([
        [SCENES.World, { mapId: 'town', characterId: 'c-ayla', cls: 'warrior' }],
      ]),
    );

    await userEvent.click(screen.getByRole('button', { name: 'Back to characters' }));
    expect(game.stopped).toEqual([SCENES.World]);
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
    ws.push({ t: 'Welcome', d: welcome });
    await waitFor(() => expect(game.started).toHaveLength(1));
    ws.push({ t: 'Kicked', d: { reason: 'logged_in_elsewhere' } });
    ws.closeFromServer(4003);
    expect(await screen.findByRole('form', { name: 'Log in' })).toBeInTheDocument();
    expect(game.stopped).toEqual([SCENES.World]);
    expect(screen.getByText(/logged in somewhere else/i)).toBeInTheDocument();
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
