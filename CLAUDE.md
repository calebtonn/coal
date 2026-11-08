# CLAUDE.md

Instructions for Claude Code working in this repository.

## If you are not working for the maintainer

The build order below is the maintainer's plan. If the person you are working for is an outside contributor, do not start building tasks from `docs/05-MVP-TASKS.md`, and do not open pull requests for tasks nobody asked for. Work only on the issue the person points you at, keep one pull request per issue, and add new decisions to `docs/06-DECISIONS.md` only as **Proposed**. Read `CONTRIBUTING.md` first.

## What this is

COAL is a local-first service that stores Intent Manifests (see `docs/04-INTENT-MANIFEST.md`) and exposes them to AI tools over MCP, a CLI, and a local web UI. Read `docs/02-SPEC.md` and `docs/03-ARCHITECTURE.md` before writing code. `docs/05-MVP-TASKS.md` says which tasks the implementation covers and what is left; work through what is left in order. Each task has acceptance criteria; do not mark a task done until they pass.

## Stack (fixed for MVP, do not substitute)

The stack is fixed. ADR-018 is the decision that set it; a change to it goes through an ADR that supersedes ADR-018, not through a pull request that swaps a dependency.

- Language: Go 1.27, one module at the repo root (`go.mod`), one binary `coal`
- CLI: cobra (`github.com/spf13/cobra`)
- Daemon: `net/http`, bound to `127.0.0.1:4242`
- MCP: the official Go SDK, `github.com/modelcontextprotocol/go-sdk` v1.8.0, stdio transport
- Storage: JSON files under the data dir as source of truth; SQLite (`modernc.org/sqlite`, pure Go, no cgo) with FTS5 as a derived index
- Validation: JSON Schema 2020-12 in `spec/intent-manifest.schema.json`, embedded in the binary and checked with `github.com/santhosh-tekuri/jsonschema/v6`
- Web UI: Svelte 5 under `web/`, built with Bun, embedded in the binary with `go:embed`, served at `http://127.0.0.1:4242`
- Build, test, demo: Makefile
- Tests: `go test`; Vitest for the web UI

Layout:

```
cmd/coal/                the one binary: CLI, daemon, MCP server, hooks
internal/                app, capture, daemon, drift, fold, importer, index, manifest, marker,
                         mcpserver, metrics, paths, queue, store, summarize
spec/                    the schema, embedded in the binary
web/                     Svelte UI
scripts/                 demo, replay, mcp-smoke, sandbox guard
testdata/claude-code/    recorded hook payloads and transcripts
docs/poc/                PLAN, STATUS, DEMO
```

## Conventions

