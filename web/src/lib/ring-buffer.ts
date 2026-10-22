// A fixed-capacity buffer that keeps the newest items: push past the
// capacity overwrites the oldest. Iteration runs from oldest to newest, and
// find looks items up by a key (inputs by sequence number in C08).
export class RingBuffer<T> implements Iterable<T> {
  private readonly items: (T | undefined)[];
  private start = 0; // index of the oldest item
  private count = 0;

  constructor(readonly capacity: number) {
    if (!Number.isInteger(capacity) || capacity < 1) {
      throw new RangeError(`ring buffer capacity must be a positive integer, got ${capacity}`);
    }
    this.items = new Array<T | undefined>(capacity).fill(undefined);
  }

  get size(): number {
    return this.count;
  }

  push(item: T): void {
    const end = (this.start + this.count) % this.capacity;
    this.items[end] = item;
    if (this.count < this.capacity) {
      this.count++;
    } else {
      this.start = (this.start + 1) % this.capacity; // overwrote the oldest
    }
  }

  /** The newest item, or undefined when empty. */
  last(): T | undefined {
    if (this.count === 0) {
      return undefined;
    }
    return this.items[(this.start + this.count - 1) % this.capacity];
  }

  /** The first item, oldest to newest, whose key matches. */
  find<K>(key: (item: T) => K, wanted: K): T | undefined {
    for (const item of this) {
      if (key(item) === wanted) {
        return item;
      }
    }
    return undefined;
  }

  /** Drops the items the predicate rejects, keeping the order. */
  retain(keep: (item: T) => boolean): void {
    const kept = [...this].filter(keep);
    this.items.fill(undefined);
    this.start = 0;
    this.count = 0;
    for (const item of kept) {
      this.push(item);
    }
  }

  clear(): void {
    this.items.fill(undefined);
    this.start = 0;
    this.count = 0;
  }

  *[Symbol.iterator](): Iterator<T> {
    for (let k = 0; k < this.count; k++) {
      yield this.items[(this.start + k) % this.capacity] as T;
    }
  }
}
