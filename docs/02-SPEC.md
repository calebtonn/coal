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
- `coal open <id> [--artifact n]` opens the artifact with the OS default handler. ADR-017 (Proposed) gives the no-flag form place behaviour; until it is accepted, `coal open <id>` opens the first artifact.
- `coal reindex`
- `coal mcp` runs the MCP server on stdio (used by tool configs)
- `coal resume [--tool claude-code]` finds the manifest for the current directory and launches the harness with a first prompt that asks it to open from the next step
- `coal new` creates a second manifest in a directory that already has one and makes it active (ADR-013)

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

Implements the capture contract from ADR-011 (`sessionStart`, `fileWritten`, `turnEnd`, `sessionEnd`) against Claude Code hooks. Installs into `~/.claude/settings.json` with `coal adapter install claude-code`.

- `SessionStart`: match a manifest for `cwd` (F5.1). If found: print a `systemMessage` banner (`COAL: <summary>. Next: <nextStep>. <n> open questions.`) and inject `additionalContext` with summary, nextStep, openQuestions, artifact list, and an instruction to open the first reply by offering to continue. Then drain `~/.coal/queue/`.
- `PostToolUse` on `Write` / `Edit` for `*.html`, `*.md`, `*.py`, `*.ts`, `*.tsx`: attach to the matched active manifest; if none, create one lazily with `intent.summary` = the session's first user message (trimmed to 200 chars), `intent.source` = `user`, and `contexts[]` = `{kind: path, key: <git top level, else cwd>}` plus `{kind: repo, key: <normalized origin URL>}` when there is a remote. Write the `.coal` marker (ADR-013) unless `markers: none`. Register the file as an artifact.
- `Stop`: bump `state.lastActivityAt` only.
- `SessionEnd`: write `~/.coal/queue/<sessionId>.json` (session id, transcript path, cwd) and return within the budget. No summary work here.
- Queue worker (run by the daemon or the next `SessionStart`): for each job, if the model recorded state through `coal_update_state`, nothing more; else extract `state.summary` deterministically from the transcript; if `config.summarizer` is set, run it instead. Then delete the job.

F5.1 Matching. A `.coal` marker in `cwd` or any parent up to the git top level wins. Otherwise match `contexts[]`: a `path` context matches when `realpath(cwd) === key` or starts with `key + sep`; a `repo` context matches when the cwd's normalized remote equals the key. Prefer the longest matching key. Tests cover: subdirectory, sibling with shared prefix (`budget` vs `budget-old`), home directory, two clones of one remote.

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