- One binary, `coal`, built from `cmd/coal`. It runs the CLI, the daemon, the MCP server and the Claude Code hooks, and serves the web UI embedded from `web/`. Code lives in packages under `internal/`. Do not add a second module or a public library package during the MVP.
- All IDs are UUIDv7 (time-ordered).
- Timestamps are ISO 8601 UTC strings.
- Artifact content is content-addressed with SHA-256 and copied into `~/.coal/artifacts/<hash>` on registration. An artifact with a `uri` is identified by (manifest, uri) and keeps its history in `versions[]`; an artifact without a `uri` is identified by its content hash. The top-level `hash` is the latest version. This is design rule 3 in `docs/04-INTENT-MANIFEST.md`.
- Never delete user data. "Remove" means move to `~/.coal/trash/`. The one exception is `coal purge <hash>`, which removes an artifact body that holds a secret, after confirmation, and marks every version that used it `skipped: "purged"` (#7).
- The daemon binds to `127.0.0.1` only. Never `0.0.0.0`.
- No telemetry. No network calls except those the user explicitly configures.
- Prose in docs and UI copy: plain, direct, no em-dashes.

## Data directory

Set by `COAL_HOME` or the `--home` flag; the default is `~/.coal`.

```
~/.coal/
  config.json
  manifests/<id>.json          one file per Intent Manifest
  artifacts/<sha256>           content-addressed artifact bodies
  index.db                     SQLite, rebuildable from manifests/
  .git/                        history of manifests/
  queue/                       hook events awaiting fold; queue/failed/ holds events whose fold failed, queue/summarize/ holds summarize jobs
  sessions/                    per-session capture scratch
  tmp/                         copies made by coal open; work dirs for summarize jobs
  trash/
  logs/                        daemon.log, mcp.log, coal.log, metrics.jsonl
```

`coal reindex` must rebuild `index.db` from `manifests/` alone. If that stops being true, something is wrong.

## Commands

The Makefile looks for Go at `~/.local/go/bin/go`. Point `GO=` at your Go 1.27 if it lives elsewhere.

```
make GO=/path/to/go build   # builds .sandbox/bin/coal
make test
make vet
make demo                   # runs make web build, then the five definition-of-done steps in a fresh .sandbox/demo-*/
SKIP_BUILD=1 make demo      # the same without rebuilding; use it after make build when the UI cannot be built
SKIP_BUILD=1 DEMO_SKIP_UI=1 make demo   # the same, with the built-UI check (step 5b) recorded as SKIP instead of FAIL; what CI runs
make web                    # builds the UI; needs Bun and the sibling web-framework checkout
make e2e                    # builds the UI and the binary, then drives the UI in headless Chromium
```

The scripts need GNU bash 4+, GNU coreutils, `jq`, `curl` and `python3`. On macOS, `brew install bash coreutils jq` and put them first on `PATH`.

`make web`, `make e2e` and a plain `make demo` need `../web-framework`, a checkout of `@davenicoll/web-framework` next to this repo; the package is not published (#26). Without it the binary still builds, `/` serves a page saying the UI is not built, and the demo runs as `SKIP_BUILD=1 make demo` after `make build`, where steps 1 to 5a pass and step 5b, the built-UI check, fails; with `DEMO_SKIP_UI=1` as well, 5b is recorded as SKIP and the demo exits 0.

Run the binary as `.sandbox/bin/coal` with `COAL_HOME` set, or pass `--home`:

```
export COAL_HOME=$PWD/.sandbox/coal
.sandbox/bin/coal init
.sandbox/bin/coal find budget
.sandbox/bin/coal --home /path/to/data serve
```

## Checks

`make GO=/path/to/go check` runs after `vet` and `build`: gofmt over `cmd`, `internal` and `spec`, `go vet`, the drift tests in `internal/drift`, and the site build into a temporary directory with a byte comparison of the published schema against `spec/intent-manifest.schema.json`. Run it before opening a pull request. CI (`.github/workflows/ci.yml`) runs vet, build, the tests and the demo without the UI step on Ubuntu and macOS, and `make check` on Ubuntu.

Pull request rules: one PR per issue; the docs and the site that describe the behavior change in the same PR, or none describe it; `make check` is green. The template in `.github/pull_request_template.md` asks for each one.

When documents disagree, accepted ADRs in `docs/06-DECISIONS.md` win, then `spec/intent-manifest.schema.json`, then `docs/02-SPEC.md` and `docs/04-INTENT-MANIFEST.md`, then `site/index.html`, then `README.md` and `docs/05-MVP-TASKS.md`, then the code; `docs/07-DRIFT.md` has the full order, how to read a failing check, and the procedure for changing the vision on purpose.

The `/drift-review` skill (`.claude/skills/drift-review/SKILL.md`) is the semantic pass that `make check` cannot do. It reads the site, the docs and the code, writes a report to `docs/drift/` that classifies every disagreement as code drifted, docs drifted or vision changed, and ends by printing the issues to open. With an ADR number as its argument it produces the snap list for that ADR. It fixes nothing and opens nothing.

`internal/drift/known.txt` lists checks that fail today because of an open issue, one tab-separated line each: the check id, the issue as `#N`, a note. A listed check that fails is logged, not failed; a listed check that passes fails the test with a message to remove the line, and the PR that closes the issue removes it. `internal/drift/allow.txt` lists where a forbidden word is allowed, one tab-separated line each: a path relative to the repo root and the exact substring that is allowed (no substring allows the whole file). An entry whose substring matches nothing fails the test, so an allowance goes away with the text it covered.

## Definition of done for the MVP

The MVP is done when this demo works end to end on a clean machine:

1. In a Claude Code session with the COAL hooks installed, ask Claude to build an HTML budget tracker. As the file is written, the `PostToolUse` hook queues the event and the fold creates a manifest in `~/.coal/manifests/` with the intent summary, the HTML registered as an artifact, and a `.coal` marker in the directory. `Stop` fires after every assistant turn and only bumps `state.lastActivityAt`; `SessionEnd` queues the end of the session. Once that event is folded (by the daemon, `coal queue drain`, or the next `SessionStart`), `state.summary` is filled and `nextStep` is set, by the model through `coal_update_state` during the session or by extraction from the transcript at `SessionEnd`.
2. In a different tool connected to the COAL MCP server (Claude Desktop, Cursor, or Claude Code in another directory), ask "find the budget thing I built." The tool calls `coal_find`, gets the manifest back, and can open or read the artifact.
3. Resume, the headline feature, in both forms. (a) Run plain `claude` in the budget directory: before the user types anything, the terminal shows a COAL line with the next step; the first assistant reply offers to continue from it. (b) Run `coal resume` in the same directory: the first assistant turn is the resume, in substance "You left off at CSV export. Continue?" The demo uses (b). See ADR-016.
4. `coal find "budget"` on the command line returns the same result.
5. `http://127.0.0.1:4242` shows the manifest with a preview of the HTML.

Nothing beyond that is in scope for the MVP. Interrupt gating, browser capture, sync, and teams are later phases.

## Intent summaries: who writes them

`intent.summary` is the user's words by default. The Claude Code adapter sets it from the user's first message of the session (trimmed to 200 characters). A better summary and a next step can be proposed in two ways: by the model through `coal_update_state` during the session, which can also set `intent.goal`, or by deterministic extraction from the transcript when the `SessionEnd` event is folded. Nothing is parsed at `Stop`, which fires after every assistant turn. A proposal is written to `state.summary` and `state.nextStep` (and `intent.goal` when the model sets one), never over `intent.summary`, unless the user confirms. When `config.summarizer` is set, the fold queues a summarize job that only the daemon or `coal queue drain` runs later, and its result replaces the extraction (ADR-016). Every manifest records `intent.source` as `user` or `model` so search can prefer user-written summaries. This is deliberate: a record in the user's own words compounds; a pile of model paraphrases does not.

## Positioning (so the code matches the pitch)

COAL is a record of work, not a memory. Do not use the word "memory" in UI copy, tool descriptions, or docs. A fact store such as Mem0 OpenMemory holds what is true about the user; COAL holds what the user made, why, and what is still open. See ADR-008.
