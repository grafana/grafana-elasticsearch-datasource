import type { PluginOptions } from '@grafana/plugin-e2e';
import { defineConfig, devices } from '@playwright/test';
import { dirname } from 'node:path';

const pluginE2eAuth = `${dirname(require.resolve('@grafana/plugin-e2e'))}/auth`;

/**
 * Read environment variables from file.
 * https://github.com/motdotla/dotenv
 */
// require('dotenv').config();

// GRAFANA_URL is set only by the Cloud cron workflow (playwright-cloud); its presence signals a
// run against the shared Cloud instance. Kept in sync with isCloudRun in tests/e2e/testEnv.ts,
// which this file cannot import — Playwright loads the config before TypeScript path resolution
// applies to the test directory.
const isCloudRun = !!process.env.GRAFANA_URL;

/**
 * See https://playwright.dev/docs/test-configuration.
 */
export default defineConfig<PluginOptions>({
  testDir: './tests/e2e',
  /* Run tests in files in parallel */
  fullyParallel: true,
  /* Fail the build on CI if you accidentally left test.only in the source code. */
  forbidOnly: !!process.env.CI,
  /* Retry on CI only */
  retries: process.env.CI ? 2 : 0,
  /* The shared Cloud instance serves several suites at once, so a second worker competes for it
     and slows the first paint every test waits on. Run one at a time there. */
  workers: process.env.CI ? 1 : undefined,
  /* On the Cloud instance every test re-downloads the plugin bundle and Monaco assets over the
     CDN, and queries cross a Private Data Source Connect tunnel. Both push first paint and query
     round-trips past Playwright's local-run defaults. */
  timeout: isCloudRun ? 90_000 : 30_000,
  expect: { timeout: isCloudRun ? 30_000 : 5_000 },
  /* Reporter to use. See https://playwright.dev/docs/test-reporters */
  reporter: 'html',
  /* Shared settings for all the projects below. See https://playwright.dev/docs/api/class-testoptions. */
  use: {
    /* Base URL to use in actions like `await page.goto('/')`. */
    baseURL: process.env.GRAFANA_URL || 'http://localhost:3000',

    /* Collect trace when retrying the failed test. See https://playwright.dev/docs/trace-viewer */
    trace: 'on-first-retry',
  },

  /* Configure projects for major browsers */
  projects: [
    // 1. Login to Grafana and store the cookie on disk for use in other tests.
    {
      name: 'auth',
      testDir: pluginE2eAuth,
      testMatch: [/.*\.js/],
    },
    // 2. Create the datasources Cloud runs query. No-op locally, where
    //    provisioning/datasources/datasources.yml supplies them.
    {
      name: 'cloud-setup',
      testMatch: /cloud\.setup\.ts/,
      use: {
        ...devices['Desktop Chrome'],
        storageState: `playwright/.auth/${process.env.GRAFANA_ADMIN_USER || 'admin'}.json`,
      },
      dependencies: ['auth'],
    },
    // 3. Run tests in Google Chrome. Every test will start authenticated as admin user.
    {
      name: 'chromium',
      testIgnore: /cloud\.setup\.ts/,
      use: {
        ...devices['Desktop Chrome'],
        storageState: `playwright/.auth/${process.env.GRAFANA_ADMIN_USER || 'admin'}.json`,
      },
      dependencies: ['cloud-setup'],
    },
  ],
});
