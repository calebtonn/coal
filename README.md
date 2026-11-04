# COAL: Cognitive Orchestration AI Layer

<img src="assets/icon-128.png" alt="COAL icon: a lump of coal cracked open with a diamond inside" width="96">

A user-owned record of work that sits between you and every AI tool you use.

You talk to Claude, ChatGPT, Cursor, Claude Code. Each one produces artifacts (HTML pages, scripts, docs, decisions) and each one forgets what you were doing the moment the session ends. The chat is the only index, and the chat is the wrong index. Nobody thinks "which conversation was that in." They think "the budget thing I built last week."

COAL fixes that by holding three things the user owns and any AI tool can read:

1. **Intent**: what you were trying to do, in your words.
2. **Artifacts**: what got produced, content-addressed, with provenance.
3. **State**: where you left off, what is still open, what comes next.

It runs locally. Files on disk are the source of truth. Any model that speaks MCP can query it.

## Status

Pre-MVP. This repo is the build spec. Start with `CLAUDE.md`, then `docs/05-MVP-TASKS.md`.

## Layout

```
coal/
  CLAUDE.md                     Instructions for Claude Code
  assets/                       Icon: icon.png plus 16 to 512 px sizes
  docs/
    01-THESIS.md                Why this exists, one page
    02-SPEC.md                  MVP functional spec
    03-ARCHITECTURE.md          Components, data flow, diagrams
    04-INTENT-MANIFEST.md       The open standard (v0.1)
    05-MVP-TASKS.md             Ordered build tasks with acceptance criteria
    06-DECISIONS.md             Architecture decision records
  spec/
    intent-manifest.schema.json JSON Schema for the manifest
    examples/                   Example manifests
  packages/
    diamond/                    Types, validation, storage, index (the part inside the coal)
    coal/                       The `coal` CLI: serve, mcp, adapter, find, add, open; web UI under web/
```

Two packages on purpose. One installable, one library. Split further only when a second implementer needs a piece on its own.

## What the MVP does and does not do

Capture in the MVP comes from Claude Code only, because it is the one mainstream tool with lifecycle hooks. The pain that started this project (an HTML page buried in a claude.ai chat) is served in the MVP by `coal add` on a downloaded artifact, and properly by browser capture in Phase 2. The MVP proves the record and the resume; it does not yet prove universal capture.

## Principles

- **Local-first.** Your data lives in `~/.coal/`. No account required to use it.
- **Files are truth.** Manifests are plain JSON. SQLite is a derived index that can be rebuilt at any time.
- **Open standard.** The Intent Manifest schema is the product. The daemon is a reference implementation.
- **User-owned policy.** Any rules about attention, interruption, or routing belong to the user and are readable by the user.

## License

Apache 2.0 (proposed).
