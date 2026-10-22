// The negative lint test: a fixture that imports from a lower layer must be
// flagged by import-x/no-restricted-paths, or the layer rule is not being
// enforced. Run by `npm run lint`.
import { ESLint } from 'eslint';

const fixture = 'src/lib/lower-layer-import.lintfixture.ts';
const rule = 'import-x/no-restricted-paths';

const eslint = new ESLint({
  overrideConfig: [{ ignores: ['!**/*.lintfixture.ts'] }],
});
const [result] = await eslint.lintFiles([fixture]);
const hits = (result?.messages ?? []).filter((m) => m.ruleId === rule);
if (hits.length === 0) {
  console.error(`check-layers: ${fixture} was not flagged by ${rule}`);
  process.exit(1);
}
console.log(`check-layers: ${rule} flags a lower-layer import (${hits[0].message})`);
