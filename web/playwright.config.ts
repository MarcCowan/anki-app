import { defineConfig, devices } from '@playwright/test'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

const port = 18080
const baseURL = `http://127.0.0.1:${port}`

export default defineConfig({
  testDir: './e2e',
  forbidOnly: !!process.env.CI,
  retries: 0,
  reporter: 'list',
  // Generous on purpose: on a loaded or shared CI runner WebKit can take
  // several seconds to run the app's JS (traces show the server answering in
  // ~60 ms while the browser is starved), so the 5 s default expect timeout is
  // a race against CPU, not a UI check.
  timeout: 60_000,
  expect: { timeout: 15_000 },
  use: {
    baseURL,
    trace: 'retain-on-failure',
  },
  projects: [
    { name: 'iphone', use: { ...devices['iPhone 16'] } },
    { name: 'desktop', use: { ...devices['Desktop Chrome'], viewport: { width: 1440, height: 900 } } },
  ],
  webServer: {
    command: 'node e2e/serve.mjs',
    url: `${baseURL}/api/auth`,
    reuseExistingServer: false,
    timeout: 180_000,
    stdout: 'ignore',
    stderr: 'pipe',
    env: {
      ADDR: `127.0.0.1:${port}`,
      DB_PATH: join(tmpdir(), `anki-e2e-${Date.now()}.db`),
      BACKUP_DIR: join(tmpdir(), `anki-e2e-backups-${Date.now()}`),
      ALLOWED_HOSTS: '127.0.0.1,localhost',
    },
  },
})
