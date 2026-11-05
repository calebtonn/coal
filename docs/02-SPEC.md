# MVP Functional Spec

Version 0.1. Scope is deliberately small. See `CLAUDE.md` for the definition of done.

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
| `intent.summary` | One line, in the user's words (see ADR-009) |
| `intent.source` | `user` or `model`: who wrote the summary |
| `intent.goal` | Longer description, optional |
| `intent.tags` | Free-form strings |
| `status` | `active`, `paused`, `done`, `abandoned` |
| `artifacts[]` | Things produced, each with type, title, uri, hash, producer |
| `state` | `lastActivityAt`, `openQuestions[]`, `nextStep` |
| `provenance` | Which tool, which model, session reference |
| `links[]` | Relations to other manifests |

### Artifact

A file or URI produced during a session. Registered artifacts are copied into content-addressed storage so the manifest survives the original being moved or the chat being deleted.

## Functional requirements

### F1. Storage (diamond)

- F1.1 Manifests persist as one JSON file each in `~/.coal/manifests/`.
- F1.2 Artifact bodies persist in `~/.coal/artifacts/<sha256>`.
- F1.3 A SQLite index (`index.db`) provides full-text search over `intent.summary`, `intent.goal`, `intent.tags`, `artifacts[].title`, and `state.nextStep` using FTS5.
- F1.4 `reindex` rebuilds `index.db` from the manifest files only.
- F1.5 Every write validates against the manifest schema. Invalid manifests are rejected with a readable error.

### F2. Daemon

- F2.1 Fastify server bound to `127.0.0.1:4242`.
- F2.2 REST endpoints:
  - `GET /manifests?q=&status=&limit=` search
  - `GET /manifests/:id`
  - `POST /manifests` create
  - `PATCH /manifests/:id` partial update
  - `POST /manifests/:id/artifacts` register an artifact (multipart or `{uri}`)
  - `GET /artifacts/:hash` serve the body with its stored MIME type
  - `POST /reindex`
  - `GET /health`
- F2.3 Serves the web UI from `/`.
- F2.4 Logs to `~/.coal/logs/daemon.log`, rotating at 10 MB.

### F3. CLI (`coal`)

- `coal init` creates `~/.coal/` and config
- `coal serve` starts the daemon in the foreground
- `coal add --intent "..." [--artifact path]...` creates a manifest
- `coal find "<query>"` searches, prints a table: id, summary, status, last activity, artifact count
- `coal show <id>` prints the manifest
- `coal open <id> [--artifact n]` opens the artifact with the OS default handler
- `coal reindex`
- `coal mcp` runs the MCP server on stdio (used by tool configs)

### F4. MCP server

Tools exposed:

| Tool | Input | Output |
|---|---|---|
| `coal_find` | `query`, optional `status`, `limit` | List of manifest summaries with ids |
| `coal_get` | `id` | Full manifest |
| `coal_read_artifact` | `id`, `artifactIndex` | Artifact body as text (or a `file://` path for binaries) |
| `coal_record_intent` | `summary`, `goal?`, `tags?`, `provenance` | New manifest id |
| `coal_register_artifact` | `manifestId`, `path` or `content` + `title`, `type` | Artifact hash |
| `coal_update_state` | `id`, `nextStep?`, `openQuestions?`, `status?` | Updated manifest |

Resources exposed:

- `coal://manifests/active` lists active manifests (so a model can see current work without searching)

### F5. Claude Code adapter

The adapter translates Claude Code hook payloads into the capture contract from ADR-011: `sessionStart`, `prompt`, `fileWritten`, `turnEnd` and `sessionEnd`. Hooks never write manifests. Each hook drops one event file in `~/.coal/queue/` and exits. One writer, holding `queue/.lock`, folds events into manifests in order.

Hooks, installed into `~/.claude/settings.json` (or a project `.claude/settings.json`) with `coal adapter install claude-code`:

- `SessionStart`: fold any queued events first, so the previous session's end is applied. Then match the working directory to a manifest (see Matching). On a match, print a `systemMessage` the user sees before typing, for example `COAL: You left off at "Add CSV export". 1 open question.`, and `additionalContext` with the summary, `nextStep`, open questions, artifact list and an instruction to open the first reply by offering to continue.
- `UserPromptSubmit`: queue the prompt. The first prompt that passes the summary filter (#11) becomes `intent.summary` with `source: user`.
- `PostToolUse` on `Write`, `Edit`, `MultiEdit` and `NotebookEdit`: queue the written path. The writer attaches it to the matched manifest, or creates one lazily, and registers it as an artifact version (artifact identity is `(manifest, uri)`, see `docs/04-INTENT-MANIFEST.md`). Files on the secret deny-list, or with bodies that look like tokens, are recorded with `skipped` and no copy.
- `Stop`: runs after every turn, not at session end. Queue a cheap `turnEnd` with `last_assistant_message`; the writer bumps `state.lastActivityAt`.
- `SessionEnd`: queue `sessionEnd` and return at once, because the default timeout is 1.5 seconds. It also fires on `/clear`. Sessions that end without it are folded on the next `SessionStart`.

**Matching** (ADR-013). In order: a `.coal` marker at or above the working directory, nearest first; a `path` context where the resolved working directory equals the key or sits under `key + "/"`, longest key first; a `repo` context whose key equals the working directory's normalized origin remote. Only `active` and `paused` manifests match. On lazy creation the writer adds a `path` context (git top level, else the working directory), a `repo` context when there is a remote, and the `.coal` marker, which it lists in `.git/info/exclude`. A second session in the same project attaches to the same manifest.

**Summaries** (#2). No model call by default. The model is asked, through the `SessionStart` context and the `coal_update_state` description, to record `nextStep` when it finishes a piece of work. If it did not, `sessionEnd` falls back to extraction: `state.summary` from the last assistant message, and `nextStep` only from a line that starts with "Next step" or "Next:". A model summarizer over the transcript is an opt-in setting in `config.json`, which counts as a call the user configured. Model proposals go to `intent.goal` and `state.summary`, never over `intent.summary`.

### F6. Web UI

- Single page at `/`.
- Search box, results list, manifest detail pane.
- HTML artifacts render in a sandboxed iframe. Text artifacts render as code. Others show a download link.
- No auth (local only).

## Non-functional requirements

- Search returns in under 100 ms for 10,000 manifests.
- Daemon idle memory under 100 MB.
- All data readable without the software: a user can `cat` a manifest and `open` an artifact.
- No outbound network calls.

## Out of scope for MVP

- Browser capture from claude.ai or chatgpt.com (phase 2)
- Vector / semantic search (phase 2; FTS5 is enough to prove the wedge)
- Interrupt gating, Slack, calendar, IDE presence (phase 3)
- Sync across machines, encryption at rest, accounts (phase 4)
- Teams and org features
