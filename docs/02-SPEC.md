# MVP Functional Spec

Version 0.1. Scope is deliberately small. See `CLAUDE.md` for the definition of done. Since 2026-11-07 this spec describes the Go implementation on `main` (ADR-018; it began as Dave Nicoll's proof of concept on `poc/go`). The text below says what the binary does.

## Users

One user: a person who uses more than one AI tool to build things and loses track of what was built where. The MVP is single-user, single-machine.

## Jobs to be done

1. **Capture.** When an AI session produces something, record what it was for and what it produced, without the user doing anything.
2. **Find.** Ask any AI tool (or the CLI) for a thing by what it was for, not by where it was made, and get it back.
3. **Resume.** Open the artifact, and see the state: what was done, what was left open.

## Objects

### Intent Manifest

The unit of record. One manifest per unit of intent (a project, a task, a thing you were building). Full schema in `docs/04-INTENT-MANIFEST.md`. Summary:

| Field | Purpose |
|---|---|
| `id` | UUIDv7 |
| `rev` | Write revision, incremented on every write; writers pass it back for compare-and-swap |
| `intent.summary` | One line, in the user's words (see ADR-009) |
| `intent.source` | `user` or `model`: who wrote the summary. Defaults to `model` when absent |
| `intent.goal` | Longer description, optional |
| `intent.tags` | Free-form strings |
| `status` | `active`, `paused`, `done`, `abandoned` |
| `artifacts[]` | Things produced, each with type, title, uri, hash, mime, producer, `versions[]`, and `skipped` when the body was not copied |
| `state` | `lastActivityAt`, `summary`, `openQuestions[]`, `nextStep` |
| `provenance` | Which tool, which model, session reference |
| `contexts[]` | Where the intent is relevant: a `path` key (project root) and a `repo` key (normalized remote) when there is one |
| `links[]` | Relations to other manifests |
| `ext` | Namespaced extensions; capture writes `ext["io.coalmine.state"]` (`recordedBy`, `at`) when a tool records state on purpose |

### Artifact

A file or URI produced during a session. Registered artifacts are copied into content-addressed storage so the manifest survives the original being moved or the chat being deleted. An artifact with a `uri` is identified by (manifest, `uri`) and keeps its history in `versions[]`; registering the same path or uri again adds a version. An artifact without a `uri` is identified by its content hash. The top-level `hash`, `size`, `createdAt` and `producer` always describe the latest version. A body that looks like a secret, or is over 5 MiB, is recorded with `skipped` and no copy.

## Functional requirements

### F1. Storage

- F1.1 Manifests persist as one JSON file each in `~/.coal/manifests/`. The data directory is `~/.coal` unless `COAL_HOME` or `--home` says otherwise.
- F1.2 Artifact bodies persist in `~/.coal/artifacts/<sha256 hex>` (64 lowercase hex characters, no `sha256:` prefix in the file name).
- F1.3 A SQLite index (`index.db`) provides full-text search over `intent.summary`, `intent.goal`, `intent.tags`, `artifacts[].title`, `state.nextStep` and `state.summary` using FTS5 with the porter tokenizer.
- F1.4 `reindex` rebuilds `index.db` from the manifest files only.
- F1.5 Every write validates against the manifest schema. Invalid manifests are rejected with a readable error.
- F1.6 Data directory layout:

```
~/.coal/
  config.json                  markers, captureExtensions, summarizer
  manifests/<id>.json          one file per manifest, the truth
  manifests/.<id>.lock         per-manifest write lock
  artifacts/<sha256 hex>       content-addressed bodies, append only
  index.db                     derived, rebuilt by coal reindex
  sessions/<sessionRef>.json   capture scratch: first prompt, matched manifest, last reply
  queue/<eventId>.json         capture events waiting to be folded
  queue/.lock                  the single-writer lock a drain holds
  queue/failed/                events that would not parse or apply
  queue/summarize/             summarize jobs, with its own failed/
  tmp/                         copies made by coal open, summarizer work dirs
  trash/                       removed manifests, never deleted
  logs/                        daemon.log, mcp.log, coal.log (hook and store errors), metrics.jsonl
  .git/                        history of manifests/ (ADR-014); .gitignore keeps the rest out
  .git.lock, .index.lock, .config.lock, .history-pending
```

### F2. Daemon

- F2.1 HTTP server (Go `net/http`) bound to `127.0.0.1:4242`. `coal serve --port` changes the port; the host cannot be changed. A request whose `Host` is not `127.0.0.1` or `localhost` on that port gets 403, and so does a write whose `Origin` is not the daemon's own. Every response carries `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer` and `X-Frame-Options: SAMEORIGIN`; API responses are `Cache-Control: no-store`.
- F2.2 REST endpoints:
  - `GET /health`
  - `GET /api/manifests?q=&status=&limit=` search. Returns `{results: [...]}` in the index row shape (`id`, `summary`, `source`, `status`, `lastActivityAt`, `updatedAt`, `artifactCount`, `nextStep`, `snippet`, `matched`), not full manifests. With no `q` it lists manifests newest first.
  - `POST /api/manifests` create
  - `GET /api/manifests/:id` full manifest; `:id` is the id or a unique prefix of 8 or more characters
  - `PATCH /api/manifests/:id` partial update. `If-Match: "<rev>"` makes it compare-and-swap: a stale rev gets 412 with the current manifest as the body and nothing is written. A patch that changes state stamps `ext["io.coalmine.state"]` with `recordedBy: "coal-web"`.
  - `POST /api/manifests/:id/artifacts` register an artifact. JSON body: `{path}` (absolute path to a regular file) or `{content, title[, type, mime]}`.
  - `GET /api/artifacts/:hex` serve the body with its stored MIME type and `Content-Security-Policy: sandbox`; `:hex` is the sha256 as 64 lowercase hex characters
  - `POST /api/reindex` returns `{indexed, skipped}`
  - `GET /api/schema` the embedded Intent Manifest schema
- F2.3 Serves the web UI from `/`.
- F2.4 Logs to `~/.coal/logs/daemon.log`, rotating at 10 MB.
- F2.5 Every two seconds the daemon drains the capture queue, runs queued summarize jobs, and commits manifest history that hooks left pending (ADR-014).

### F3. CLI (`coal`)

One binary. `--home <dir>` on any command overrides `COAL_HOME` and `~/.coal`. Ids may be given as a unique prefix of 8 or more characters.

- `coal init` creates the data directory and config if they do not exist
- `coal serve [--port 4242]` runs the daemon and web UI in the foreground on `127.0.0.1`
- `coal add --intent "..." [--goal "..."] [--status s] [--tag t]... [--artifact path]...` records a manifest in your own words
- `coal find [query...] [--status s] [--limit n] [--json]` searches; with no query, lists manifests newest first. Prints a table: id, summary, status, last activity, artifact count, with a snippet line under each row. `--json` prints an array of rows in the API shape
- `coal show <id>` prints the manifest as JSON
- `coal open <id> [--artifact n]` opens a fresh copy of the artifact (made under `tmp/`) with the OS default handler; `n` counts from 1 in manifest order and defaults to 1
- `coal rename <id> "<summary>"` sets `intent.summary` in your own words and `intent.source` to `user`
- `coal rm <id>` moves a manifest to `trash/`; nothing is deleted
- `coal purge <hash> [--yes]` deletes an artifact body that holds a secret and marks every version that used it `skipped: "purged"`; the one exception to never deleting
- `coal reindex` rebuilds `index.db` from `manifests/`
- `coal validate <file>...` checks manifest files against the schema
- `coal log <id> [--patch]` shows a manifest's change history (ADR-014)
- `coal stats [--days n]` counts local usage events from `logs/metrics.jsonl`; nothing leaves the machine
- `coal mcp` runs the MCP server on stdio (used by tool configs)
- `coal hook <session-start|user-prompt-submit|post-tool-use|stop|session-end>` is what the installed Claude Code hooks run: it reads the hook JSON on stdin, queues the event, and never fails the session
- `coal queue drain [--retry-failed]` folds every queued capture event into manifests, then runs summarize jobs
- `coal adapter install|uninstall claude-code --settings <file>` adds or removes the five hooks in a Claude Code settings file (F5)
- `coal import claude-code --dir <projects-dir> [--accept] [--markers] [--json]` proposes one manifest per project from Claude Code transcripts already on disk (ADR-012)
- `coal resume [--tool claude-code] [--print]` finds the manifest for the current directory and launches the harness with a first prompt that asks it to open from the next step; `--print` prints the command instead
- `coal new --intent "..." | --switch <id>` creates a second manifest in a directory that already has one and makes it active (ADR-013)
- `coal version` prints the version
- `coal completion` and `coal help` are the shell completion and help built into the CLI

### F4. MCP server

Tools exposed:

| Tool | Input | Output |
|---|---|---|
| `coal_find` | `query`; optional `status`, `limit` (default 20, at most 200) | `{results: [...]}`, each with `id`, `summary`, `source`, `status`, `lastActivityAt`, `artifactCount`, `nextStep`, `snippet`, `matched` |
| `coal_get` | `id` | Full manifest, including `rev` |
| `coal_read_artifact` | `id`; pick the artifact by `artifactIndex` (0-based, default 0) or `uri`; optional `version` (0-based index into `versions[]`, oldest first, default the latest) | `manifestId`, `artifactIndex`, `title`, `uri`, `mime`, `hash`, `size`, `version`, `versionCount`, then `text` (capped at 200,000 characters, with `truncated`) or a `file://` `path` for binary content; a skipped version returns `skipped` and no content |
| `coal_record_intent` | `summary`, `summaryFromUser` (true only when `summary` is the user's own words); optional `goal`, `tags`, `cwd` (absolute, becomes a `path` context), `tool`, `model`, `sessionRef` | `{id, rev, source}` |
| `coal_register_artifact` | `manifestId`; `path` (absolute) or `content`; optional `title`, `type`, `mime`, `uri` (identity for inline content; the same uri again adds a version) | `{manifestId, artifactIndex, artifact, latestHash, versionCount}` |
| `coal_update_state` | `id`; optional `nextStep`, `openQuestions`, `summary`, `goal`, `status`, `rev` | Updated manifest. A `rev` that no longer matches returns a tool error with the current rev and state, and writes nothing. `intent.summary` cannot be changed here |

Resources exposed:

- `coal://manifests/active` lists active manifests, newest activity first, up to 50, each with `id`, `summary`, `nextStep`, `lastActivityAt` and `artifactCount` (so a model can see current work without searching)

### F5. Claude Code adapter

Implements the capture contract from ADR-011 (`sessionStart`, `prompt`, `fileWritten`, `turnEnd`, `sessionEnd`) against Claude Code hooks. `coal adapter install claude-code --settings <file>` installs five hooks into the given settings file: `SessionStart`, `UserPromptSubmit`, `PostToolUse` with matcher `Write|Edit|MultiEdit|NotebookEdit`, `Stop`, and `SessionEnd` with a 5 second timeout. `--settings` is required; COAL never edits `~/.claude/` on its own. Each hook command runs `coal hook <event>` with `COAL_HOME` baked in when it was set; install is idempotent and keeps the first `<file>.bak`.

Every hook writes one event file, `~/.coal/queue/<eventId>.json`, carrying the capture event (`id`, `at`, `tool`, `sessionRef`, `cwd`, `kind`, and per kind `prompt`, `path`, `lastAssistantMessage`, `transcriptPath`, `reason`, `source`, `model`, `projectsRoot`). Every hook except `SessionEnd` then drains the queue in process under `queue/.lock`; `SessionEnd` only queues, and its event is folded later by the daemon tick, the next `SessionStart`, or `coal queue drain`.

- `SessionStart`: drain the queue, which folds the previous session's end. Match a manifest for `cwd` (F5.1) and attach the session to it. If found: print a `systemMessage` banner (`COAL: <summary>. Next: <nextStep>. <n> open questions.`) and inject `additionalContext` with summary, nextStep, openQuestions, artifact list, and an instruction to open the first reply by offering to continue and to call `coal_update_state` when work finishes.
- `UserPromptSubmit`: keep the first prompt that passes the summary filter as the session's first prompt. It becomes `intent.summary` when the session creates a manifest, and replaces a `Work in <project>` placeholder on the manifest it attaches to.
- `PostToolUse` on `Write`, `Edit`, `MultiEdit` or `NotebookEdit` for files whose extension is in `config.captureExtensions` (default `.html`, `.md`, `.py`, `.ts`, `.tsx`), inside the project root (git top level, else `cwd`), outside `.git` and the data directory, and at most 5 MiB: attach to the matched active manifest; if none, create one lazily with `intent.summary` = the session's first prompt (trimmed to 200 chars), `intent.source` = `user` (or `Work in <project>` with source `model` when no prompt passed the filter), and `contexts[]` = `{kind: path, key: <git top level, else cwd>}` plus `{kind: repo, key: <normalized origin URL>}` when there is a remote. Write the `.coal` marker (ADR-013) unless `markers: none`. Register the file as an artifact; the same path again adds a version. Nothing is captured when the project root would be the home directory, above it, or `/`.
- `Stop`: bump `state.lastActivityAt` and keep the last assistant message in the session scratch for extraction.
- `SessionEnd`: queue the event and return within the budget. No summary work here.
- Fold at session end (the queue worker, run by the daemon tick, the next `SessionStart` or `coal queue drain`): if the manifest's `ext["io.coalmine.state"]` stamp shows that `coal_update_state` or a PATCH recorded state since the session started, nothing more. A stamp from an earlier session does not count, so today a session that only talks replaces the earlier model-written summary with extraction; that is a known bug (#20), not the intent. Else deterministic extraction runs at fold time: `state.summary` and `state.nextStep` come from the last assistant message (first paragraph, and the last line starting `Next step:` or `Next:`) and, when the transcript is a Claude Code session file, from the transcript (last assistant message, else the last user prompt that passes the summary filter, followed by `Files touched: ...` with at most five names). A session that wrote nothing and got no reply leaves state alone. When `config.summarizer` is set, a summarize job is queued in `queue/summarize/` after the extraction; only the daemon or `coal queue drain` runs it later, never a hook, and its result replaces the extraction under a `coal-summarizer` stamp (ADR-016). The event file is deleted once folded; an event that fails to parse or apply moves to `queue/failed/`, and a failed summarize job to `queue/summarize/failed/`.

F5.1 Matching. A `.coal` marker in `cwd` or any parent up to the git top level wins. Otherwise match `contexts[]`: a `path` context matches when the resolved `cwd` equals `key` or starts with `key` followed by the path separator; a `repo` context matches when the cwd's normalized remote equals the key. Prefer the longest matching key. Only active or paused manifests match, a path key at or above the home directory never matches, and ties go to the most recently active manifest. Tests cover: subdirectory, sibling with shared prefix (`budget` vs `budget-old`), home directory, two clones of one remote.

### F6. Web UI

- Single page at `/`.
- Search box, results list, manifest detail pane.
- HTML artifacts render in a sandboxed iframe. Text artifacts render as code. Others show a download link.
- No auth (local only).
- The UI is Svelte 5, built with Bun under `web/` and embedded in the binary; it currently depends on `@davenicoll/web-framework`, which is not published anywhere, so a fresh clone serves a "web UI is not built" page until that dependency is vendored or published (#26).

## Non-functional requirements

- Search returns in under 100 ms for 10,000 manifests.
- Daemon idle memory under 100 MB.
- All data readable without the software: a user can `cat` a manifest and `open` an artifact.
- No network calls except those the user configures, such as `config.summarizer`.

## Out of scope for MVP

- Browser capture from claude.ai or chatgpt.com (phase 2)
- Vector / semantic search (phase 2; FTS5 is enough to prove the wedge)
- Interrupt gating, Slack, calendar, IDE presence (phase 3)
- Sync across machines, encryption at rest, accounts (phase 4)
- Teams and org features
