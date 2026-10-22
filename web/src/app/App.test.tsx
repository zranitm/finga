import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { createRestClient } from '@/net/rest';
import { GameSocket } from '@/net/socket';
import { clearToken } from '@/net/token';
import { fakeFetch, sampleCharacter } from '@/testing/fake-fetch';
import { FakeWebSocket, fakeWebSocketClass } from '@/testing/fake-socket';
import { resetStores } from '@/testing/reset-stores';

import { App } from './App';
import { socketUrl } from './services';

const welcome = {
  token: 'tok-rotated',
  character: { ...sampleCharacter, max_hp: 204, max_mp: 28 },
  inventory: [],
  map_id: 'town',
  tick: 1,
  server_time: 0,
};

describe('App', () => {
  beforeEach(() => {
    FakeWebSocket.reset();
    resetStores();
  });
  afterEach(() => {
    clearToken();
  });

  it('flows from login to the characters screen to the world placeholder, and back out', async () => {
    const ff = fakeFetch(({ method, path }) => {
      if (path === '/api/sessions') return { status: 201, body: { token: 'tok-1' } };
      if (path === '/api/characters' && method === 'GET')
        return { status: 200, body: { characters: [sampleCharacter] } };
      if (path === '/api/sessions/current') return { status: 204 };
      return { status: 404, body: { code: 'not_found', message: 'nope' } };
    });
    const services = {
      rest: createRestClient({ origin: '', fetch: ff.fetch }),
      socket: new GameSocket({ url: 'ws://game.test/ws', WebSocket: fakeWebSocketClass }),
    };
    render(<App services={services} />);

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

    await userEvent.click(screen.getByRole('button', { name: 'Back to characters' }));
    expect(await screen.findByText("alice's characters")).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Log out' }));
    await waitFor(() => expect(screen.getByRole('form', { name: 'Log in' })).toBeInTheDocument());
    expect(ff.of('DELETE', '/api/sessions/current')).toHaveLength(1);
  });

  it('opens settings from the characters screen', async () => {
    const ff = fakeFetch(({ path }) => {
      if (path === '/api/sessions') return { status: 201, body: { token: 'tok-1' } };
      if (path === '/api/characters') return { status: 200, body: { characters: [] } };
      return { status: 200, body: { control_preset: 'classic' } };
    });
    const services = {
      rest: createRestClient({ origin: '', fetch: ff.fetch }),
      socket: new GameSocket({ url: 'ws://game.test/ws', WebSocket: fakeWebSocketClass }),
    };
    render(<App services={services} />);
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
