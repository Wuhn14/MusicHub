import { mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { defineConfig, devices } from '@playwright/test';

// Needs the UI to be built first (npm run build): the Go binary embeds it.
const appPort = 18090;
const fakePort = 18081;
const fakeURL = `http://127.0.0.1:${fakePort}`;
const dataDir = mkdtempSync(join(tmpdir(), 'musichub-e2e-'));
const musicDir = mkdtempSync(join(tmpdir(), 'musichub-e2e-music-'));

export default defineConfig({
  testDir: './e2e',
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : 'list',
  use: {
    baseURL: `http://127.0.0.1:${appPort}`,
    trace: 'retain-on-failure',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: [
    {
      command: `go run ../cmd/fakemb -addr 127.0.0.1:${fakePort}`,
      url: `${fakeURL}/artist?query=lumen`,
      reuseExistingServer: !process.env.CI,
      timeout: 120_000,
    },
    {
      command: 'go run ../cmd/musichub',
      url: `http://127.0.0.1:${appPort}/healthz`,
      reuseExistingServer: !process.env.CI,
      timeout: 120_000,
      env: {
        MUSICHUB_PORT: String(appPort),
        MUSICHUB_DATA: dataDir,
        MUSICHUB_MUSIC: musicDir,
        MUSICHUB_MB_URL: fakeURL,
        MUSICHUB_CAA_URL: fakeURL,
      },
    },
  ],
});
