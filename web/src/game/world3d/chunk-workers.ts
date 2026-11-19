// Chunks built on Web Workers, so decoding and building a region's ground never holds up a frame (large-regions.md section 10).
import type { BuiltChunk, ChunkRequest, ChunkSource } from './chunk-build';

/** A request to a worker, and its answer: the chunk built, or why not. */
export interface ChunkAsk {
  readonly id: number;
  readonly req: ChunkRequest;
}

type ChunkAnswer =
  | { readonly id: number; readonly chunk: BuiltChunk }
  | { readonly id: number; readonly error: string };

/** A source spreading chunks over count workers, started at its first request; a worker that fails fails what it was asked. */
export function workerChunks(count: number): ChunkSource {
  const waiting = new Map<
    number,
    { resolve: (c: BuiltChunk) => void; reject: (e: Error) => void }
  >();
  let pool: Worker[] = [];
  let next = 0;
  const start = (): Worker[] =>
    Array.from({ length: count }, (_, w) => {
      const worker = new Worker(new URL('./chunk-worker.ts', import.meta.url), { type: 'module' });
      worker.onmessage = ({ data }: MessageEvent<ChunkAnswer>) => {
        const asked = waiting.get(data.id);
        waiting.delete(data.id);
        if ('chunk' in data) {
          asked?.resolve(data.chunk);
        } else {
          asked?.reject(new Error(data.error));
        }
      };
      worker.onerror = (e) => {
        for (const [id, asked] of waiting) {
          if (id % count === w) {
            waiting.delete(id);
            asked.reject(new Error(`chunk worker: ${e.message}`));
          }
        }
      };
      return worker;
    });
  return (req) =>
    new Promise((resolve, reject) => {
      if (pool.length === 0) {
        pool = start();
      }
      const id = next++;
      waiting.set(id, { resolve, reject });
      pool[id % count]!.postMessage({ id, req } satisfies ChunkAsk);
    });
}
