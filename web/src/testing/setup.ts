// Vitest setup: jest-dom's matchers and Testing Library's cleanup between
// tests (Vitest runs without globals, so the automatic cleanup is off).
import '@testing-library/jest-dom/vitest';
import { cleanup } from '@testing-library/react';
import { afterEach } from 'vitest';

afterEach(() => {
  cleanup();
});
