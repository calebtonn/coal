# COAL proof of concept: plan

Status: in progress. Branch `poc/go`, local only. Started 2026-11-04.

This plan follows the build recommendation in the spec review of commit `4f6da29` (section 6, first choice: a Go core with the web UI embedded). Dave chose that stack, so for this POC it replaces the "Stack (fixed for MVP)" section of `CLAUDE.md`. Every other rule in `CLAUDE.md` still holds: 127.0.0.1 only, never delete user data, no telemetry or network calls, UUIDv7 ids, ISO 8601 UTC, plain prose.

Progress lives in `STATUS.md` next to this file.

## Goal

One working end-to-end demo, all local and sandboxed:

1. A Claude Code session is captured by hooks. A manifest lands in the COAL data dir with the intent in the user's words, the written files as artifacts, and a next step.
2. `coal find budget` returns it on the command line.
3. An MCP client calls `coal_find` and gets the same manifest, then reads the artifact.
4. A new session in the same directory shows "You left off at ..." before the user types, and the model's first reply is told to offer to continue.
5. The web UI at `http://127.0.0.1:4242` shows the manifest with a preview of the HTML.

## Stack

| Part | Choice |
|---|---|
| Language | Go 1.27 (installed under `~/.local/go`), one module at the repo root, one binary `coal` |
| CLI | `github.com/spf13/cobra` |
| Daemon | `net/http`, bound to `127.0.0.1:4242` |
| MCP | `github.com/modelcontextprotocol/go-sdk`, stdio transport |
| Index | `modernc.org/sqlite` (pure Go), FTS5 with the porter tokenizer, rebuildable from `manifests/` |
| Schema | Hand-written JSON Schema 2020-12 in `spec/intent-manifest.schema.json`, validated with `github.com/santhosh-tekuri/jsonschema/v6` in Go and Ajv in the UI |
| Storage | JSON files under the data dir, which is a local git repo committed after each write (ADR-014) |
| Web UI | Vite + React + TypeScript under `web/`, built into the binary with `go:embed` |
| Tests | `go test`, Vitest for the UI |

The TypeScript workspace files from the original scaffold (`package.json`, `pnpm-workspace.yaml`, `packages/`) stay untouched. The POC lives beside them.

## Layout

```
go.mod                         module github.com/calebtonn/coal
cmd/coal/                      cobra commands, one file per command group
spec/                          schema (unchanged location) plus embed.go so Go can embed it
internal/manifest/             Go types, UUIDv7, timestamps, schema validation
internal/store/                JSON files, locks, rev compare-and-swap, artifacts, trash, git commits
internal/index/                SQLite FTS5 index and search
internal/queue/                event files and the single writer that folds them into manifests
internal/capture/              the capture contract (ADR-011) and the Claude Code translation
internal/marker/               the .coal marker and session-to-manifest matching
internal/daemon/               HTTP API and static UI
internal/mcpserver/            MCP tools and resource
web/                           Vite + React UI; web/embed.go embeds web/dist
docs/poc/                      PLAN.md, STATUS.md, DEMO.md
```

## Data directory

Set by `COAL_HOME`, default `~/.coal`. During the POC it is always `.sandbox/coal` inside the repo (excluded from git).

```
$COAL_HOME/
  config.json
  manifests/<id>.json          one file per Intent Manifest, the truth
  artifacts/<sha256hex>        content-addressed bodies, append only
  sessions/<sessionRef>.json   writer scratch: first prompt, matched manifest, cwd
  queue/<uuidv7>.json          events dropped by hooks, deleted once folded
  index.db                     derived, rebuildable with coal reindex
  trash/                       removed manifests go here, never deleted
  logs/
  .git/                        local history of manifests/ and sessions/; artifacts and index.db ignored
```

## Design decisions fixed for the POC

**Schema changes.** v0.1 is unpublished, so these change the schema without a version bump:

