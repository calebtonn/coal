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

## In progress

- M2 store (builder on Opus).

## Next

- M3 index, M4 daemon (strict CSP on `index.html`), M5 MCP, M6 capture, M8 demo, M9 remaining issue work, M10 import.

## Issues

Order from Dave, 2026-11-04: implement every issue locally. GitHub issues stay open; nothing is closed or commented on.

| Issue | Status | Commits |
|---|---|---|
| #1 SessionEnd, not Stop | Docs done; code waits for M6 | `731d9bf` |
| #2 How summaries get written | Docs done; code waits for M6 | `731d9bf` |
| #3 systemMessage resume | Docs done; code waits for M6 | `731d9bf` |
| #4 Session matching | Docs done; code waits for M6 | `731d9bf` |
| #5 Artifact identity | Schema done; store in M2 | `ad0e43e` |
| #6 rev compare-and-swap | Schema done; store in M2, API in M4, MCP in M5 | `ad0e43e` |
| #7 Secrets | Schema rule done; scan in M2; purge in M9 | `ad0e43e` |
| #8 Hooks in other tools | Done | `1d10c78` |
| #9 Smaller inconsistencies | Done. OpenPortable dropped because it could not be found | `ad0e43e`, `b8a7a95` |
| #11 Summary filter, rename | Filter in M6; rename in M9 | |
| #12 Decision-gate metrics | M9 | |
| #13 Prior art | Done | `aae12af` |
| #14 Second writer | Done | `aae12af` |
| #15 coal_find retries, porter | M3 and M5 | |

## Blocked

- Nothing.

## Notes for a restart

- M4 daemon CSP for `index.html`: `default-src 'self'; script-src 'self' 'sha256-...'; style-src 'self'; img-src 'self' data:; frame-src 'self'; object-src 'none'; base-uri 'none'`. Compute the script hash at startup from the inline `data-wf-theme-init` script in the embedded `index.html`, because it changes when the framework changes. If a browser reports style violations, add `'unsafe-inline'` to `style-src` only.

- Go is at `~/.local/go/bin`; the Makefile adds it to PATH.
- `manifest.Manifest` keeps unknown top-level fields in `Extra` and writes them back, so a rewrite does not drop them. Unknown fields inside nested objects are not kept yet.
- The web UI uses Svelte 5 with `@davenicoll/web-framework` from the sibling checkout `../web-framework`, installed with Bun (Dave, 2026-11-04). pnpm is not needed.
- A live Claude Code run would use Dave's account, so it stays optional and waits for his go-ahead. Recorded hook payloads under `testdata/claude-code/` are the test path.
