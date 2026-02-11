import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import tseslint from 'typescript-eslint'
import { defineConfig, globalIgnores } from 'eslint/config'

export default defineConfig([
  globalIgnores(['dist']),
  {
    files: ['**/*.{ts,tsx}'],
    extends: [
      js.configs.recommended,
      tseslint.configs.strictTypeChecked,
      tseslint.configs.stylisticTypeChecked,
      reactHooks.configs.flat.recommended,
      reactRefresh.configs.vite,
    ],
    languageOptions: {
      ecmaVersion: 2020,
      globals: globals.browser,
      parserOptions: {
        projectService: true,
        tsconfigRootDir: import.meta.dirname,
      },
    },
    rules: {
      // Already enforced by tsconfig verbatimModuleSyntax
      '@typescript-eslint/consistent-type-imports': 'off',

      // ~10 justified non-null assertions; warn to discourage new ones
      '@typescript-eslint/no-non-null-assertion': 'warn',

      // Allow number/boolean in template literals
      '@typescript-eslint/restrict-template-expressions': ['error', {
        allowNumber: true,
        allowBoolean: true,
      }],

      // Catch unhandled promises; allow `void fn()` for fire-and-forget
      '@typescript-eslint/no-floating-promises': ['error', {
        ignoreVoid: true,
      }],

      // Allow async functions as event handler attributes (React pattern)
      '@typescript-eslint/no-misused-promises': ['error', {
        checksVoidReturn: { attributes: false },
      }],

      // Too noisy for `return setState(...)` arrow function patterns
      '@typescript-eslint/no-confusing-void-expression': 'off',

      // Allow `parseInt(x) || 0` pattern (intentional NaN handling)
      '@typescript-eslint/prefer-nullish-coalescing': 'warn',

      // Some defensive checks are intentional; warn not error
      '@typescript-eslint/no-unnecessary-condition': 'warn',

      // Allow underscore-prefixed unused vars (destructuring pattern)
      '@typescript-eslint/no-unused-vars': ['error', {
        argsIgnorePattern: '^_',
        varsIgnorePattern: '^_',
      }],

      // Prefer interfaces (already the codebase pattern)
      '@typescript-eslint/consistent-type-definitions': ['error', 'interface'],

      // Not all thrown values are Errors in third-party code
      '@typescript-eslint/only-throw-promises': 'off',

      // Legitimate React pattern: setState in effects for derived state reset
      'react-hooks/set-state-in-effect': 'off',

      // Ref assignment during render is the standard pattern for keeping
      // callback refs current without re-triggering effects
      'react-hooks/refs': 'off',

      // Variable reassignment in render (e.g. tracking lastSectionId in .map)
      'react-hooks/immutability': 'off',

      // Allow catch(e) with any — typing every catch as unknown is too noisy
      '@typescript-eslint/use-unknown-in-catch-callback-variable': 'off',

      // Lucide icon deprecation warnings are cosmetic, not bugs
      '@typescript-eslint/no-deprecated': 'warn',
    },
  },
])
