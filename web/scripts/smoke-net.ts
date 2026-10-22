// Hand-run check of the net layer against a running server (make up):
// signs up, logs in, creates a character, opens the socket, says Hello and
// prints the Welcome. Standalone on purpose (Node's fetch and WebSocket,
// raw JSON), so it needs no bundler:
//
//   node scripts/smoke-net.ts [http://localhost:8080]
export {};
const serverOrigin = process.argv[2] ?? 'http://localhost:8080';
const stamp = Date.now().toString(36).slice(-6);
const username = `smoke${stamp}`;

async function call(
  method: string,
  path: string,
  token: string | null,
  body?: unknown,
): Promise<unknown> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }
  const res = await fetch(serverOrigin + path, {
    method,
    headers,
    ...(body !== undefined && { body: JSON.stringify(body) }),
  });
  const text = await res.text();
  if (!res.ok) {
    throw new Error(`${method} ${path}: ${res.status} ${text}`);
  }
  return text ? JSON.parse(text) : undefined;
}

await call('POST', '/api/accounts', null, { username, password: 'correct horse' });
const { token } = (await call('POST', '/api/sessions', null, {
  username,
  password: 'correct horse',
})) as {
  token: string;
};
const character = (await call('POST', '/api/characters', token, {
  name: `S${stamp}`,
  class: 'warrior',
})) as {
  id: string;
  name: string;
};
console.log(`signed up ${username}, character ${character.name} (${character.id})`);

const ws = new WebSocket(serverOrigin.replace('http', 'ws') + '/ws');
ws.addEventListener('open', () => {
  ws.send(
    JSON.stringify({ t: 'Hello', d: { token, protocol_version: 1, character_id: character.id } }),
  );
});
ws.addEventListener('message', (event) => {
  const msg = JSON.parse(String(event.data)) as { t: string; d: Record<string, unknown> };
  if (msg.t === 'Welcome') {
    console.log(`Welcome on ${String(msg.d.map_id)}, token rotated: ${msg.d.token !== token}`);
  }
  if (msg.t === 'Snapshot') {
    console.log('Snapshot received; closing');
    ws.close(1000, 'bye');
  }
});
ws.addEventListener('close', (event) => {
  console.log(`closed with ${event.code}`);
  process.exit(event.code === 1000 ? 0 : 1);
});
setTimeout(() => {
  console.error('no Welcome within 5 s');
  process.exit(1);
}, 5000);
