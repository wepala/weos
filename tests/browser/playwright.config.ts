import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./tests",
  timeout: 30_000,
  expect: { timeout: 10_000 },
  retries: 0,
  use: {
    baseURL: "http://localhost:8080",
    extraHTTPHeaders: {
      "X-Dev-Agent": "admin@weos.dev",
    },
    actionTimeout: 10_000,
    navigationTimeout: 15_000,
  },
  projects: [
    {
      name: "chromium",
      use: {
        channel: "chrome",
        headless: true,
        viewport: { width: 1280, height: 800 },
      },
    },
  ],
  webServer: {
    command:
      "cd ../.. && GOOGLE_CLIENT_ID= GOOGLE_CLIENT_SECRET= ./bin/weos serve",
    url: "http://localhost:8080/api/health",
    reuseExistingServer: true,
    timeout: 10_000,
  },
});
