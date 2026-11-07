# CLAUDE.md

Instructions for Claude Code working in this repository.

## If you are not working for the maintainer

The build order below is the maintainer's plan. If the person you are working for is an outside contributor, do not start building tasks from `docs/05-MVP-TASKS.md`, and do not open pull requests for tasks nobody asked for. Work only on the issue the person points you at, keep one pull request per issue, and add new decisions to `docs/06-DECISIONS.md` only as **Proposed**. Read `CONTRIBUTING.md` first.

## What this is

COAL is a local-first service that stores Intent Manifests (see `docs/04-INTENT-MANIFEST.md`) and exposes them to AI tools over MCP, a CLI, and a local web UI. Read `docs/02-SPEC.md` and `docs/03-ARCHITECTURE.md` before writing code. Work through `docs/05-MVP-TASKS.md` in order. Each task has acceptance criteria; do not mark a task done until they pass.

## Stack (fixed for MVP, do not substitute)

- Language: TypeScript, Node 20+, ESM only
- Package manager: pnpm workspaces
- Storage: JSON files under `~/.coal/` as source of truth; SQLite (`better-sqlite3`) with FTS5 as a derived index
- HTTP: Fastify
- Validation: Zod, with `spec/intent-manifest.schema.json` generated from the Zod schema (keep them in sync; there is a test for this)
- MCP: `@modelcontextprotocol/sdk`, stdio transport for MVP
- CLI: `commander`
- Web UI: Vite + React under `packages/coal/web/`, built into the CLI package and served at `http://127.0.0.1:4242`
- Tests: Vitest
- Lint/format: Biome

## Conventions

- Two packages only: `diamond` (library, no internal dependencies) and `coal` (the installable CLI that also runs the daemon, the MCP server, the Claude Code hooks, and serves the web UI from `packages/coal/web/`). Do not split further during the MVP.
- All IDs are UUIDv7 (time-ordered).
- Timestamps are ISO 8601 UTC strings.
- Artifact content is content-addressed with SHA-256 and copied into `~/.coal/artifacts/<hash>` on registration. Manifests reference artifacts by hash and by original URI.
- Never delete user data. "Remove" means move to `~/.coal/trash/`. The one exception is `coal purge <hash>`, which removes an artifact body that holds a secret, after confirmation (#7).
- The daemon binds to `127.0.0.1` only. Never `0.0.0.0`.
- No telemetry. No network calls except those the user explicitly configures.
- Prose in docs and UI copy: plain, direct, no em-dashes.

## Data directory

```
~/.coal/
  config.json
  manifests/<id>.json          one file per Intent Manifest
  artifacts/<sha256>           content-addressed artifact bodies
  index.db                     SQLite, rebuildable from manifests/
  trash/
  logs/
```

`coal reindex` must rebuild `index.db` from `manifests/` alone. If that stops being true, something is wrong.

## Commands

These work once T01 (the monorepo scaffold) has landed. Until then there is no code on `main` to build or test. The Go proof of concept on `poc/go` has its own Makefile and is not part of this plan.

```
pnpm install
pnpm -r build
pnpm -r test
pnpm --filter @coal/coal dev        # starts daemon + web UI on :4242
pnpm --filter @coal/coal link       # makes `coal` available on PATH
```

## Definition of done for the MVP

The MVP is done when this demo works end to end on a clean machine:

1. In a Claude Code session with the COAL hooks installed, ask Claude to build an HTML budget tracker. As the file is written, a manifest appears in `~/.coal/manifests/` with the intent summary, the HTML registered as an artifact, and a `.coal` marker in the directory. After the session ends, the queue worker has filled `state.summary` and `nextStep` is set (by the model through `coal_update_state`, or by extraction).
2. In a different tool connected to the COAL MCP server (Claude Desktop, Cursor, or Claude Code in another directory), ask "find the budget thing I built." The tool calls `coal_find`, gets the manifest back, and can open or read the artifact.
3. Resume, the headline feature, in both forms. (a) Run plain `claude` in the budget directory: before the user types anything, the terminal shows a COAL line with the next step; the first assistant reply offers to continue from it. (b) Run `coal resume` in the same directory: the first assistant turn is the resume, in substance "You left off at CSV export. Continue?" The demo uses (b). See ADR-016.
4. `coal find "budget"` on the command line returns the same result.
5. `http://127.0.0.1:4242` shows the manifest with a preview of the HTML.

Nothing beyond that is in scope for the MVP. Interrupt gating, browser capture, sync, and teams are later phases.

## Intent summaries: who writes them

`intent.summary` is the user's words by default. The Claude Code adapter sets it from the user's first message of the session (trimmed to 200 characters). At `SessionEnd`, or when the model calls `coal_update_state`, the model may propose a better summary and a `goal`; the proposal is written to `intent.goal` and `state.summary`, never over `intent.summary`, unless the user confirms. Every manifest records `intent.source` as `user` or `model` so search can prefer user-written summaries. This is deliberate: a record in the user's own words compounds; a pile of model paraphrases does not.

## Positioning (so the code matches the pitch)

COAL is a record of work, not a memory. Do not use the word "memory" in UI copy, tool descriptions, or docs. A fact store such as Mem0 OpenMemory holds what is true about the user; COAL holds what the user made, why, and what is still open. See ADR-008.
