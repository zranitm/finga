// Vitest runs without globals, so Testing Library's automatic cleanup is off.
import '@testing-library/jest-dom/vitest';
import { cleanup } from '@testing-library/react';
import { afterEach, beforeEach, vi } from 'vitest';

beforeEach(() => {
  vi.spyOn(console, 'info').mockImplementation(() => undefined); // the socket's line for each close
});

afterEach(() => {
  cleanup();
});
