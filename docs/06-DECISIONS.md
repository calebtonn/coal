# Architecture Decision Records

Short form. Add a new entry for any decision that changes the shape of the system.

## ADR-001: Local-first, files as source of truth

**Status.** Accepted.

**Context.** The thesis is user ownership of intent and artifacts. A hosted database contradicts that on day one.

**Decision.** `~/.coal/` holds plain JSON manifests and content-addressed artifact bodies. SQLite is a derived index only.

**Consequences.** No sync in MVP. Multi-device is a Phase 4 problem solved by syncing files (e.g. encrypted directory sync), not by a server owning the data.

**In the code.** `internal/paths` resolves the data directory. `internal/store` writes the manifests and the content-addressed artifact bodies as plain files. `internal/index` is the derived SQLite index, and `cmd/coal/find.go` holds coal reindex, the command that rebuilds it from the manifest files alone.

## ADR-002: MCP as the primary read/write interface for AI tools

**Status.** Accepted.

**Context.** Every major AI tool is adopting MCP. Building vendor-specific plugins for each would be a treadmill.

**Decision.** The MCP server is the canonical integration point. CLI and REST exist for humans and scripts, not for models.

**Consequences.** COAL works with any future MCP client without changes. Capture still needs per-tool adapters where lifecycle hooks are needed (Claude Code first).

**In the code.** `internal/mcpserver` is the MCP server and `cmd/coal/mcp.go` starts it over stdio. The REST API for humans and scripts is `internal/daemon/api.go`; the CLI is `cmd/coal`.

## ADR-003: Claude Code hooks for first capture, no browser capture in MVP

**Status.** Accepted. Amended by ADR-011: Claude Code is no longer the only tool with hooks; the MVP scope stands.

**Context.** Browser capture from claude.ai or chatgpt.com requires an extension and fights with page structure that changes without notice. Claude Code exposes deterministic hooks.

**Decision.** MVP captures only from Claude Code. Browser capture is Phase 2.

**Consequences.** The MVP proves capture, find, and resume for a developer audience first. The "HTML buried in a chat" case on claude.ai is served in MVP only by manual `coal add` from a downloaded artifact.

**In the code.** `cmd/coal/hook.go` receives the Claude Code hook events and `internal/capture/claudecode.go` translates them. `cmd/coal/adapter.go` installs the hooks. There is no browser capture; the manual path, coal add, is in `cmd/coal/manifests.go`.

## ADR-004: Full-text search before semantic search

**Status.** Accepted.

**Context.** Vector search needs an embedding model, which means either a local model or a network call.

**Decision.** FTS5 only in MVP. Intent summaries are short and written in the user's words; keyword match is usually enough.

**Consequences.** "Find the thing about money" may miss "budget tracker." Acceptable for MVP; revisit with a local embedding model in Phase 2.

**In the code.** `internal/index/index.go` creates the FTS5 table and `internal/index/search.go` builds the query and ranks with bm25. `cmd/coal/find.go` is the command line entry; the MCP tool is in `internal/mcpserver/tools.go`. There is no embedding model anywhere in `go.mod`.

## ADR-005: TypeScript monorepo

**Status.** Superseded by ADR-018 on 2026-11-07. The text below is kept as written.

**Context.** MCP SDK, Claude Code hook scripting, and the web UI are all most natural in TypeScript.

**Decision.** One language. pnpm workspaces with exactly two packages: `diamond` (library) and `coal` (the installable, which runs the daemon, MCP server and hooks as subcommands and serves the web UI). Six packages were the first draft; collapsed because a solo nights-and-weekends build should spend its hours on the demo, not on workspace plumbing.

**Consequences.** `better-sqlite3` requires a native build; document the prerequisites. A Rust or Go implementation could replace `diamond` later behind the same schema. Split packages only when a second implementer needs one piece alone.

## ADR-006: Open source under Apache 2.0, spec published separately

**Status.** Accepted 2026-11-04.

**Context.** Adoption depends on trust; trust depends on auditability. The schema matters more than the implementation.

**Decision.** Code and spec both Apache 2.0. The spec gets its own repo and versioning once there is a second implementer.

**Consequences.** Decided 2026-11-04: fully open source, no paid tier planned. Sync, org install and any hosted piece are open too. If that ever changes it needs a new ADR and a check against the author's employment terms.

**In the code.** `LICENSE` is the Apache 2.0 text and covers the whole repository, spec included. The spec still lives in this repo at `spec/intent-manifest.schema.json`; `site/build.sh` and `.github/workflows/pages.yml` publish it at its $id.

