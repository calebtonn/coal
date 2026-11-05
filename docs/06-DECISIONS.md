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

**Consequences.** The schema `$id` is `https://coalmine.io/schema/intent-manifest/0.1.json` and must be served at that path once the site is up (Phase 1 task). In Phase 1 nothing runs on coalmine.app; it redirects to coalmine.io. Registering the domain is not a reason to host anything before the Phase 1 retro.
