<p align="center">
  <img src="assets/icon-256.png" alt="COAL icon: a lump of coal cracked open with a diamond inside" width="160">
</p>

<h1 align="center">COAL</h1>
<p align="center"><b>Cognitive Orchestration AI Layer</b></p>
<p align="center">A record of your work that you own and any AI tool can read.</p>
<p align="center"><a href="https://coalmine.io">coalmine.io</a></p>

I use Claude, ChatGPT, Cursor and Claude Code in the same week. Each one produces things: an HTML page, a script, a plan, a decision. Each one forgets what I was doing when the session ends. Last week I spent twenty minutes scrolling through chats looking for a budget tracker I had built, because the only way to find it was to remember which conversation it was in.

COAL keeps a record of that work outside any one tool. For each thing you were working on it stores:

- the intent, in your own words
- the artifacts, content-addressed, with which tool and model produced them
- the state: last activity, next step, open questions

It runs on your machine. The record is a folder of JSON files you can read with any editor. Any tool that speaks MCP can search it and write to it. When you start a new session in a folder that has a record, the tool opens by telling you where you left off.

## Status

Nothing is built yet. This repo is the spec and the task list. If you want to follow along or help, start with `CLAUDE.md` and `docs/05-MVP-TASKS.md`.

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

Two packages. `diamond` is the library. `coal` is what you install. I will split them further only if someone else needs one piece on its own.

## What the MVP covers

Capture comes from Claude Code only, because it is the one mainstream tool with lifecycle hooks today. The case that started this (a page buried in a claude.ai chat) is handled in the MVP by running `coal add` on a downloaded file. Browser capture comes in Phase 2. The MVP shows that the record and the resume work with one harness. It does not yet show that capture works everywhere.

## Phases

The MVP is Phase 1. Phases 2 through 4 describe where this goes if Phase 1 holds up. Each one starts only after the retro on the one before it. Phase 1 has a date. The others do not.

| Phase | Name | Status | What it has to show |
|---|---|---|---|
| 1 | Record | MVP, in progress | A record captured in one tool can be read back and resumed in another |
| 2 | Reach | Not started | Capture works from every tool, including ones with no hooks |
| 3 | Attention | Not started | The record plus simple rules is enough to surface the right thing at the right time |
| 4 | Portability | Not started | Someone else's software writes manifests that work here |

### Phase 1: Record (the MVP, six weeks)

- Intent Manifest v0.1, with a published schema, an example and a validator
- Claude Code hooks: capture on write, summarize on stop
- Resume: a new session opens with the next step before you type
- MCP server, `coal` CLI, local web UI
- Two packages: `diamond` and `coal`

Done when: the week six retro. If I have not reached for it unprompted at least five times in a week, I stop.

### Phase 2: Reach

- `coal import`: build manifests from the session logs already on disk
- Browser capture from claude.ai and chatgpt.com
- Cursor and other MCP writers
- Semantic search with a local embedding model
- Raycast extension as the entry point

Done when: a manifest written by something other than Claude Code is found and resumed from a third tool.

### Phase 3: Attention

- Signals: calendar, git activity, IDE focus, time of day
- Surfacing by context instead of search
- Policy engine: editable rules; the user's overrides are the training signal
- Interrupt gating for Slack and chat
- Menu bar app for notifications and launching

Done when: surfaced items get opened more often than dismissed, and nobody has turned surfacing off.

### Phase 4: Portability

- Spec v1.0 with a second implementer
- Encrypted multi-device sync
- Export and import between stores
- Optional adapter for a preferences store such as OpenMemory
- Org install and fleet config, open source like everything else

Done when: a manifest written by software this repo did not produce validates and resumes here.

## Principles

- Local first. Your data lives in `~/.coal/`. There is no account.
- Files are the truth. Manifests are plain JSON. SQLite is an index that can be deleted and rebuilt.
- The schema is the product. The daemon is one implementation of it.
- Rules about attention, interruption and routing belong to the user and are readable by the user.

## License

Apache 2.0. See `LICENSE`. All of it is open source: the schema, the library, the CLI, every adapter, and the policy engine when it exists. No paid tier.
