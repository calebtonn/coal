# Architecture Decision Records

Short form. Add a new entry for any decision that changes the shape of the system.

## ADR-001: Local-first, files as source of truth

**Context.** The thesis is user ownership of intent and artifacts. A hosted database contradicts that on day one.

**Decision.** `~/.coal/` holds plain JSON manifests and content-addressed artifact bodies. SQLite is a derived index only.

**Consequences.** No sync in MVP. Multi-device is a Phase 4 problem solved by syncing files (e.g. encrypted directory sync), not by a server owning the data.

## ADR-002: MCP as the primary read/write interface for AI tools

**Context.** Every major AI tool is adopting MCP. Building vendor-specific plugins for each would be a treadmill.

**Decision.** The MCP server is the canonical integration point. CLI and REST exist for humans and scripts, not for models.

**Consequences.** COAL works with any future MCP client without changes. Capture still needs per-tool adapters where lifecycle hooks are needed (Claude Code first).

## ADR-003: Claude Code hooks for first capture, no browser capture in MVP

**Status.** Accepted. Amended by ADR-011: Claude Code is no longer the only tool with hooks; the MVP scope stands.

**Context.** Browser capture from claude.ai or chatgpt.com requires an extension and fights with page structure that changes without notice. Claude Code exposes deterministic hooks.

**Decision.** MVP captures only from Claude Code. Browser capture is Phase 2.

**Consequences.** The MVP proves capture, find, and resume for a developer audience first. The "HTML buried in a chat" case on claude.ai is served in MVP only by manual `coal add` from a downloaded artifact.

## ADR-004: Full-text search before semantic search

**Context.** Vector search needs an embedding model, which means either a local model or a network call.

**Decision.** FTS5 only in MVP. Intent summaries are short and written in the user's words; keyword match is usually enough.

**Consequences.** "Find the thing about money" may miss "budget tracker." Acceptable for MVP; revisit with a local embedding model in Phase 2.

## ADR-005: TypeScript monorepo

**Context.** MCP SDK, Claude Code hook scripting, and the web UI are all most natural in TypeScript.

**Decision.** One language. pnpm workspaces with exactly two packages: `diamond` (library) and `coal` (the installable, which runs the daemon, MCP server and hooks as subcommands and serves the web UI). Six packages were the first draft; collapsed because a solo nights-and-weekends build should spend its hours on the demo, not on workspace plumbing.

**Consequences.** `better-sqlite3` requires a native build; document the prerequisites. A Rust or Go implementation could replace `diamond` later behind the same schema. Split packages only when a second implementer needs one piece alone.

## ADR-006: Open source under Apache 2.0, spec published separately

**Context.** Adoption depends on trust; trust depends on auditability. The schema matters more than the implementation.

**Decision.** Code and spec both Apache 2.0. The spec gets its own repo and versioning once there is a second implementer.

**Consequences.** Decided 2026-11-04: fully open source, no paid tier planned. Sync, org install and any hosted piece are open too. If that ever changes it needs a new ADR and a check against the author's employment terms.

## ADR-007: The whiteboard pipeline is deferred

**Context.** The original sketch routed signals through API Gateway, EventBridge, Lambda, Glue, S3, and SageMaker into Agent Core.

**Decision.** None of that is in the MVP. Signal collection, cognitive state inference, and the policy engine are Phase 3 and will run locally first.

**Consequences.** If Phase 3 ever needs hosted inference, it is a per-user opt-in that reads the local manifests, not a central pipeline that all signals flow through by default.

## ADR-008: COAL is a record of work, not a memory

**Context.** Mem0 OpenMemory, OpenPortable, and the platforms' built-in memory all store facts, preferences, or session notes across tools. Positioning COAL as "memory" invites a direct comparison on their terms and loses.

**Decision.** COAL stores what the user made, why, and what is still open: intent, content-addressed artifacts with provenance, open loops, and contexts. It does not store facts about the user. It is designed to sit on top of a fact store rather than replace one; a future adapter may read user preferences from OpenMemory to improve summaries and surfacing.

**Consequences.** The word "memory" does not appear in UI copy, tool descriptions, or docs. The MCP tools are named around work (`coal_find`, `coal_record_intent`), not recall. The headline feature is resume: the next tool opens by offering to continue, driven by `contexts[]` and `state.nextStep`.