## ADR-007: The whiteboard pipeline is deferred

**Status.** Accepted.

**Context.** The original sketch routed signals through API Gateway, EventBridge, Lambda, Glue, S3, and SageMaker into Agent Core.

**Decision.** None of that is in the MVP. Signal collection, cognitive state inference, and the policy engine are Phase 3 and will run locally first.

**Consequences.** If Phase 3 ever needs hosted inference, it is a per-user opt-in that reads the local manifests, not a central pipeline that all signals flow through by default.

**In the code.** Nothing implements this decision; what keeps the absence true is `go.mod`, which has no cloud SDK among its direct requires, and the dependency check in `internal/drift`, which fails when a direct dependency outside the ADR-018 list is added.

## ADR-008: COAL is a record of work, not a memory

**Status.** Accepted.

**Context.** Mem0 OpenMemory and the platforms' built-in memory store facts, preferences, or session notes across tools. Positioning COAL as "memory" invites a direct comparison on their terms and loses.

**Decision.** COAL stores what the user made, why, and what is still open: intent, content-addressed artifacts with provenance, open loops, and contexts. It does not store facts about the user. It is designed to sit on top of a fact store rather than replace one; a future adapter may read user preferences from OpenMemory to improve summaries and surfacing.

**Consequences.** The word "memory" does not appear in UI copy, tool descriptions, or docs. The MCP tools are named around work (`coal_find`, `coal_record_intent`), not recall. The headline feature is resume: the next tool opens by offering to continue, driven by `contexts[]` and `state.nextStep`.

**In the code.** The rule is enforced by tests: `internal/mcpserver/server_test.go` checks every tool description and the server instructions, `internal/capture/capture_test.go` checks the hook output, and `internal/drift` checks the docs, the site and the web UI copy. The tool names live in `internal/mcpserver/tools.go`; the resume moment is built in `internal/capture/resume.go` and launched by `cmd/coal/resume.go`.

## ADR-009: The user writes the intent summary

**Status.** Accepted.

**Context.** A record of work is only as good as its index, and `intent.summary` is the index. If the model writes it, the record fills with paraphrases that sound alike. If the user writes it, each entry costs a sentence and compounds like a commonplace book.

**Decision.** `intent.summary` defaults to the user's own first message of the session. The model's proposals go to `intent.goal` and `state.summary`. `intent.source` records `user` or `model`. Search ranks user-written summaries first.

**Consequences.** Some summaries will be rough ("build me a thing for the budget"). That is acceptable; it is what the user will search for. A later `coal rename` lets the user tighten a summary by hand.

**In the code.** `internal/capture/summary.go` takes the user's first message and cuts it to 200 characters; `internal/fold/fold.go` writes it as intent.summary with intent.source set to user. The model's proposals arrive through coal_update_state in `internal/mcpserver/tools.go`, which writes state.summary, state.nextStep and intent.goal; extraction in `internal/capture/extract.go` writes state.summary and state.nextStep only. `internal/index/search.go` ranks user-written summaries first on ties. `cmd/coal/rename.go` is coal rename.

## ADR-010: Domains

**Status.** Accepted 2026-11-05.

**Context.** Two domains were registered on 2026-11-04: coalmine.io and coalmine.app.

**Decision.** coalmine.io is the standard: the published schema (`/schema/intent-manifest/<version>.json`), the spec, the docs and the project site. coalmine.app is reserved for anything that runs: the hosted MCP endpoint, sync, or an API, if and when any of those exist. Package names, the CLI and the data directory stay `coal`.

**Consequences.** The schema `$id` is `https://coalmine.io/schema/intent-manifest/0.1.json` and is served at that path. In Phase 1 nothing runs on coalmine.app; it redirects to coalmine.io. The one-page project site at coalmine.io is built from `site/` and deployed by `.github/workflows/pages.yml`, together with the schema at its `$id`. Nothing else is hosted before the Phase 1 retro.

