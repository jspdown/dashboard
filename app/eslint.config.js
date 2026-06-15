import js from '@eslint/js'
import { defineConfig, globalIgnores } from 'eslint/config'
import importX, { createNodeResolver } from 'eslint-plugin-import-x'
import jsxA11y from 'eslint-plugin-jsx-a11y'
import promise from 'eslint-plugin-promise'
import react from 'eslint-plugin-react'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import globals from 'globals'

export default defineConfig([
  globalIgnores(['dist', 'node_modules']),
  {
    files: ['**/*.{js,jsx}'],
    extends: [
      js.configs.recommended,
      react.configs.flat.recommended,
      react.configs.flat['jsx-runtime'],
      reactHooks.configs.flat.recommended,
      reactRefresh.configs.vite,
      jsxA11y.flatConfigs.recommended,
      importX.flatConfigs.recommended,
      promise.configs['flat/recommended'],
    ],
    languageOptions: {
      ecmaVersion: 'latest',
      sourceType: 'module',
      globals: globals.browser,
      parserOptions: {
        ecmaFeatures: { jsx: true },
      },
    },
    settings: {
      react: { version: '19.2' },
      'import-x/resolver-next': [
        createNodeResolver({ extensions: ['.js', '.jsx'] }),
      ],
    },
    rules: {
      // Likely bugs.
      'eqeqeq': ['error', 'always', { null: 'ignore' }],
      'no-console': ['warn', { allow: ['warn', 'error'] }],
      'no-debugger': 'error',
      'no-implicit-coercion': 'error',
      'no-param-reassign': 'error',
      'no-return-assign': 'error',
      'no-self-compare': 'error',
      'no-template-curly-in-string': 'error',
      'no-unmodified-loop-condition': 'error',
      'no-unreachable-loop': 'error',
      'no-unused-vars': ['error', {
        argsIgnorePattern: '^_',
        varsIgnorePattern: '^_',
        caughtErrorsIgnorePattern: '^_',
        ignoreRestSiblings: true,
      }],
      'no-use-before-define': ['error', { functions: false, classes: true }],
      'prefer-const': 'error',
      'require-atomic-updates': 'error',

      // React.
      'react/jsx-no-leaked-render': 'error',
      'react/jsx-no-target-blank': ['error', { warnOnSpreadAttributes: true }],
      'react/jsx-no-useless-fragment': 'error',
      'react/no-array-index-key': 'warn',
      'react/no-unstable-nested-components': ['error', { allowAsProps: true }],
      'react/self-closing-comp': 'error',
      // Not needed with the new JSX transform (React 17+).
      'react/react-in-jsx-scope': 'off',
      'react/prop-types': 'off',

      // The new React Compiler-oriented checks in eslint-plugin-react-hooks
      // v7 are too strict for code that wasn't written for the compiler.
      // Keep the classic Rules of Hooks; turn off the rest.
      'react-hooks/use-memo': 'off',
      'react-hooks/set-state-in-effect': 'off',
      'react-hooks/refs': 'off',

      // Import hygiene.
      'import-x/first': 'error',
      'import-x/newline-after-import': 'error',
      'import-x/no-duplicates': 'error',
      'import-x/no-self-import': 'error',
      'import-x/no-cycle': ['error', { maxDepth: 10 }],
      'import-x/no-useless-path-segments': 'error',
      'import-x/order': ['error', {
        groups: [
          ['builtin', 'external'],
          ['internal', 'parent', 'sibling', 'index'],
        ],
        'newlines-between': 'always',
        alphabetize: { order: 'asc', caseInsensitive: true },
      }],
    },
  },
  {
    // ESLint plugin packages re-export themselves under their named export
    // too, which trips these rules even though the imports are correct.
    files: ['eslint.config.js'],
    rules: {
      'import-x/no-named-as-default': 'off',
      'import-x/no-named-as-default-member': 'off',
    },
  },
  {
    // The Vite config runs in Node, not in the browser, so it needs the
    // Node globals (process, etc.).
    files: ['vite.config.js'],
    languageOptions: {
      globals: globals.node,
    },
  },
])
