import js from '@eslint/js';
import ts from 'typescript-eslint';
import hooks from 'eslint-plugin-react-hooks';
export default ts.config(js.configs.recommended, ...ts.configs.recommended, {
  files: ['**/*.{ts,tsx}'], plugins: { 'react-hooks': hooks },
  rules: { ...hooks.configs.recommended.rules },
}, { ignores: ['.staging/**', 'node_modules/**', 'playwright-report/**', 'test-results/**'] });
