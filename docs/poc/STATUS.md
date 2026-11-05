# COAL POC status

Updated 2026-11-04. Branch `poc/go`. Data dir for all testing: `.sandbox/coal` (excluded from git).

## Done

- Read the review, repo docs, issues #1 to #9 and #11 to #15, PR #10, and the Claude Code hooks reference.
- Installed Go 1.27.3 under `~/.local/go`.
- Wrote `docs/poc/PLAN.md`.

## In progress

- M0 toolchain and scaffold.

## Next

- M1 schema and types, M2 store, M3 index, M4 daemon, M5 MCP, M6 capture, M7 web UI, M8 demo.

## Blocked

- Nothing.

## Notes for a restart

- Go is at `~/.local/go/bin`; the Makefile adds it to PATH.
- The web UI uses Svelte 5 with `@davenicoll/web-framework` from the sibling checkout `../web-framework`, installed with Bun (Dave, 2026-11-04). pnpm is not needed.
- A live Claude Code run would use Dave's account, so it stays optional and waits for his go-ahead. Recorded hook payloads under `testdata/claude-code/` are the test path.
