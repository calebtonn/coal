# COAL POC status

Updated 2026-11-04. Branch `poc/go`. Data dir for all testing: `.sandbox/coal` (excluded from git).

## Done

- Read the review, repo docs, issues #1 to #9 and #11 to #15, PR #10, and the Claude Code hooks reference.
- Installed Go 1.27.3 under `~/.local/go`.
- Wrote `docs/poc/PLAN.md`.
- M0 toolchain and scaffold (`cc67131`): `go.mod`, `coal version`, `coal init`, `COAL_HOME` and `--home`, `Makefile`, gitignore rules.
- M1 schema and manifest types: schema changes (`ad0e43e`); `internal/manifest`, `spec/embed.go` and `coal validate` in the commit that adds this line.

- PR #10 fetched as `pr-10` and merged into `poc/go` (`0c95b8b`); ADR-011 to ADR-015 marked accepted for the POC. Follow-up edits to F5, T08, T09, the README phase table, the definition of done and the capture diagram (`731d9bf`).

- M7 web UI on Svelte 5 and `@davenicoll/web-framework` (`1b6c815`, `ce6489d`, `6e0b921`): 15 Vitest tests pass, svelte-check clean. It depends on the sibling checkout through symlinks, so `preserveSymlinks` is set in the Vite and TypeScript configs. Built against the M4 API contract; not yet wired to a daemon. The Ajv validator is generated at build time (`748f9f5`), so no `unsafe-eval` is needed; regenerate with `bun run gen:validator` in `web/` after a schema change (a test fails if it drifts).
- M2 store: `internal/store` and the `skipped` field on artifact versions (`da41f14`); `coal add`, `coal show`, `coal rm`, and `coal init` through the store, in the commit that adds this line. History commits `manifests/` and `sessions/` only; `config.json` and `.gitignore` stay untracked.
- M3 index and find: `internal/index` on `modernc.org/sqlite` with FTS5, porter stemming, prefix search, bm25 with user summaries first on ties, HTML-escaped snippets and matched columns (`86bf586`, `04025c3`); `coal find` and `coal reindex`, with `coal add` and `coal rm` updating the index (`4af2b5d`). A search over 10,000 manifests takes about 14 ms. `coal find` shows the shortest unique id prefix of at least 8 characters, because UUIDv7 ids from the same minute share their first 8. `internal/app` puts the store and the index behind one API for the CLI, daemon, MCP server and capture writer; `coal add`, `show`, `rm`, `find` and `reindex` use it (`7ef1718`). `coal open <id> [--artifact n]` (n from 1) opens a fresh copy at `tmp/<hex8>-<title>` in the data directory, with an extension from the MIME type when the title has none, so viewers never get the content-addressed original (`936ed4d`, `6128742`, and the commit that adds this text).
- M8 end-to-end demo (`f4743b3`): `scripts/demo.sh` (also `make demo`) builds, creates a fresh `.sandbox/demo-<timestamp>/`, and walks the five definition-of-done steps with PASS or FAIL each, then prints hook timings and `coal stats`. It passed twice in a row on 2026-11-05, once with a full build and once with `SKIP_BUILD=1`; hooks took 23 to 140 ms. `replay-session.sh` and `mcp-smoke.sh` honour `COAL_HOME`, replay takes `PROJECT` and `RESUME=0`, and all three scripts refuse paths outside `.sandbox/` unless `COAL_ALLOW_OUTSIDE_SANDBOX=1`. `docs/poc/DEMO.md` covers the demo, manual commands, an MCP client config, and an optional live Claude Code run for Dave in a sandboxed `CLAUDE_CONFIG_DIR`.
- M6b fold and hooks (`0d69953`, `f371373`, `26995c6`, `a5310ad`): `internal/fold` folds queued events into manifests through `internal/app` with session scratch in `sessions/`; `coal hook <event>`, `coal queue drain`, and `coal adapter install|uninstall claude-code --settings <file>` (the flag is required). The resume rename hint now uses the shortest unique id prefix, and `queue.DrainUntil` bounds the session-start wait at one second. `scripts/replay-session.sh` replays the recorded session in `.sandbox/projects/budget`; each hook takes about 20 to 100 ms as a real process. Tests pass under `-race`. `coal serve` drains the queue on every daemon tick (checked by hand: a queued event was folded within one tick). Capture records `manifest_created`, `session_attached`, `resume_shown` and `resume_acted` through `internal/metrics`.
- M6a capture contract, queue, marker and matching (`219bae6`, `b21a219`, `c4b678b`): `internal/capture` (events, Claude Code translation, summary filter, resume output, sessionEnd extraction), `internal/queue` (flock single writer, `failed/` for bad events), `internal/marker` (git root and remote without shelling out, `.coal` marker, matching), and recorded payloads in `testdata/claude-code/` with `/PROJECT/budget` as a placeholder path. Passes under `go test -race` with Zig as the C compiler.
- M5 MCP server (`28fb2b4`, `8229764`): `coal mcp` on stdio with the Go SDK v1.8.0, six tools and the `coal://manifests/active` resource, all through `internal/app`. `coal_find` returns `{results: [...]}` so structured content is an object for older clients, plus a compact text rendering. `coal_update_state` returns a tool error with the current rev and state on a stale `rev`. `coal_record_intent` sets `source: user` only when the model passes `summaryFromUser: true`, and turns `cwd` into a `path` context. Logs go to `logs/mcp.log`. `scripts/mcp-smoke.sh` passes against the sandbox; it keeps stdin open until the find response arrives, because the server drops in-flight requests at EOF. 7 tests pass, also under `-race`. `coal_find` calls are counted through `internal/metrics` (`85e5f23`).
- M4 daemon (`1b1b247`, `02cba27`): `coal serve [--port 4242]` on 127.0.0.1 only, `internal/daemon` with the API the web UI calls, `If-Match` on PATCH with 412 and the current manifest on a stale rev, artifact bodies with the stored MIME and `Content-Security-Policy: sandbox`, the embedded UI with SPA fallback and a strict CSP carrying the hash of each inline script, a Host and Origin guard, and `logs/daemon.log` rotating at 10 MB. `make web` and `make dev` work. `go test -race` passes with Zig as the C compiler. `Options.Tick` drains the capture queue every two seconds (wired in `a5310ad`). A headless Chromium check (`make e2e`, `d478d9d`) passes with no console errors and no CSP violations; it needed `font-src 'self' data:` for Vite's inlined font subsets, and `style-src 'self'` holds with no `unsafe-inline`.
- M9 rename, log, purge and stats (`d942568`, `1c8eafa`, `411d445`, `03eecb4`, `7931b30`): `internal/metrics` writes and summarises `logs/metrics.jsonl` in the same line format as the MCP server. `coal rename` sets the summary and `source: user`, and an ambiguous id prefix now lists the candidates for every command. `coal log` shows git history with UTC dates and works for a removed manifest by full id. `coal purge` asks for "yes", marks versions `skipped: "purged"`, then deletes the body and any `tmp/` copy, and never deletes a manifest. `coal stats` prints counts and the two ratios. Design rule 6 and T14 updated. The CLAUDE.md line about purge waits for Dave, because agents do not edit CLAUDE.md. Artifacts carry an optional `ext`, with the Agent Trace reference under `ext["dev.agent-trace"]` documented (ADR-015, `948c5d7`); the web validator needs regenerating for it.
- M10 import (`c0662d9`, `1aa777b`): `coal import claude-code --dir <projects-dir> [--accept] [--markers] [--json]` reads Claude Code transcripts with a versioned reader (`claude-code-transcript@1`), skipping lines and files that do not parse and never following symlinks. Sessions group by project root, the git top level while the directory exists, else the recorded cwd. Each group proposes one manifest: the first prompt passing the summary filter (source `user`) or "Work in <base>", state and next step from the newest session, and written files that still exist under the project as artifacts. `createdAt` is the first session's start. Projects that already have a manifest are reported as already recorded. `--dir` is required and `.coal` markers are only written with `--markers`. Tested on synthetic fixtures in `testdata/claude-code/transcripts/` only; Dave's `~/.claude/projects/` was never read.

