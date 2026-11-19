import { afterAll, afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { servePublic } from '@/testing/public-on-disk';
import { TINY } from '@/testing/region-fixture';

import { buildChunk, transfers } from './chunk-build';
import type { BuiltChunk, ChunkRequest } from './chunk-build';
import type { ChunkAsk } from './chunk-workers';
import { workerChunks } from './chunk-workers';
import { linearPalette } from './ground-mesh';
import { bakeUrl, chunkFile } from './region-bake';

const request = (cx: number, cz: number): ChunkRequest => ({
  url: bakeUrl(TINY, chunkFile(cx, cz)),
  cell: 2,
  linear: linearPalette([0x8cc265, 0x6f6a63, 0xa88b62, 0xe2cf9a, 0xeef2f6, 0x5b8fc7, 0x9c5d4b]),
  coarsest: 4,
});

/** A worker as the page has them, answering on its own thread here: what chunk-worker.ts does, or an error for the chunk failing names. */
class FakeWorker {
  static made: FakeWorker[] = [];
  static failing = '';
  onmessage: ((e: { data: unknown }) => void) | null = null;
  onerror: ((e: { message: string }) => void) | null = null;
  readonly asked: number[] = [];
  readonly work: Promise<void>[] = []; // each build, answered or not
  ended = false;

  constructor(readonly url: URL) {
    FakeWorker.made.push(this);
  }

  terminate(): void {
    this.ended = true;
  }

  postMessage({ id, req }: ChunkAsk): void {
    this.asked.push(id);
    if (req.url.endsWith(FakeWorker.failing)) {
      void Promise.resolve().then(() => this.onerror?.({ message: 'it stopped' }));
      return;
    }
    this.work.push(
      buildChunk(req).then(
        (chunk) => this.onmessage?.({ data: { id, chunk } }),
        (err: Error) => this.onmessage?.({ data: { id, error: err.message } }),
      ),
    );
  }
}

describe('chunks built on workers', () => {
  beforeEach(() => {
    servePublic();
    FakeWorker.made = [];
    FakeWorker.failing = '-';
    vi.stubGlobal('Worker', FakeWorker);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('starts its workers at the first chunk, then deals chunks out among them', async () => {
    const chunks = workerChunks(2);
    expect(FakeWorker.made).toEqual([]);
    const got = await Promise.all([
      chunks.build(request(0, 0)),
      chunks.build(request(1, 0)),
      chunks.build(request(0, 1)),
    ]);
    expect(got.map((c) => [c.cx, c.cz])).toEqual([
      [0, 0],
      [1, 0],
      [0, 1],
    ]);
    expect(FakeWorker.made.map((w) => w.asked)).toEqual([[0, 2], [1]]);
    expect(FakeWorker.made[0]!.url.pathname).toMatch(/chunk-worker\.ts$/);
  });

  it("fails a chunk the worker couldn't build, and a stopped worker's chunks but not the other's", async () => {
    const chunks = workerChunks(2);
    await expect(
      chunks.build({ ...request(0, 0), url: bakeUrl(TINY, chunkFile(7, 7)) }),
    ).rejects.toThrow(/404/);
    FakeWorker.failing = chunkFile(1, 1); // asked as id 1, of the second worker
    const [lost, kept] = await Promise.allSettled([
      chunks.build(request(1, 1)),
      chunks.build(request(0, 0)),
    ]);
    expect(lost).toMatchObject({
      status: 'rejected',
      reason: new Error('chunk worker: it stopped'),
    });
    expect(kept).toMatchObject({ status: 'fulfilled' });
  });

  it('ends its workers at close, leaving what they were asked unanswered, and starts new ones at the next chunk', async () => {
    const chunks = workerChunks(2);
    chunks.close(); // none started yet
    expect(FakeWorker.made).toEqual([]);
    await chunks.build(request(0, 0));
    const first = [...FakeWorker.made];
    let answered = false;
    void chunks.build(request(1, 0)).then(() => (answered = true));
    chunks.close();
    expect(first.map((w) => w.ended)).toEqual([true, true]);
    await Promise.all(first.flatMap((w) => w.work)); // the fake still answers; the pool lets it go
    await new Promise<void>((r) => setImmediate(r));
    expect(answered).toBe(false);
    expect((await chunks.build(request(0, 1))).cz).toBe(1);
    expect(FakeWorker.made.length).toBe(4);
    expect(FakeWorker.made.slice(2).map((w) => w.ended)).toEqual([false, false]);
  });
});

describe('a chunk worker', () => {
  const posted: { data: unknown; transfer: unknown }[] = [];

  beforeEach(async () => {
    servePublic();
    posted.length = 0;
    vi.spyOn(self, 'postMessage').mockImplementation((data: unknown, options?: unknown) => {
      posted.push({
        data,
        transfer: (options as StructuredSerializeOptions | undefined)?.transfer,
      });
    });
    await import('./chunk-worker');
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  afterAll(() => {
    self.onmessage = null; // the module set it on this page, which other tests share
  });

  it('answers each request with the chunk built, handing its arrays over, or with why not', async () => {
    const ask = (data: ChunkAsk) => {
      self.onmessage!(new MessageEvent('message', { data }));
    };
    ask({ id: 3, req: request(1, 0) });
    ask({ id: 4, req: { ...request(0, 0), url: bakeUrl(TINY, chunkFile(9, 9)) } });
    await vi.waitFor(() => expect(posted.length).toBe(2));
    const answer = (id: number) => posted.find((p) => (p.data as { id: number }).id === id)!;
    const chunk = (answer(3).data as { chunk: BuiltChunk }).chunk;
    expect(chunk).toEqual(await buildChunk(request(1, 0)));
    expect(answer(3).transfer).toEqual(transfers(chunk));
    expect((answer(4).data as { error: string }).error).toMatch(/chunks\/9_9\.bin: 404/);
  });
});
