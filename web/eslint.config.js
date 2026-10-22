// @ts-check
import eslint from '@eslint/js';
import prettier from 'eslint-config-prettier';
import importX from 'eslint-plugin-import-x';
import reactHooks from 'eslint-plugin-react-hooks';
import globals from 'globals';
import tseslint from 'typescript-eslint';

// Page 7's layers, top (generic) to bottom (specific). A folder imports only
// from the layers above it, and folders in the same layer never import each
// other; app composes everything. ESLint enforces it with
// import-x/no-restricted-paths, so a wrong import fails `make lint`.
const layers = [
  ['lib', 'config', 'gamedata'],
  ['protocol', 'sim'],
  ['net', 'state', 'input', 'components'],
  ['game', 'features'],
  ['app'],
];

/** @returns {{ target: string, from: string, message: string }[]} */
function forbiddenImports() {
  const zones = [];
  layers.forEach((layer, depth) => {
    for (const folder of layer) {
      // Every folder in the same layer or below is off limits.
      const banned = layers
        .slice(depth)
        .flat()
        .filter((other) => other !== folder);
      for (const other of banned) {
        zones.push({
          target: `./src/${folder}`,
          from: `./src/${other}`,
          message: `${folder} may not import from ${other} (page 7 layers)`,
        });
      }
    }
  });
  // Features never import each other; app composes them.
  zones.push({
    target: './src/features/!(shared)',
    from: './src/features',
    message: 'a feature may not import another feature',
  });
  return zones;
}

export default tseslint.config(
  {
    ignores: ['dist', 'coverage', 'public', 'src/gamedata', 'node_modules', '**/*.lintfixture.ts'],
  },
  eslint.configs.recommended,
  ...tseslint.configs.recommendedTypeChecked,
  ...tseslint.configs.stylisticTypeChecked,
  importX.flatConfigs.recommended,
  importX.flatConfigs.typescript,
  {
    // These two warn about the normal way of importing ESLint plugins (a
    // default export that also has named exports); not worth the noise.
    rules: {
      'import-x/no-named-as-default': 'off',
      'import-x/no-named-as-default-member': 'off',
    },
  },
  {
    files: ['**/*.{ts,tsx}'],
    languageOptions: {
      parserOptions: { projectService: true, tsconfigRootDir: import.meta.dirname },
      globals: { ...globals.browser },
    },
    plugins: { 'react-hooks': reactHooks },
    rules: {
      ...reactHooks.configs.recommended.rules,
      '@typescript-eslint/no-explicit-any': 'error',
      '@typescript-eslint/consistent-type-imports': 'error',
      'import-x/no-restricted-paths': ['error', { zones: forbiddenImports() }],
      'import-x/no-cycle': 'error',
      'import-x/order': [
        'error',
        {
          groups: ['builtin', 'external', 'internal', 'parent', 'sibling'],
          'newlines-between': 'always',
          alphabetize: { order: 'asc', caseInsensitive: true },
        },
      ],
    },
    settings: {
      'import-x/resolver': { typescript: { project: './tsconfig.json' } },
    },
  },
  {
    files: ['scripts/**/*.mjs', 'eslint.config.js', 'vite.config.ts'],
    languageOptions: { globals: { ...globals.node } },
  },
  {
    files: ['scripts/**/*.mjs', 'eslint.config.js'],
    ...tseslint.configs.disableTypeChecked,
  },
  prettier,
);
