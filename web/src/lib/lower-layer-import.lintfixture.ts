// A deliberate rule violation, never imported by code: lib (layer 1)
// importing from app (layer 5). scripts/check-layers.mjs asserts that ESLint
// flags it; the normal lint run ignores *.lintfixture.ts files.
import { App } from '@/app/App';

export const fixture = App;
