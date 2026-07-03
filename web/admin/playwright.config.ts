// Copyright (C) 2026 Wepala, LLC
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

import { defineConfig } from '@playwright/test'
import { defineBddConfig } from 'playwright-bdd'

// UI acceptance tests for the embedded admin SPA. The webServer boots the
// real weos binary (which serves the SPA compiled into it) against a clean
// SQLite database, seeded so the dev user exists. The agent API is mocked
// per scenario with page.route — the LLM-independent API behavior is
// covered by the godog suite in tests/e2e; these tests validate the chat
// interface itself. Run with: make test-ui (regenerates web/dist, then
// builds bin/weos so it embeds the current SPA — web/dist is not checked
// in). `npm run test:e2e` alone only works against an up-to-date bin/weos.
const testDir = defineBddConfig({
  features: 'e2e/features/**/*.feature',
  steps: 'e2e/steps/**/*.ts',
})

const dbPath = '/tmp/weos-ui-e2e.db'
const port = 8098

export default defineConfig({
  testDir,
  timeout: 30_000,
  use: {
    baseURL: `http://127.0.0.1:${port}`,
  },
  webServer: {
    // GOOGLE_CLIENT_ID/SECRET are pinned empty so a developer's local .env
    // (godotenv never overrides set variables) cannot flip the test server
    // into OAuth mode — these tests run against dev auth. GEMINI_API_KEY is
    // pinned for the same reason: the not-configured scenario needs the
    // agent unconfigured, and the rest mock the agent API with page.route.
    command:
      `cd ../.. && rm -f ${dbPath} ${dbPath}-shm ${dbPath}-wal && ` +
      `GOOGLE_CLIENT_ID= GOOGLE_CLIENT_SECRET= GEMINI_API_KEY= DATABASE_DSN=${dbPath} ./bin/weos seed && ` +
      `GOOGLE_CLIENT_ID= GOOGLE_CLIENT_SECRET= GEMINI_API_KEY= DATABASE_DSN=${dbPath} SERVER_PORT=${port} ` +
      `./bin/weos serve`,
    url: `http://127.0.0.1:${port}/api/health`,
    reuseExistingServer: false,
    timeout: 60_000,
  },
})
