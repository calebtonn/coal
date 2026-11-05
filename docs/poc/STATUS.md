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
- M3 index and find: `internal/index` on `modernc.org/sqlite` with FTS5, porter stemming, prefix search, bm25 with user summaries first on ties, HTML-escaped snippets and matched columns (`86bf586`, `04025c3`); `coal find` and `coal reindex`, with `coal add` and `coal rm` updating the index (`4af2b5d`). A search over 10,000 manifests takes about 14 ms. `coal find` shows the shortest unique id prefix of at least 8 characters, because UUIDv7 ids from the same minute share their first 8. `coal show` and `coal open` are not part of M3 here: show landed in M2 and open waits.
- M6a capture contract, queue, marker and matching (`219bae6`, `b21a219`, `c4b678b`): `internal/capture` (events, Claude Code translation, summary filter, resume output, sessionEnd extraction), `internal/queue` (flock single writer, `failed/` for bad events), `internal/marker` (git root and remote without shelling out, `.coal` marker, matching), and recorded payloads in `testdata/claude-code/` with `/PROJECT/budget` as a placeholder path. `go test -race` could not run because there is no C compiler on this laptop; the queue tests passed 100 runs in a row instead. M6b (folding through the store, `coal hook`, `coal queue drain`, `coal adapter`) is next.

## In progress

- Nothing.

## Next

- M4 daemon (strict CSP on `index.html`), M5 MCP, M6 capture, M8 demo, M9 remaining issue work, M10 import.

## Issues

Order from Dave, 2026-11-04: implement every issue locally. GitHub issues stay open; nothing is closed or commented on.

| Issue | Status | Commits |
|---|---|---|
| #1 SessionEnd, not Stop | Docs done; event queue and non-blocking drain done in M6a; hooks and folding wait for M6b | `731d9bf`, `219bae6`, `b21a219` |
| #2 How summaries get written | Docs done; extraction fallback (`ExtractState`) done in M6a; applying it at sessionEnd waits for M6b | `731d9bf`, `219bae6` |
| #3 systemMessage resume | Docs done; resume JSON (`ResumeOutput`) done in M6a; the `coal hook session-start` command waits for M6b | `731d9bf`, `219bae6` |
| #4 Session matching | Docs done; marker, contexts and matching with the #4 tests done in M6a; lazy creation waits for M6b | `731d9bf`, `c4b678b` |
| #5 Artifact identity | Schema and store done; MCP `version` argument in M5 | `ad0e43e`, `da41f14` |
| #6 rev compare-and-swap | Schema and store done; API in M4, MCP in M5 | `ad0e43e`, `da41f14` |
| #7 Secrets | Schema rule, deny-list and token scan done; purge in M9 | `ad0e43e`, `da41f14` |
| #8 Hooks in other tools | Done | `1d10c78` |
| #9 Smaller inconsistencies | Done. OpenPortable dropped because it could not be found | `ad0e43e`, `b8a7a95` |
| #11 Summary filter, rename | Filter (`PickSummary`) and the rename hint in the resume line done in M6a; rename in M9 | `219bae6` |
| #12 Decision-gate metrics | M9 | |
| #13 Prior art | Done | `aae12af` |
| #14 Second writer | Done | `aae12af` |
| #15 coal_find retries, porter | Porter, prefix, snippet and matched fields done; the `coal_find` description waits for M5 | `86bf586` |

## Blocked

- Nothing.

## Notes for a restart

- M4 daemon CSP for `index.html`: `default-src 'self'; script-src 'self' 'sha256-...'; style-src 'self'; img-src 'self' data:; frame-src 'self'; object-src 'none'; base-uri 'none'`. Compute the script hash at startup from the inline `data-wf-theme-init` script in the embedded `index.html`, because it changes when the framework changes. If a browser reports style violations, add `'unsafe-inline'` to `style-src` only.

- Go is at `~/.local/go/bin`; the Makefile adds it to PATH.
- `manifest.Manifest` keeps unknown top-level fields in `Extra` and writes them back, so a rewrite does not drop them. Unknown fields inside nested objects are not kept yet.
- The web UI uses Svelte 5 with `@davenicoll/web-framework` from the sibling checkout `../web-framework`, installed with Bun (Dave, 2026-11-04). pnpm is not needed.
- A live Claude Code run would use Dave's account, so it stays optional and waits for his go-ahead. Recorded hook payloads under `testdata/claude-code/` are the test path.
