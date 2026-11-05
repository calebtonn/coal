# MVP Build Tasks

Ordered. Each task is sized for one Claude Code session. Do not start a task until the one before it passes its acceptance criteria. Commit after each task.

Estimated total: six weeks of nights and weekends, or two weeks full time.

## Week 1: Core

### T01. Monorepo scaffold
- pnpm workspace, TypeScript project references, Biome, Vitest configured at root
- Two packages created with `package.json`, `tsconfig.json`, empty `src/index.ts`: `diamond` and `coal`; `packages/coal/web/` holds the Vite app
- **Accept:** `pnpm install && pnpm -r build && pnpm -r test` exit 0 on a clean checkout

### T02. Manifest types and validation (diamond)
- Zod schema matching `docs/04-INTENT-MANIFEST.md` exactly
- `zod-to-json-schema` generates `spec/intent-manifest.schema.json`
- Test: generated schema deep-equals the committed file (fails on drift)
- Test: `spec/examples/*.json` all validate
- Test: a manifest with a `sessionRef` that looks like a bearer token (`^Bearer `, `^sk-`, `^xox`) is rejected
- **Accept:** tests pass; `pnpm --filter @coal/diamond schema:gen` is idempotent

### T03. File storage (diamond)
- `CoalStore` class with `init(dir)`, `createManifest`, `getManifest`, `updateManifest`, `listManifests`, `removeManifest` (moves to trash)
- `registerArtifact(manifestId, {path | content, title, type, mime, producer})` computes sha256, copies body to `artifacts/<hash>`, appends to manifest
- Atomic writes (write temp, rename)
- **Accept:** round-trip tests pass; a manifest created, updated, then read from disk with `JSON.parse` matches

### T04. FTS index (diamond)
- SQLite via `better-sqlite3`; FTS5 virtual table over summary, goal, tags, artifact titles, nextStep
- `reindex()` drops and rebuilds from `manifests/`
- `search(query, {status?, limit?})` returns ranked ids with snippets
- Index updated on every store write
- **Accept:** test seeds 10,000 synthetic manifests, `search` returns in under 100 ms; deleting `index.db` and running `reindex()` restores identical results

## Week 2: Interfaces

### T05. CLI
- Commands per `docs/02-SPEC.md` F3
- `coal find` prints a table (use `cli-table3` or plain padding)
- `coal open` uses `open` / `xdg-open` / `start` by platform
- **Accept:** `coal init && coal add --intent "test" --artifact ./README.md && coal find test` prints the manifest; `coal open <id>` opens README

### T06. MCP server (`coal mcp`)
- `@modelcontextprotocol/sdk` server on stdio exposing the six tools and one resource in F4, as a subcommand of the `coal` binary
- Tool descriptions written for a model reader: say when to call each
- `coal mcp` in CLI launches it
- **Accept:** MCP Inspector lists all tools; `coal_find` returns the T05 manifest; `coal_read_artifact` returns README text

### T07. Daemon (`coal serve`)
- Fastify on 127.0.0.1:4242, routes per F2, as a subcommand of the `coal` binary
- Serves `packages/coal/web/dist` at `/`
- `GET /artifacts/:hash` sets `Content-Type` from stored mime and `Content-Security-Policy: sandbox`
- **Accept:** `curl localhost:4242/health` returns `{ok:true}`; all routes covered by supertest-style tests

## Week 3: Capture

### T08. Claude Code adapter (`coal adapter install claude-code`)
- Internal capture contract per ADR-011; the Claude Code adapter translates hook payloads into it
- Hooks: `SessionStart`, `PostToolUse`, `Stop`, `SessionEnd`, behaving exactly as F5
- `coal adapter install claude-code` merges hook config into `~/.claude/settings.json` without clobbering existing hooks; `uninstall` reverses it
- Lazy manifest creation writes `contexts[]` and the `.coal` marker (ADR-013); a second session in the same directory attaches, never creates
- `SessionEnd` only queues; the queue worker fills `state.summary` as F5 says
- **Accept:** definition-of-done step 1 passes; a second session in the demo directory adds to the same manifest; `SessionEnd` returns in under 500 ms on a cold disk

### T09. Resume (headline feature)
- `SessionStart` matching per F5.1, with the tests listed there
- Banner via `systemMessage`; verify how the current Claude Code build displays it and adjust
- `additionalContext` carrying summary, nextStep, openQuestions, artifacts, and the open-by-offering-to-continue instruction
- `coal resume`: finds the manifest, launches `claude` with a first prompt supplied, so the first turn is the resume
- **Accept:** definition-of-done step 3 passes in both forms: plain `claude` shows the banner before the user types and the first reply offers to continue; `coal resume` opens with the resume as the first turn

## Week 4: Web UI

### T10. Web UI
- Vite + React, no router needed
- Left: search box, results list (summary, status pill, relative time, artifact count)
- Right: manifest detail, artifact tabs, HTML in sandboxed iframe, code in `<pre>`
- Keyboard: `/` focuses search, arrows move selection, enter opens
- **Accept:** definition-of-done step 4 works; Lighthouse accessibility 90+

## Week 5: Hardening

### T11. Error handling, logging, history
- `~/.coal/` becomes a git repo on `coal init` if `git` is on PATH (ADR-014); the daemon commits debounced, never from a hook; `coal log <id>` shows a manifest's history
- Every CLI and MCP error is a one-line human message plus `--verbose` stack
- Daemon logs rotate
- Corrupt manifest file is skipped with a warning, not fatal
- **Accept:** fuzz test writes garbage into `manifests/`, daemon still starts and serves the rest

### T12. Install path
- `npm i -g @coal/coal` works from a tarball (`pnpm pack`)
- `coal init` prints next steps: install adapter, add MCP server to Claude Desktop / Cursor config (print the exact JSON)
- README quickstart updated and tested on a clean VM or container
- **Accept:** a second person follows README only and completes the definition-of-done demo

## Week 6: Demo and decision

### T12b. Publish the schema at its id
- Static site from the repo (GitHub Pages or similar) serving `spec/` at `https://coalmine.io/schema/`
- coalmine.app redirects to coalmine.io
- **Accept:** `curl https://coalmine.io/schema/intent-manifest/0.1.json` returns the committed schema byte for byte

### T13. Demo script and recording
- Write `docs/DEMO.md`: exact steps, exact prompts
- Record a 3 minute screen capture of the end-to-end demo
- **Accept:** recording exists; demo works twice in a row from a fresh `~/.coal`

### T14. Decision gate
- Use COAL for real for one week. Every day, log: did it capture what I built, did I find things by intent, did it get in the way
- Write `docs/RETRO.md` with the answers and a recommendation: continue to Phase 2, pivot, or stop
- **Accept:** retro written; decision made

## Deferred (explicitly not now)

- Browser extension for claude.ai / chatgpt.com capture
- Embedding-based search
- Policy engine, interrupt gating, Slack and calendar signals
- Sync, encryption, accounts, teams
- Published npm packages under a real org
- Website for the spec