- End-to-end check by the lead from a fresh sandbox at `85e5f23`: replayed session gives one manifest in the user's words with the HTML artifact, the `.coal` marker and next step "add CSV export"; `coal find`, MCP `coal_find` and `/api/manifests` all return it; the second session start prints the resume line; the daemon serves the artifact with `Content-Security-Policy: sandbox`. Full Go suite and vet pass.

## In progress

- Fable 5.1 review gate on frozen worktree `.sandbox/review-85e5f23`: three reviewers (capture path; storage, schema and index; daemon, MCP and web UI).

- Nothing.

## Next

- The live Claude Code run in DEMO.md, for Dave. Regenerate the web validator for artifact `ext`.

## Issues

Order from Dave, 2026-11-04: implement every issue locally. GitHub issues stay open; nothing is closed or commented on.

| Issue | Status | Commits |
|---|---|---|
| #1 SessionEnd, not Stop | Done: queue, non-blocking drain, SessionEnd hook with `timeout: 5`, fold on SessionStart, stale sessions closed after 12 hours | `731d9bf`, `219bae6`, `b21a219`, `f371373`, `26995c6` |
| #2 How summaries get written | Done for the POC: extraction fallback at sessionEnd unless the model recorded state; the opt-in model summarizer is stretch | `731d9bf`, `219bae6`, `f371373` |
| #3 systemMessage resume | Done: `coal hook session-start` prints systemMessage and additionalContext | `731d9bf`, `219bae6`, `0d69953`, `26995c6` |
| #4 Session matching | Done: marker, contexts, matching, lazy creation, second session attaches | `731d9bf`, `c4b678b`, `f371373` |
| #5 Artifact identity | Schema, store and the MCP `version` argument done | `ad0e43e`, `da41f14`, `28fb2b4` |
| #6 rev compare-and-swap | Done: schema, store, API `If-Match` and the MCP conflict | `ad0e43e`, `da41f14`, `1b1b247`, `28fb2b4` |
| #7 Secrets | Schema rule, deny-list, token scan and `coal purge` done; CLAUDE.md line added by the lead | `ad0e43e`, `da41f14`, `03eecb4` |
| #8 Hooks in other tools | Done | `1d10c78` |
| #9 Smaller inconsistencies | Done. OpenPortable dropped because it could not be found | `ad0e43e`, `b8a7a95` |
| #11 Summary filter, rename | Filter, placeholder upgrade and the rename hint with a unique prefix done; `coal rename` done | `219bae6`, `0d69953`, `f371373`, `1c8eafa` |
| #12 Decision-gate metrics | `coal_find` calls by client name done; capture counters (resume shown and acted on, sessions attached, manifests created) done; `coal stats` and the rename and purge counters done; T14 reads `coal stats` | `28fb2b4`, `a5310ad`, `d942568`, `7931b30` |
| #13 Prior art | Done | `aae12af` |
| #14 Second writer | Done | `aae12af` |
| #15 coal_find retries, porter | Done: porter, prefix, snippet and matched fields, and the `coal_find` description with the retry and active-resource guidance | `86bf586`, `28fb2b4` |

