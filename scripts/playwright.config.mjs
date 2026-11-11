import { defineConfig } from '@playwright/test';
import path from 'node:path';

const artifacts = path.resolve(import.meta.dirname, '../output/playwright/browser-tests');

export default defineConfig({
  testDir: './browser-tests',
  timeout: 180_000,
  expect: { timeout: 15_000 },
  fullyParallel: false,
  workers: 1,
  retries: 0,
  outputDir: path.join(artifacts, 'results'),
  reporter: [
    ['list'],
    ['html', { outputFolder: path.join(artifacts, 'report'), open: 'never' }],
  ],
  use: {
    baseURL: process.env.PROJECT_HUB_BASE_URL || 'http://127.0.0.1:8089',
    browserName: 'chromium',
    headless: true,
    viewport: { width: 1440, height: 1000 },
    locale: 'zh-CN',
    timezoneId: 'Asia/Shanghai',
    acceptDownloads: true,
    actionTimeout: 15_000,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
});