**In the code.** The $id is in `spec/intent-manifest.schema.json`. `site/build.sh` copies the schema to the path the $id names and assembles `site/index.html` and `site/404.html` into the site; `.github/workflows/pages.yml` deploys it. The marker schema URL that `internal/marker/marker.go` writes is not published yet (#27).

## ADR-011: One capture contract, adapters for every coding agent with hooks

**Status.** Accepted 2026-11-05. Amends ADR-003.

**Context.** ADR-003 chose Claude Code because it was the only mainstream tool with lifecycle hooks. That is no longer true (#8): Cursor, Codex CLI and Gemini CLI all have session, tool and stop or end events, and all pass JSON on stdin. If the Claude Code adapter is written as a special case, each later tool means rework, and Phase 2's "Reach" stays expensive for no reason.

**Decision.** Phase 1 defines a small internal capture contract that adapters translate into: `sessionStart {tool, cwd, sessionRef}`, `fileWritten {path}`, `turnEnd`, `sessionEnd {transcriptPath?}`. The Claude Code adapter is its first implementation. The MVP still ships Claude Code only. Cursor and Codex adapters are the first Phase 2 items. Phase 2's hard problem is restated as chat apps (claude.ai, chatgpt.com), which have no hooks.

**Consequences.** A little extra work in T08 kept the contract separate from Claude Code's payload shapes (`internal/capture`: the contract in `event.go`, the Claude Code translation in `claudecode.go`). Phase 2's "done when" (a manifest written by something other than Claude Code, resumed from a third tool) becomes reachable with two small adapters. ADR-003's MVP scope stands; only its reasoning changes.

**In the code.** The capture contract is `internal/capture/event.go`; the Claude Code adapter is `internal/capture/claudecode.go`. `cmd/coal/hook.go` is the hook entry point and `cmd/coal/adapter.go` installs and removes adapters. Claude Code is the only adapter today.

## ADR-012: Import from session records already on disk

**Status.** Accepted 2026-11-05. First Phase 2 item.

**Context.** The case that started COAL is a page buried in a claude.ai chat, and the MVP only serves it through a manual `coal add`. Browser capture is the planned fix, and ADR-003 already notes it is fragile. Meanwhile every coding agent writes its sessions to disk (Claude Code under `~/.claude/projects/`, Codex under `~/.codex/sessions/`), and claude.ai and ChatGPT both offer account data exports. Live capture also starts from an empty record, so the week six retro measures a record with only six weeks in it.

**Decision.** Add `coal import`. It reads these sources, proposes manifests (grouped by working directory for coding agents, by conversation for chat exports), and shows them for the user to accept, merge or discard before anything is written. Artifacts in chat exports (code blocks, HTML) are registered as artifacts. Grouping chat conversations into intents uses a model only when the user has configured one, which keeps the no-network rule. Import is the first Phase 2 item, or goes into Phase 1 week 5 if hardening finishes early.

**Consequences.** The browser extension moves later and may not be needed. Transcript formats are undocumented and change, so each importer is versioned and skips what it cannot parse. Import is batch, so chat-side state lags until the next run. The record has history on day one, which makes the retro a fairer test. Since 2026-11-07 `coal import claude-code` is on `main` (ADR-018); its symlink bug is #23. Import from Codex sessions and from chat exports is still open.

**In the code.** `internal/importer/claudecode` reads Claude Code session records and proposes manifests; `cmd/coal/import.go` is coal import claude-code. There is no Codex or chat export importer yet.

## ADR-013: The project directory anchors the intent

**Status.** Accepted 2026-11-05, with the `markers` setting. Resolves #4. Marker shape and schema URL filled in on 2026-11-07 from the implementation (ADR-018).

**Context.** Sessions are not intents. One session can touch several intents, and one intent runs across many sessions and tools. Matching sessions to manifests is the hardest part of capture, and the current spec leaves it open (#4). For coding work there is already a boundary the user chose: the project directory. F5 mentions a `.coal` marker file but nothing defines it.

**Decision.** On the first capture in a directory, COAL writes a `.coal` file at the git top level (or cwd when there is no repo) holding `{"$schema": "https://coalmine.io/schema/coal-marker/0.1.json", "manifest": "<id>"}` and nothing else. The marker schema URL is what COAL writes; it is not yet published (the path returns 404), tracked as #27. Every later session from any tool in that directory or below attaches to that manifest. The manifest itself stays in `~/.coal/manifests/`, so there is still one source of truth (ADR-001). By default the marker is listed in `.git/info/exclude`; the user can choose to commit it. `coal new` in a directory with a marker creates a second manifest and makes it the active one; the marker then holds `{"manifest": "<active id>", "manifests": ["<id>", ...]}`, where `manifest` is the active one and `manifests` lists every manifest anchored in the directory. Optionally, `coal adapter install` adds one line to the project's `AGENTS.md` or `CLAUDE.md` telling agents to call `coal_get` with the id in `.coal` at the start of work, so agents with no COAL adapter can still resume.

**Consequences.** Identity for coding work is deterministic and needs no model. Chat work has no directory and still relies on grouping (ADR-012). COAL writes a file into user projects, which some people will not want, so `markers: "project" | "none"` is a setting, with `none` falling back to `contexts[]` path matching.

**In the code.** `internal/marker/marker.go` finds the project root, writes and reads the .coal file and lists it in the repo's exclude file; `internal/marker/match.go` matches a directory to a manifest. `internal/fold/fold.go` writes the marker on first capture and reads the markers setting from `internal/fold/config.go`. `cmd/coal/new.go` is coal new, which adds a second manifest to a marker. The AGENTS.md or CLAUDE.md line is not implemented.

## ADR-014: Keep history of manifest changes

**Status.** Accepted 2026-11-05. Status and decision reworded on 2026-11-07 to describe the implementation (ADR-018). Two constraints: a hook never runs git (SessionEnd has a 1.5 s budget), it only marks `.history-pending` through deferred history; the daemon tick commits `manifests/` in a batch under the store-wide `.git.lock` once that marker has been quiet for two seconds, and `coal queue drain` or any non-hook CLI command commits what is pending as soon as it opens the store. Uses the `git` on PATH; if none, skip silently. Landed in the proof of concept (M11). Noted on 2026-11-08: 1.5 s is the Claude Code default this decision was written against; the adapter installs the SessionEnd hook with a 5 second timeout (`cmd/coal/adapter.go`), and the hook only queues an event, so it fits either budget.

**Context.** Manifests are mutable. `nextStep`, `status` and `openQuestions` are overwritten in place, so there is no way to answer "what was next last week", to see when an intent was abandoned, or to recover from a bad write by a model.

**Decision.** `~/.coal/` is a local git repository with no remote by default. Writes are committed in batches, never inside a hook: a hook write touches `.history-pending` and runs no git; the daemon tick (after two quiet seconds) or any non-hook CLI command (on opening the store) then commits `manifests/` under the store-wide `.git.lock`, with a message naming the tool and operation for a direct write, or the manifests changed for a batch. `artifacts/` is excluded, since it is already content-addressed and append-only, and `index.db` is excluded because it is derived. `coal log <id>` shows a manifest's history. `reindex` still reads `manifests/` only.

Considered: an append-only event log per manifest with the manifest as a projection. More precise, but a second format for readers and implementers to understand. Not keeping history: the current spec.

**Consequences.** Git becomes a dependency: the `git` on PATH, and without one there is no history. History comes with ordinary tools: `git log`, `git diff`, `git revert`. Phase 4 sync can start from syncing a git repository. Lost updates are handled by `rev` (ADR-019, which closed #6); history only makes them visible. Only `manifests/` is committed; session scratch and open copies stay out of the history. Commits are not one per write: writers that finish close together can share a commit, so a commit message names the write that took the lock, not every change in it. Hooks open the store with deferred history: a write touches `.history-pending` and runs no git, the daemon commits once that marker has been quiet for two seconds, and any non-hook command commits what is pending when it opens the store.

**In the code.** `internal/store/git.go` runs git and keeps everything but the manifests out of the history; `internal/store/history.go` is the deferred history and the pending marker. The daemon tick that commits is in `internal/daemon/daemon.go`; coal queue drain is in `cmd/coal/hook.go`; `cmd/coal/log.go` is coal log.

## ADR-015: Use Agent Trace for line-level code provenance

**Status.** Accepted 2026-11-05.

**Context.** Each artifact carries a `producer` (tool, model, session). Agent Trace, published by Cursor in January 2026 under CC BY 4.0 and supported by Cognition, OpenCode and others, is an open, storage-agnostic spec that links code ranges to the conversations and contributors behind them. The README says the schema is the product. A second provenance format next to an emerging standard would compete with it rather than build on it.

**Decision.** `producer` stays minimal: tool, model, sessionRef. COAL does not add line-level attribution to the manifest. When an Agent Trace record exists for a code artifact, or an adapter can produce one, the artifact references it under `ext["dev.agent-trace"]` with the record's URI or hash. COAL owns intent and state; Agent Trace owns which lines came from where.

**Consequences.** Code provenance from COAL can be read by tools that adopt Agent Trace, at the cost of one extension key. Agent Trace is still a draft, so the reference stays under `ext` until v1.0 of both specs, when it can be reconsidered as a first-class field. Reference: https://agent-trace.dev/

**In the code.** `spec/intent-manifest.schema.json` defines producer and the artifact ext object; `internal/manifest/manifest.go` carries them through unchanged. `docs/04-INTENT-MANIFEST.md` documents the dev.agent-trace key. No adapter writes an Agent Trace record yet.

## ADR-016: Capture lifecycle and the resume moment

**Status.** Accepted 2026-11-05. Resolves #1, #2, #3. Consequences amended on 2026-11-07 for the order of extraction and the summarizer (ADR-018). Noted on 2026-11-08: 1.5 s is the Claude Code default this decision was written against; the adapter installs the SessionEnd hook with a 5 second timeout (`cmd/coal/adapter.go`), and the hook only queues an event, so it fits either budget.

**Context.** The first draft assumed `Stop` meant end of session, that a hook could ask the running model for a summary, and that a `SessionStart` hook could make the assistant speak before the user typed. None of those are true of Claude Code hooks: `Stop` fires after every assistant turn, `SessionEnd` is the per-session event with a 1.5 second budget, a command hook cannot query the session's model, and no assistant turn happens until the user sends one.

**Decision.**
- `PostToolUse` and `Stop` do cheap work only: register artifacts, bump `state.lastActivityAt`.
- `SessionEnd` writes a job file under `~/.coal/queue/` and returns. The daemon or the next `SessionStart` drains the queue. Sessions that never end cleanly are drained the same way.
- Summaries: the model records its own state through `coal_update_state`, prompted by `SessionStart` context and the tool description. If nothing was recorded, deterministic extraction from the transcript (last user message, last assistant message, files touched) fills `state.summary`. A headless model call over the transcript is an opt-in `config.json` setting, which counts as a call the user configured. `intent.summary` stays the user's first message (ADR-009).
- The resume moment has two forms. Plain `claude` in a matched directory: the `SessionStart` hook prints a `systemMessage` banner with the next step before the user types, and injects `additionalContext` so the first reply opens by offering to continue. `coal resume`: a launcher that finds the manifest for the directory and starts the harness with a first prompt already supplied, so the first turn is the resume. The demo uses `coal resume`.

**Consequences.** `coal resume` moves a small piece of the Phase 2 entry-point idea into Phase 1. The headline wording changes from "the tool speaks first before you type" to "the next session opens with the next step." How `SessionStart` output is displayed has changed between Claude Code releases; T09 verified the banner and the offer to continue on Claude Code 2.1.312 on 2026-11-07. The configured summarizer does not replace extraction at fold time. Deterministic extraction always runs when the session end is folded, and when `config.summarizer` is set a summarize job is queued under `queue/summarize/` that only the daemon or `coal queue drain` runs later; its result replaces the extraction, and it is skipped when the model recorded state since the session started. A user without a running daemon still gets the extracted summary. The summarizer runs with no shell, a timeout and `COAL_SUMMARIZER=1` set, and hooks exit with nothing done under that variable, so the summarizer's own session is never captured.

**In the code.** `cmd/coal/hook.go` dispatches the five hook events and `internal/capture/claudecode.go` turns them into capture events. `internal/queue` holds the events and jobs; `internal/fold/fold.go` folds them into manifests, and the daemon tick in `internal/daemon/daemon.go` drains the queue. Extraction is `internal/capture/extract.go`; the opt-in summarizer is `internal/summarize`. coal_update_state is in `internal/mcpserver/tools.go`. The SessionStart banner and additionalContext are built in `internal/capture/resume.go`; `cmd/coal/resume.go` is the coal resume launcher.

## ADR-017: Places for areas of work

**Status.** Proposed 2026-11-06. Phase 5, which starts only after the Phase 4 retro. The exception is `intent.name` and `intent.icon`, which may come earlier (see Consequences).

**Context.** Applications in an operating system each had a name and an icon, and opening one put the user into a kind of work. AI chat interfaces mostly lost that. Most tools open on an empty prompt and a list of conversations. Some, such as Claude Projects and ChatGPT Projects, group chats, files and instructions, but only their own. Across tools the user rebuilds the setup each time: which chat, which files, which data sources. COAL records the work, and resume (ADR-016) brings back the next step of one manifest, but nothing opens a manifest together with the chats, files and data sources that go with it. Much of that work also happens outside AI tools. A video is planned and storyboarded with AI, cut in Final Cut Pro from footage in a folder, and published on an upload page. Some work never finishes. A home budget is an area of ongoing responsibility, and "add CSV export" is a project inside it. The schema does not tell the two apart. Places do not replace apps such as Final Cut Pro, and they do not bring back fixed applications, which the thesis expects to dissolve. They bring back the entry point: a name, an icon and a starting set of the apps, chats and files an area uses, built around the user's work instead of one vendor's interface.

**Decision.**
- A place is a manifest a person can open, usually an area. It gets an optional `intent.name`, such as "Home budget", and an optional `intent.icon`: an emoji, or the `sha256:` hash of an image in the same manifest's `artifacts[]`. `intent.name` is the user's words, like `intent.summary` (ADR-009). A model may propose one, and it is written only when the user accepts. Readers show `intent.summary` when there is no name.
- A manifest gets an optional `kind`: `project`, the default, for work that can reach `status: done`, or `area`, which stays open and is never `done`. This is unrelated to `contexts[].kind`. A project joins an area through the existing `links[]` entry with `rel: partOf`. Capture creates projects, not areas. The user makes an area and puts projects in it; a model may propose the link, written when the user accepts. This answers the hierarchy question in `docs/04-INTENT-MANIFEST.md` for projects: `partOf` is enough.
- A manifest gets an optional `kit[]`: the entry points the work uses. Each entry has a `title`, a locator and a `type` that says how it opens: `chat` (a conversation or chat project to reopen), `mcp` (a server attached to the tool) or `link` (any other `uri`, opened with the system handler). An optional `label`, such as "planner" or "ideas", is for display only. For the home budget the kit could hold the claude.ai project, the budget spreadsheet, the folder of bank exports, the planner list, the ideas note and a spreadsheet MCP server. For a video it could hold the planning chat, the storyboard, the Final Cut Pro library, the footage and music folders, and the upload page. An app outside AI joins a kit through the file or URL it opens, such as the Final Cut Pro library.
- A kit entry is a locator. It is not copied or hashed, and it does not drive surfacing. What the work produced stays in `artifacts[]`, and where the work should surface stays in `contexts[]`.
- An `mcp` entry holds only the name of a server the user has already configured, in `~/.coal/config.json` or in the tool's own settings. It never holds a command, arguments, a URL or environment, so a manifest cannot make COAL run anything new.
- `coal open <id>` on a place, or any launcher, shows its name, icon, kit, active projects and next step, and launches from the kit. `coal open --artifact n` keeps its Phase 1 meaning, and so does `coal open` on a manifest with no kit. For Claude Code, opening passes the place's servers at launch with `--mcp-config` and a file under `~/.coal/`, so nothing is written into the user's project, and injects the next step as resume does. `coal open` asks before it attaches a server or launches something other than a document for the first time in a place. Kit entries written by a model or another program are shown, not launched, until the user accepts them.

Considered: a separate workspace object. Not for now: it is a second record type for readers and writers to learn, and an area with `partOf` projects covers the same ground. Putting `kit[]` under `ext["io.coalmine.place"]` until a second client reads it, as ADR-015 does for Agent Trace. A fair first step for the kit; `intent.name`, `intent.icon` and `kind` change how every reader lists work, so they are first-class fields. Leaving it to each tool: each holds only its own pieces. A COAL desktop shell that replaces the system launcher: it would compete with operating systems and launchers instead of feeding them. The Phase 2 Raycast extension and the Phase 3 menu bar app stay thin clients that read places.

**Consequences.**
- The fields are optional and readers that follow design rule 5 ignore them. Two catches for whichever version adds them: the v0.1 schema pins `version` and `$schema` with `const`, so a new version needs a rule for older readers; and a writer that drops unknown keys on save will erase a kit on its next write, so the Go manifest type must keep unknown keys (ADR-018).
- A kit points at private things: chat URLs, file paths, server names. Design rule 6 applies: entries are locators, never credentials. When a manifest is exported, or synced with `privacy.scope` other than `local`, its kit is left out unless the user includes it.
- Coding agents and desktop apps read MCP servers from config files, and some take them at launch, so COAL can attach servers to them. Web chat apps such as claude.ai and chatgpt.com take connectors only through their own settings and cannot reach a local server. There a place is a list of links.
- `intent.name` and `intent.icon` are useful before Phase 5, in search results and the web UI, so they may move into v0.2 after the Phase 1 retro, ahead of the rest of this ADR. That moves a small piece of Phase 5 forward, the way ADR-016 moved `coal resume` into Phase 1.
- Places need artifacts kept by reference. A 40 GB video export is too large to copy into `~/.coal/artifacts/`, and a Final Cut Pro library is a `.fcpbundle` package, a folder that changes on every edit, so it has no single body to hash. References build on the artifact identity from issue #5, which ADR-019 accepts: an artifact with a `uri` is identified by (manifest, `uri`) and keeps its history in `versions[]`. The `versions[]` model wins over a separate reference artifact. A reference is a version in `versions[]` that records `uri`, `size` and a new `modifiedAt`, sets `skipped: "reference"`, and has no `hash`. `hash` becomes optional on a version only when `skipped` is set; everywhere else it stays required. Design rule 3, as ADR-019 rewrites it, already gives such an artifact its identity, and it is shown as missing, not removed, when the file moves. Two rules still have to say so: the store conformance rule (it covers only bodies the store copied) and the convention that every artifact is copied into `~/.coal/artifacts/<hash>`. COAL keeps no copy, so it cannot bring the file back if the user deletes it. `intent.icon`, when it is a hash, always names a copied artifact in the same manifest, never a reference. Making `hash` optional on a skipped version is not an additive change (design rule 5), and `docs/04-INTENT-MANIFEST.md` lists large artifacts as a question for v1.0, so this schema change must land by spec v1.0 in Phase 4, ahead of the rest of this ADR.
- Places also need a way to see work in apps that have no hooks. A place may watch the folders in its kit, a local signal like those in Phase 3. Watching moves `state.lastActivityAt`, and in folders the user marks for exports it registers new files as artifacts kept by reference, so new footage clips are not registered. Watching is opt-in per folder, local, and reads file names, sizes and times, not contents. Artifacts a watcher adds follow the kit's export rule. Without watching, a model records progress through `coal_update_state`, or the user edits the manifest. This can land with Phase 5.
- If this ADR is accepted, the phase map in `docs/03-ARCHITECTURE.md` and the roadmap on the site gain Phase 5 too.

## ADR-018: Adopt the Go proof of concept as the Phase 1 implementation

**Status.** Accepted 2026-11-07. Supersedes ADR-005. Layout detail filled in on 2026-11-08 from the implementation: `internal/drift` joins the layout as a test-only package that adds no code to the binary.

**Context.** ADR-005 chose a TypeScript monorepo. Three weeks later it had produced no code: two empty package READMEs and the workspace files. Meanwhile Dave Nicoll built a proof of concept in Go on the `poc/go` branch (`d041c64`) that implements the Phase 1 plan end to end: capture from Claude Code hooks, store, index, daemon, MCP server, web UI, import, history and resume. On 2026-11-07 it was built from a fresh clone on macOS. The demo's capture, find, resume banner and `coal resume` steps passed; only the web UI check did not, because the UI cannot be built there. A live Claude Code 2.1.312 session captured a manifest, recorded its state over MCP, opened the next plain session by offering to continue, and resumed through `coal resume`. The README says the schema is the product; the language of the first implementation is an implementation detail, and ADR-005 already allowed a Go implementation behind the same schema.

**Decision.** `main` adopts the proof of concept as the Phase 1 implementation.

Stack: Go 1.27; `cobra` for the CLI; a `net/http` daemon bound to 127.0.0.1; the official Go MCP SDK (`github.com/modelcontextprotocol/go-sdk` v1.8.0) over stdio; `modernc.org/sqlite` with FTS5, no cgo; the JSON Schema validated with `github.com/santhosh-tekuri/jsonschema`; a web UI in Svelte 5, built with Bun and embedded in the binary; a Makefile for build, test and demo.

Layout: `cmd/coal` is the one binary, with the CLI, the daemon, the MCP server and the hooks as subcommands; `internal/{app,capture,daemon,fold,importer,index,manifest,marker,mcpserver,metrics,paths,queue,store,summarize}`; `spec/` holds the schema, embedded in the binary; `web/` the Svelte UI; `scripts/` the demo, replay, MCP smoke test and sandbox guard; `testdata/claude-code` the recorded hook payloads and transcripts; `docs/poc` the PLAN, STATUS and DEMO of the proof of concept.

`poc/go` was merged into `main` through the `adopt-go` branch on 2026-11-07 (PR 29); that branch is history and nothing is committed to it any more. The TypeScript, pnpm, Fastify, Zod and Vite+React sections of CLAUDE.md and the docs were retired, and the empty `packages/coal` and `packages/diamond` READMEs, the root `package.json` and `pnpm-workspace.yaml` were removed. ADR-005's two-package rule is replaced by one binary and internal packages; a package is split out only when a second implementer needs one piece alone.

**Consequences.** CLAUDE.md, CONTRIBUTING, README and docs/02, 03 and 05 were updated in the same change (PR 29). Tests must pass on macOS and Linux; on 2026-11-07 three Go test packages failed on macOS, and each is tracked as an issue: the MIME table has no Markdown entry on macOS, so a `.md` artifact is served as plain text (#22); symlinked temp directories break the import and store tests, because the project root is resolved and the compared paths are not (#23); a store test expects `/proc` and the sensitive-path check misses a symlinked home and macOS Library paths (#24). Other bugs found in the same run are tracked the same way: a chat-only session overwrites `state.summary` (#20); `session_attached` and `resume_acted` only count file writes (#21); the `.coal` marker schema URL is not published (#27); `intent.summary` is cut at 200 characters mid-sentence (#28). None of them was fixed in that change; each is one issue and one later PR to `main`, and all of them are still open. The web UI depends on `@davenicoll/web-framework`, a private sibling checkout that is not published anywhere, so `make web`, `make e2e` and the demo's UI check only run on Dave's machine; the framework must be vendored or published before a release (#26). The demo scripts need GNU bash 4 or later and GNU coreutils until they are rewritten, and the Makefile expects Go under `~/.local/go`, so `GO=` must point at the installed Go 1.27 elsewhere (#25).

**In the code.** The stack is `go.mod` and the `Makefile`. The layout is `cmd/coal`, `internal`, `spec`, `web`, `scripts`, `testdata/claude-code` and `docs/poc`, as the Decision lists them. `internal/drift` is a test-only package that checks the direct dependencies in `go.mod` against this ADR's list and adds no code to the binary.

## ADR-019: Artifact identity is (manifest, uri) with versions[]

**Status.** Accepted 2026-11-07. Resolves #5, #6 and #7. Constrains ADR-017.

**Context.** The first draft of the schema identified every artifact by content hash, so a file edited across sessions appeared as a new artifact each time (#5), two writers could overwrite each other's manifest unnoticed (#6), and nothing in the schema kept a secret out of an artifact body or out of `sessionRef` (#7). The proof of concept answered all three with small schema changes, and docs/04 design rule 3 already states the identity rule. Every change is an optional field or a tighter constraint on an existing one, so it fits design rule 5, additive evolution.

**Decision.** The Intent Manifest schema stays at version 0.1 and its published `$id`. The following are accepted into 0.1:

- top-level `rev`, an integer starting at 1 and incremented on every write, so a writer can pass the `rev` it read and get a conflict if it moved (#6);
- `artifacts[].versions[]` of `$defs.artifactVersion` (`hash`, `size`, `createdAt`, `producer`), newest last; the top-level `hash`, `size`, `createdAt` and `producer` mirror the latest version so simple readers still work (#5);
- `artifacts[].skipped`, a string saying why a body was not copied (#7);
- `artifacts[].ext`, for extension keys such as `dev.agent-trace` (ADR-015);
- `sessionRef` must not look like a token, enforced with a `not: {pattern}` for common token shapes (#7);
- `intent.source` defaults to `"model"`.

An artifact with a `uri` is identified by (manifest, uri) and keeps its history in `versions[]`. An artifact without a `uri` is identified by content hash. 0.1 stays Draft and freezes at the Phase 1 retro (T14).

ADR-017 (Places, PR 19, Proposed) must build its "artifacts kept by reference" on `versions[]`: a version kept by reference records `uri`, `size` and a new `modifiedAt`, sets `skipped: "reference"`, and has no `hash`. `hash` therefore becomes optional on a version only when `skipped` is set. `intent.icon` is a hash, so the icon is always a copied artifact. ADR-017 must reference #5 and state that the `versions[]` model wins.

**Consequences.** Readers written against the first draft keep working: the top-level artifact fields are unchanged and every new field is optional. Writers that ignore `rev` still write, but without the conflict check. `skipped` gains the value `"reference"` beside the secret reasons and `"purged"`. The schema in `spec/` and docs/04 carry these fields on `main` since ADR-018 (PR 29), and the schema served at the `$id` matches them. Because 0.1 is still Draft, no version bump is needed; after the retro, any further change to the schema needs a new version.

**In the code.** The fields are in `spec/intent-manifest.schema.json` and the Go types in `internal/manifest/manifest.go`. `internal/store/store.go` increments rev on every write and returns a conflict when it moved; `internal/store/artifact.go` keeps versions[] by (manifest, uri) and mirrors the latest version into the top-level fields; `internal/store/secrets.go` decides when a body is skipped. `cmd/coal/purge.go` marks versions purged. `spec/examples/budget-tracker.manifest.json` shows the fields in use and is validated by `internal/manifest`.
