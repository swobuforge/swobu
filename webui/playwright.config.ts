import { defineConfig } from '@playwright/test';
export default defineConfig({
  // Journeys share one real Node. Parallel Access/publication mutations would
  // correctly invalidate another journey's in-flight authority review.
  workers: 1,
  testDir: './tests/e2e', use: { baseURL: 'http://127.0.0.1:4178' },
  // Wait for this strict-port child to start. An HTTP probe against an absent
  // loopback listener can hang before startup on filtered host networks.
  // Each browser test still establishes HTTP and application readiness.
  webServer: [{
    command: 'bun x --no-install vite preview --host 127.0.0.1 --port 4178 --strictPort',
    wait: { stdout: /Local:.*127\.0\.0\.1:4178/ },
    timeout: 30_000,
  }, {
    command: 'SWOBU_WEB_PROOF_KEY=synthetic-browser-proof go run -mod=readonly -buildvcs=false ../scripts/webui-daemon',
    wait: { stdout: /Web proof daemon ready:/ },
    timeout: 90_000,
    gracefulShutdown: { signal: 'SIGTERM', timeout: 5000 },
  }],
});
