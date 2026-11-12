// Vitest runs without globals, so Testing Library's automatic cleanup is off.
import '@testing-library/jest-dom/vitest';
import { cleanup } from '@testing-library/react';
import { afterEach, beforeEach, vi } from 'vitest';

import { resetStores } from './reset-stores';

beforeEach(() => {
  vi.spyOn(console, 'info').mockImplementation(() => undefined); // the socket's line for each close
});

// A worker runs file after file in one module graph (isolate: false), so each test leaves it clean for the next.
afterEach(() => {
  cleanup();
  resetStores();
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  localStorage.clear();
  sessionStorage.clear();
});
