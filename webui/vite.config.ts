import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';

// B01 stages output only. W01 owns replacement of the embedded production assets.
export default defineConfig({ plugins: [react()], test: { include: ['src/**/*.test.ts'] }, build: { outDir: '.staging', emptyOutDir: true } });