- `rev` (integer, optional, starts at 1) on the manifest. Every write increments it. Writers can pass the `rev` they read and get a conflict if it moved (#6).
- Artifact identity is `(manifest, uri)` when `uri` is present (#5). Each artifact carries `versions[]` of `{hash, size, createdAt, producer}`, newest last. The top-level `hash`, `size`, `createdAt` and `producer` always mirror the latest version so simple readers still work. Artifacts without a `uri` keep hash identity.
- `sessionRef` carries a `not: {pattern: ...}` for common token shapes so the rule travels with the schema (#7).
- `intent.source` gets `"default": "model"` (#9).
- `artifacts[].skipped` (string, optional) records why a body was not copied, for the secret deny-list (#7).

**Single writer.** Hooks never touch manifests. A hook writes one event file to `queue/` and exits. A fold step takes `queue/.lock` with `flock`, reads events in id order (UUIDv7 sorts by time), applies each one, deletes the event file, and commits. Fold runs in three places: inside any hook that gets the lock without waiting, inside `coal queue drain`, and on a timer in the daemon. A `SessionStart` hook drains before matching so the previous session's end is folded first (#1). Event files are internal scratch, not user data, so deleting them after a fold is allowed.

**Events** (the capture contract from ADR-011). One JSON object per file:

```
{ "id": uuidv7, "at": ts, "tool": "claude-code", "sessionRef": "claude-code:session:<id>",
  "cwd": "...", "kind": "sessionStart" | "prompt" | "fileWritten" | "turnEnd" | "sessionEnd",
  "model"?: "...", "prompt"?: "...", "path"?: "...", "lastAssistantMessage"?: "...",
  "transcriptPath"?: "...", "reason"?: "..." }
```

**Folding rules.**

- `sessionStart`: record the session in `sessions/`, with the matched manifest id if any.
- `prompt`: if the session has no first prompt yet and the text passes the summary filter (#11: not a slash command, at least four words, not mostly code, not on the stop list), store it as the session's first prompt.
- `fileWritten`: match or lazily create the manifest. Creation uses the session's first prompt as `intent.summary` with `source: user`, or a placeholder from the file name with `source: model` if no prompt passed. Write `contexts[]` (`path` at the git top level or cwd, `repo` with the normalized remote when there is one) and the `.coal` marker. Register the file as an artifact version. Skip paths on the deny-list (`.env*`, `*.pem`, `*.key`, `id_rsa*`, `credentials*`, `*.p12`) and bodies that match token shapes (`sk-`, `xox`, `ghp_`, `AKIA`, `-----BEGIN`); record them with `skipped`.
- `turnEnd`: bump `state.lastActivityAt`. Keep the last assistant message in the session scratch.
- `sessionEnd`: if the model recorded nothing through `coal_update_state`, set `state.summary` from the last assistant message (first paragraph, trimmed to 300 chars). Set `state.nextStep` only when a line in that message starts with "Next step" or "Next:" (#2, path 3 primary and path 4 fallback). Never overwrite `intent.summary`.

**Matching** (#4, ADR-013). In order: a `.coal` marker at or above cwd (nearest wins); a `path` context where `realpath(cwd)` equals the key or starts with `key + "/"` (longest key wins); a `repo` context whose key equals cwd's normalized origin remote. Only `active` and `paused` manifests match. Tests cover subdirectory, sibling with a shared prefix, home directory and two clones of one remote.

**Marker.** `.coal` at the git top level (or cwd without a repo) holds `{"$schema": "https://coalmine.io/schema/coal-marker/0.1.json", "manifest": "<id>"}`. COAL adds `.coal` to that repo's `.git/info/exclude` when it creates the marker. `coal new` is out of scope for the POC.

**Resume output** (#3). The `SessionStart` hook prints JSON with `systemMessage` (`COAL: You left off at "Add CSV export". 1 open question. Manifest 019a...`) and `hookSpecificOutput.additionalContext` carrying the summary, next step, open questions, artifact list and one instruction: open the first reply by offering to continue from the next step, and record progress with `coal_update_state` when work finishes.

**Hooks installed by `coal adapter install claude-code --settings <file>`.** The flag is required in the POC so nothing can touch `~/.claude/`. Each hook calls `coal hook <event>` and reads stdin:

| Event | Matcher | Does |
|---|---|---|
| `SessionStart` | all | drain queue, match, print resume JSON, write `sessionStart` event |
| `UserPromptSubmit` | all | write `prompt` event |
| `PostToolUse` | `Write\|Edit\|MultiEdit\|NotebookEdit` | write `fileWritten` event, try a non-blocking drain |
| `Stop` | all | write `turnEnd` event with `last_assistant_message`, try a non-blocking drain |
| `SessionEnd` | all | write `sessionEnd` event, try a non-blocking drain, `timeout: 5` |

**Search** (#15). FTS5 over summary, goal, tags, latest artifact titles, nextStep and state summary, with `tokenize='porter unicode61'`. Prefix matching on the last term. Results carry a snippet and the matched columns. Ties rank `intent.source = user` first. The `coal_find` description tells the model to try two or three phrasings and to read `coal://manifests/active` first.

**History.** The store runs `git init` once and `git add -A && git commit` after each fold or CLI write, holding the queue lock. `.gitignore` inside the data dir excludes `artifacts/`, `index.db`, `queue/` and `logs/`. If git is not on PATH the store logs once and carries on.

## Milestones

Each milestone is one builder task, under 1000 changed lines excluding generated code and lockfiles, with tests, committed on `poc/go`.

### M0. Toolchain and scaffold

- Go installed under `~/.local/go`; `Makefile` with `build`, `test`, `web`, `dev` targets that put it on PATH
- `go.mod`, `cmd/coal/main.go`, `coal version`, `coal init`, `COAL_HOME` handling, `.sandbox/` excluded
- Accept: `make build && .sandbox/bin/coal version` prints a version; `make test` exits 0
- Demo: `COAL_HOME=$PWD/.sandbox/coal .sandbox/bin/coal init && ls .sandbox/coal`

### M1. Schema and manifest types

- Schema changes listed above, applied by hand to `spec/intent-manifest.schema.json`; `spec/examples/budget-tracker.manifest.json` updated to carry `rev` and `versions[]`
- `internal/manifest`: types, `NewID()` (UUIDv7, lowercase), `Now()` (RFC 3339 UTC, second precision), `Validate([]byte) error` with readable errors
- Tests: the example validates; a manifest with `sessionRef: "Bearer x"` fails; a non-v7 id fails; unknown fields pass
- Accept: `go test ./internal/manifest/...` passes
- Demo: `coal validate spec/examples/budget-tracker.manifest.json`

### M2. Store

- `internal/store`: `Open(dir)`, `Create`, `Get`, `List`, `Update(id, expectedRev, fn)`, `Remove` (to `trash/`), `RegisterArtifact(manifestID, path|content, title, type, mime, producer)` with `(manifest, uri)` identity and `versions[]`, atomic writes, per-manifest lock files, deny-list and token scan, git commit after each write
- Tests: round trip; `Update` with a stale rev returns `ErrConflict`; 50 parallel `RegisterArtifact` calls on one manifest end with 50 artifacts; registering the same uri twice yields one artifact with two versions; a `.env` file is recorded with `skipped` and no body; `git log` in the data dir has one commit per write
- Accept: `go test ./internal/store/...` passes
- Demo: `coal add --intent "Household budget tracker as a single HTML page" --artifact README.md && coal show <id>`

### M3. Index and find

- `internal/index`: `Open`, `Upsert`, `Delete`, `Rebuild(store)`, `Search(query, status, limit)` returning id, rank, snippet, matched columns
- `coal find`, `coal reindex`, `coal show`, `coal open`
- Tests: `budgets` matches "budget tracker"; deleting `index.db` and rebuilding returns identical results; 10,000 synthetic manifests search in under 100 ms (skipped under `-short`)
- Accept: `go test ./internal/index/...` passes
- Demo: `coal find budget`, then `rm .sandbox/coal/index.db && coal reindex && coal find budget`

### M4. Daemon

- `coal serve`: `GET /health`, `GET /api/manifests?q=&status=&limit=`, `GET /api/manifests/{id}`, `POST /api/manifests`, `PATCH /api/manifests/{id}` with `If-Match` for `rev`, `POST /api/manifests/{id}/artifacts`, `GET /api/artifacts/{hash}` with stored MIME and `Content-Security-Policy: sandbox`, `POST /api/reindex`, `GET /api/schema`, `/` serves the embedded UI (a placeholder page until M7)
- Binds `127.0.0.1` only; a test asserts the listener address
- Drains the queue every two seconds
- Logs to `logs/daemon.log`
- Accept: `go test ./internal/daemon/...` passes with `httptest`
- Demo: `coal serve & curl -s 127.0.0.1:4242/health`

### M5. MCP server

- `coal mcp`: tools `coal_find`, `coal_get`, `coal_read_artifact` (latest version by default, optional `version`), `coal_record_intent`, `coal_register_artifact`, `coal_update_state` (optional `rev`, returns a conflict the model can act on); resource `coal://manifests/active`
- Descriptions written for a model reader, including the retry guidance from #15
- Tests: an in-process client over the SDK's in-memory transport lists tools, calls `coal_find`, reads the artifact, and sees a conflict on a stale `rev`
- Accept: `go test ./internal/mcpserver/...` passes
- Demo: `scripts/mcp-smoke.sh` pipes a JSON-RPC initialize, tools/list and `coal_find` call into `coal mcp` over stdio and prints the result

### M6. Capture

- `internal/queue`, `internal/capture`, `internal/marker`; `coal hook <event>`, `coal queue drain`, `coal adapter install|uninstall claude-code --settings <file>`
- Recorded hook payloads for a full session under `testdata/claude-code/` (SessionStart, UserPromptSubmit, PostToolUse Write, Stop, SessionEnd), with payload shapes from the Claude Code hooks reference as of 2026-11-04
- Tests: the recorded sequence yields one manifest with `source: user`, one artifact with one version, a `.coal` marker, contexts, and `state.summary`; editing the same file twice yields two versions; a second session in a subdirectory attaches to the same manifest; the SessionStart output carries `systemMessage` and `additionalContext`; every hook finishes in under 200 ms on the recorded payloads
- Accept: `go test ./internal/queue/... ./internal/capture/... ./internal/marker/...` passes
- Demo: `scripts/replay-session.sh` runs the recorded payloads through `coal hook`, then `coal find budget`, then `coal hook session-start` prints the resume line

### M7. Web UI

- `web/`: search box, results list (summary, status, relative time, artifact count), detail pane with artifact tabs, HTML in a sandboxed iframe, code in `<pre>`, a validity badge from Ajv against `/api/schema`
- `/` focuses search, arrow keys move selection, Enter opens
- Embedded with `go:embed`; `web/dist` is ignored by git except a `.gitkeep`; the daemon serves a plain "run make web" page when the build is missing
- Tests: Vitest for the search and detail components; a Go test confirms `/` serves the built `index.html` when present
- Accept: `make web && make build` then `coal serve` shows the manifest with the HTML preview
- Demo: `make dev` then open `http://127.0.0.1:4242`

### M8. End-to-end demo and docs

- `docs/poc/DEMO.md`: exact commands for the five demo steps using the sandbox
- `scripts/demo.sh`: fresh sandbox, replay, find, MCP smoke, resume line, daemon health, all in one run
- A live Claude Code run against a sandboxed project is documented as an optional step for Dave, because it uses his account
- Accept: `scripts/demo.sh` passes twice in a row from a fresh sandbox
- Demo: `scripts/demo.sh`

### Stretch, only after M8

- `coal import claude-code <projects-dir>` from transcript fixtures (ADR-012)
- `coal rename <id> "..."` (#11) and `coal log <id>` (ADR-014)
- `coal purge <hash>` as the documented exception to never-delete (#7)

## Working rules

- Builders run on Claude Opus 5.5 and get one milestone each with this plan and the relevant docs as the brief. Design, review and merges stay with the lead on Fable 5.1.
- At most two builders at once; test suites run one at a time because this laptop also runs other CI.
- All data under `.sandbox/`. Hooks are tested with recorded JSON and settings files inside the sandbox. Nothing writes to `~/.claude/` or `~/.coal/`.
- No pushes, PRs, issue comments or workflow files. Commits go to `poc/go` only.
- Stop processes by PID. No `rm -rf` outside `.sandbox/`, `web/dist` and `web/node_modules`.
