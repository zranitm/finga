// A chunk worker: builds each chunk asked of it and hands its arrays back (chunk-workers.ts).
import { buildChunk, transfers } from './chunk-build';
import type { ChunkAsk } from './chunk-workers';

self.onmessage = ({ data: { id, req } }: MessageEvent<ChunkAsk>) => {
  buildChunk(req).then(
    (chunk) => self.postMessage({ id, chunk }, { transfer: transfers(chunk) }),
    (err: unknown) =>
      self.postMessage({ id, error: err instanceof Error ? err.message : String(err) }),
  );
};
