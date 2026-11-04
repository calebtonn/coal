<p align="center">
  <img src="assets/icon-256.png" alt="COAL icon: a lump of coal cracked open with a diamond inside" width="160">
</p>

<h1 align="center">COAL</h1>
<p align="center"><b>Cognitive Orchestration AI Layer</b></p>
<p align="center">A user-owned record of work that sits between you and every AI tool you use.</p>

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

## Phases

Each phase is useful on its own and has a stop condition. Only Phase 1 has a date. The rest are earned by the retro at the end of the phase before, not scheduled.

### Phase 1: Record (six weeks)

What it proves: a user-owned record of work can be captured without effort and read back by a different tool. The next session can open by offering to continue.

- Intent Manifest v0.1, published schema with validator and example
- Claude Code hooks: capture on write, summarize on stop
- Resume: a new session speaks first with the next step
- MCP server, `coal` CLI, local web UI
- Two packages: `diamond` and `coal`

Stop condition: the week six retro. If the author did not reach for it unprompted at least five times in a week, it is a tool, not a layer.

### Phase 2: Reach

What it proves: the record works across every tool, not just the one with hooks. Capture is not a Claude Code feature.

- `coal import`: build manifests from existing session logs on first run
- Browser capture from claude.ai and chatgpt.com
- Cursor and other MCP writers
- Semantic search with a local embedding model
- Launcher integration (Raycast) as the entry point

Stop condition: a manifest written by a tool other than Claude Code is found and resumed from a third tool.

### Phase 3: Attention

What it proves: the record is enough to anticipate. The whiteboard loop (sense, model, decide, act) runs on user-owned data with user-owned rules, and people keep it on.

- Signals: calendar, git activity, IDE focus, time of day
- Surfacing by context, not search
- Policy engine: editable rules, overrides as the training signal
- Interrupt gating for Slack and chat
- Menu bar app for notifications and launching

Stop condition: surfaced items are opened more often than dismissed, and nobody has turned surfacing off.

### Phase 4: Portability

What it proves: this is a standard, not a tool. A second implementer writes manifests and a user moves between stores without loss.

- Spec v1.0 with a second implementer
- Encrypted multi-device sync
- Export and import between stores
- Optional fact-store adapter (for example OpenMemory) for preferences
- Org tier: fleet install, SSO, aggregate visibility only

Stop condition: a manifest written by software this repo did not produce validates and resumes here.

## Principles

- **Local-first.** Your data lives in `~/.coal/`. No account required to use it.
- **Files are truth.** Manifests are plain JSON. SQLite is a derived index that can be rebuilt at any time.
- **Open standard.** The Intent Manifest schema is the product. The daemon is a reference implementation.
- **User-owned policy.** Any rules about attention, interruption, or routing belong to the user and are readable by the user.

## License

Apache 2.0 (proposed).