## ADR-009: The user writes the intent summary

**Context.** A record of work is only as good as its index, and `intent.summary` is the index. If the model writes it, the record fills with paraphrases that sound alike. If the user writes it, each entry costs a sentence and compounds like a commonplace book.

**Decision.** `intent.summary` defaults to the user's own first message of the session. The model's proposals go to `intent.goal` and `state.summary`. `intent.source` records `user` or `model`. Search ranks user-written summaries first.

**Consequences.** Some summaries will be rough ("build me a thing for the budget"). That is acceptable; it is what the user will search for. A later `coal rename` lets the user tighten a summary by hand.

## ADR-010: Domains

**Context.** Two domains were registered on 2026-11-04: coalmine.io and coalmine.app.

**Decision.** coalmine.io is the standard: the published schema (`/schema/intent-manifest/<version>.json`), the spec, the docs and the project site. coalmine.app is reserved for anything that runs: the hosted MCP endpoint, sync, or an API, if and when any of those exist. Package names, the CLI and the data directory stay `coal`.

**Consequences.** The schema `$id` is `https://coalmine.io/schema/intent-manifest/0.1.json` and must be served at that path once the site is up (Phase 1 task). In Phase 1 nothing runs on coalmine.app; it redirects to coalmine.io. The one-page project site at coalmine.io is built from `site/` and deployed by `.github/workflows/pages.yml`, together with the schema at its `$id`. Nothing else is hosted before the Phase 1 retro.

## ADR-011: One capture contract, adapters for every coding agent with hooks

**Status.** Accepted 2026-11-05. Amends ADR-003.

