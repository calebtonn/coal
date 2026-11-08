import { defineConfig, devices } from '@playwright/test';

// End-to-end check of the UI as the daemon embeds it. The spec starts its
// own `coal serve` on port 4343 against a fresh sandbox data dir; build
// first with `make web build` (or run `make e2e`).
export default defineConfig({
  testDir: 'e2e',
  workers: 1,
  timeout: 30_000,
  reporter: 'list',
  outputDir: '../.sandbox/e2e-results',
  use: { ...devices['Desktop Chrome'], baseURL: 'http://127.0.0.1:4343', headless: true },
});