## Blocked

- Nothing.

## Notes for a restart

- Race detector: no gcc on this laptop, so Zig 0.17.1 is installed at `~/.local/zig` as the C compiler. Run `CGO_ENABLED=1 CC="$HOME/.local/zig/zig cc" ~/.local/go/bin/go test -race ./internal/...`. Store, queue, marker, capture and fold pass under it.
- Line budget overruns, accepted: M3 is about 1,100 lines and M6a about 1,635 lines and M6b about 1,250 lines, mostly tests.

- M4 daemon CSP for `index.html`: `default-src 'self'; script-src 'self' 'sha256-...'; style-src 'self'; img-src 'self' data:; frame-src 'self'; object-src 'none'; base-uri 'none'`. Compute the script hash at startup from the inline `data-wf-theme-init` script in the embedded `index.html`, because it changes when the framework changes. If a browser reports style violations, add `'unsafe-inline'` to `style-src` only.

- Go is at `~/.local/go/bin`; the Makefile adds it to PATH.
- `manifest.Manifest` keeps unknown top-level fields in `Extra` and writes them back, so a rewrite does not drop them. Unknown fields inside nested objects are not kept yet.
- The web UI uses Svelte 5 with `@davenicoll/web-framework` from the sibling checkout `../web-framework`, installed with Bun (Dave, 2026-11-04). pnpm is not needed.
- A live Claude Code run would use Dave's account, so it stays optional and waits for his go-ahead. Recorded hook payloads under `testdata/claude-code/` are the test path.