**Context.** ADR-003 chose Claude Code because it was the only mainstream tool with lifecycle hooks. That is no longer true (#8): Cursor, Codex CLI and Gemini CLI all have session, tool and stop or end events, and all pass JSON on stdin. If the Claude Code adapter is written as a special case, each later tool means rework, and Phase 2's "Reach" stays expensive for no reason.

**Decision.** Phase 1 defines a small internal capture contract that adapters translate into: `sessionStart {tool, cwd, sessionRef}`, `fileWritten {path}`, `turnEnd`, `sessionEnd {transcriptPath?}`. The Claude Code adapter is its first implementation. The MVP still ships Claude Code only. Cursor and Codex adapters are the first Phase 2 items. Phase 2's hard problem is restated as chat apps (claude.ai, chatgpt.com), which have no hooks.

**Consequences.** A little extra work in T08 to keep the contract separate from Claude Code's payload shapes. Phase 2's "done when" (a manifest written by something other than Claude Code, resumed from a third tool) becomes reachable with two small adapters. ADR-003's MVP scope stands; only its reasoning changes.

## ADR-012: Import from session records already on disk

**Status.** Accepted 2026-11-05. First Phase 2 item.

**Context.** The case that started COAL is a page buried in a claude.ai chat, and the MVP only serves it through a manual `coal add`. Browser capture is the planned fix, and ADR-003 already notes it is fragile. Meanwhile every coding agent writes its sessions to disk (Claude Code under `~/.claude/projects/`, Codex under `~/.codex/sessions/`), and claude.ai and ChatGPT both offer account data exports. Live capture also starts from an empty record, so the week six retro measures a record with only six weeks in it.

**Decision.** Add `coal import`. It reads these sources, proposes manifests (grouped by working directory for coding agents, by conversation for chat exports), and shows them for the user to accept, merge or discard before anything is written. Artifacts in chat exports (code blocks, HTML) are registered as artifacts. Grouping chat conversations into intents uses a model only when the user has configured one, which keeps the no-network rule. Import is the first Phase 2 item, or goes into Phase 1 week 5 if hardening finishes early.

**Consequences.** The browser extension moves later and may not be needed. Transcript formats are undocumented and change, so each importer is versioned and skips what it cannot parse. Import is batch, so chat-side state lags until the next run. The record has history on day one, which makes the retro a fairer test.

## ADR-013: The project directory anchors the intent

**Status.** Accepted 2026-11-05, with the `markers` setting. Resolves #4.

**Context.** Sessions are not intents. One session can touch several intents, and one intent runs across many sessions and tools. Matching sessions to manifests is the hardest part of capture, and the current spec leaves it open (#4). For coding work there is already a boundary the user chose: the project directory. F5 mentions a `.coal` marker file but nothing defines it.

**Decision.** On the first capture in a directory, COAL writes a `.coal` file at the git top level (or cwd when there is no repo) holding `{"$schema": "...", "manifest": "<id>"}` and nothing else. Every later session from any tool in that directory or below attaches to that manifest. The manifest itself stays in `~/.coal/manifests/`, so there is still one source of truth (ADR-001). By default the marker is listed in `.git/info/exclude`; the user can choose to commit it. `coal new` in a directory with a marker creates a second manifest and makes it the active one; the marker then holds `active` plus a list. Optionally, `coal adapter install` adds one line to the project's `AGENTS.md` or `CLAUDE.md` telling agents to call `coal_get` with the id in `.coal` at the start of work, so agents with no COAL adapter can still resume.

**Consequences.** Identity for coding work is deterministic and needs no model. Chat work has no directory and still relies on grouping (ADR-012). COAL writes a file into user projects, which some people will not want, so `markers: "project" | "none"` is a setting, with `none` falling back to `contexts[]` path matching.

## ADR-014: Keep history of manifest changes

**Status.** Accepted 2026-11-05 with two constraints: commits never run inside a hook (SessionEnd has a 1.5 s budget); the daemon or queue worker commits, debounced, under the same per-manifest lock as the write (#6). Uses the `git` on PATH; if none, skip silently. Lands in T11.

**Context.** Manifests are mutable. `nextStep`, `status` and `openQuestions` are overwritten in place, so there is no way to answer "what was next last week", to see when an intent was abandoned, or to recover from a bad write by a model.

**Decision.** `~/.coal/` is a local git repository with no remote by default. The store commits after each write, holding the per-manifest lock from #6, with a message naming the tool and operation. `artifacts/` is excluded, since it is already content-addressed and append-only, and `index.db` is excluded because it is derived. `coal log <id>` shows a manifest's history. `reindex` still reads `manifests/` only.

Considered: an append-only event log per manifest with the manifest as a projection. More precise, but a second format for readers and implementers to understand. Not keeping history: the current spec.

**Consequences.** Git (or `isomorphic-git`) becomes a dependency. History comes with ordinary tools: `git log`, `git diff`, `git revert`. Phase 4 sync can start from syncing a git repository. Lost updates are still handled by #6; history only makes them visible.

## ADR-015: Use Agent Trace for line-level code provenance

**Status.** Accepted 2026-11-05.

**Context.** Each artifact carries a `producer` (tool, model, session). Agent Trace, published by Cursor in January 2026 under CC BY 4.0 and supported by Cognition, OpenCode and others, is an open, storage-agnostic spec that links code ranges to the conversations and contributors behind them. The README says the schema is the product. A second provenance format next to an emerging standard would compete with it rather than build on it.

**Decision.** `producer` stays minimal: tool, model, sessionRef. COAL does not add line-level attribution to the manifest. When an Agent Trace record exists for a code artifact, or an adapter can produce one, the artifact references it under `ext["dev.agent-trace"]` with the record's URI or hash. COAL owns intent and state; Agent Trace owns which lines came from where.

**Consequences.** Code provenance from COAL can be read by tools that adopt Agent Trace, at the cost of one extension key. Agent Trace is still a draft, so the reference stays under `ext` until v1.0 of both specs, when it can be reconsidered as a first-class field. Reference: https://agent-trace.dev/

## ADR-016: Capture lifecycle and the resume moment

**Status.** Accepted 2026-11-05. Resolves #1, #2, #3.

**Context.** The first draft assumed `Stop` meant end of session, that a hook could ask the running model for a summary, and that a `SessionStart` hook could make the assistant speak before the user typed. None of those are true of Claude Code hooks: `Stop` fires after every assistant turn, `SessionEnd` is the per-session event with a 1.5 second budget, a command hook cannot query the session's model, and no assistant turn happens until the user sends one.

**Decision.**
- `PostToolUse` and `Stop` do cheap work only: register artifacts, bump `state.lastActivityAt`.
- `SessionEnd` writes a job file under `~/.coal/queue/` and returns. The daemon or the next `SessionStart` drains the queue. Sessions that never end cleanly are drained the same way.
- Summaries: the model records its own state through `coal_update_state`, prompted by `SessionStart` context and the tool description. If nothing was recorded, deterministic extraction from the transcript (last user message, last assistant message, files touched) fills `state.summary`. A headless model call over the transcript is an opt-in `config.json` setting, which counts as a call the user configured. `intent.summary` stays the user's first message (ADR-009).
- The resume moment has two forms. Plain `claude` in a matched directory: the `SessionStart` hook prints a `systemMessage` banner with the next step before the user types, and injects `additionalContext` so the first reply opens by offering to continue. `coal resume`: a launcher that finds the manifest for the directory and starts the harness with a first prompt already supplied, so the first turn is the resume. The demo uses `coal resume`.

**Consequences.** `coal resume` moves a small piece of the Phase 2 entry-point idea into Phase 1. The headline wording changes from "the tool speaks first before you type" to "the next session opens with the next step." How `SessionStart` output is displayed has changed between Claude Code releases; T09 verifies on the current build.

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
- The fields are optional and readers that follow design rule 5 ignore them. Two catches for whichever version adds them: the v0.1 schema pins `version` and `$schema` with `const`, so a new version needs a rule for older readers; and a writer that parses with Zod's default object schema drops unknown keys on save, so T02 should keep unknown keys or a v0.1 COAL will erase a kit on its next write.
- A kit points at private things: chat URLs, file paths, server names. Design rule 6 applies: entries are locators, never credentials. When a manifest is exported, or synced with `privacy.scope` other than `local`, its kit is left out unless the user includes it.
- Coding agents and desktop apps read MCP servers from config files, and some take them at launch, so COAL can attach servers to them. Web chat apps such as claude.ai and chatgpt.com take connectors only through their own settings and cannot reach a local server. There a place is a list of links.
- `intent.name` and `intent.icon` are useful before Phase 5, in search results and the web UI, so they may move into v0.2 after the Phase 1 retro, ahead of the rest of this ADR. That moves a small piece of Phase 5 forward, the way ADR-016 moved `coal resume` into Phase 1.
- Places need artifacts kept by reference. A 40 GB video export is too large to copy into `~/.coal/artifacts/`, and a Final Cut Pro library is a `.fcpbundle` package, a folder that changes on every edit, so it has no single body to hash. References build on the artifact identity from issue #5, which ADR-019 (on main after the adopt-go merge) accepts: an artifact with a `uri` is identified by (manifest, `uri`) and keeps its history in `versions[]`. The `versions[]` model wins over a separate reference artifact. A reference is a version in `versions[]` that records `uri`, `size` and a new `modifiedAt`, sets `skipped: "reference"`, and has no `hash`. `hash` becomes optional on a version only when `skipped` is set; everywhere else it stays required. Design rule 3, as ADR-019 rewrites it, already gives such an artifact its identity, and it is shown as missing, not removed, when the file moves. Two rules still have to say so: the store conformance rule (it covers only bodies the store copied) and the convention that every artifact is copied into `~/.coal/artifacts/<hash>`. COAL keeps no copy, so it cannot bring the file back if the user deletes it. `intent.icon`, when it is a hash, always names a copied artifact in the same manifest, never a reference. Making `hash` optional on a skipped version is not an additive change (design rule 5), and `docs/04-INTENT-MANIFEST.md` lists large artifacts as a question for v1.0, so this schema change must land by spec v1.0 in Phase 4, ahead of the rest of this ADR.
- Places also need a way to see work in apps that have no hooks. A place may watch the folders in its kit, a local signal like those in Phase 3. Watching moves `state.lastActivityAt`, and in folders the user marks for exports it registers new files as artifacts kept by reference, so new footage clips are not registered. Watching is opt-in per folder, local, and reads file names, sizes and times, not contents. Artifacts a watcher adds follow the kit's export rule. Without watching, a model records progress through `coal_update_state`, or the user edits the manifest. This can land with Phase 5.
- If this ADR is accepted, the phase map in `docs/03-ARCHITECTURE.md` and the roadmap on the site gain Phase 5 too.
