# Thesis

## The claim

Applications as fixed interfaces are dissolving. Increasingly the interface is generated on demand by a model: you ask for a budget tracker and one appears. What does not dissolve is the data and services behind those interfaces (your calendar, your repos, your messages) and, more importantly, the person's own intent and working state.

Every AI tool today keeps its own copy of that state inside its own sessions. Claude knows what you did in Claude. Cursor knows what you did in Cursor. None of them know what you did in the other, and none of them have an incentive to share, because the session history is the lock-in.

The result is already visible to anyone using these tools seriously: dozens of parallel chats, artifacts buried inside them, and the only index being "which conversation was that in."

## The bet

In an AI-first OS, the durable objects are not apps and not chats. They are:

- **Intent**: what the user is trying to accomplish
- **Artifacts**: what has been produced toward it
- **State**: where things stand and what comes next

Whoever holds those three things holds the cognitive model of the user. The platforms want to hold it. COAL bets that the user should, and that if the user holds it in an open, portable format, every AI tool gets better because it can read it.

## The wedge

The first problem COAL solves is narrow and felt today: find and resume the thing you built, regardless of which AI tool built it. That is a cross-tool artifact and intent index, exposed to AI tools over MCP so the model you are talking to can look it up for you.

Everything on the original whiteboard (sense, model, decide, act; interrupt gating; IDE concierge) is a later feature of the same layer. The index comes first because it is useful alone, requires no inference, and proves the core thesis: a user-owned layer can sit underneath many AI tools at once.

## Where it sits among memory systems

The useful map is the one from cognitive science, because that is how people in this space will place it on first read:

- Semantic: what is true about you. Facts, preferences, observations. Mem0 OpenMemory, Hindsight, and the platforms' built-in memory live here. Graphiti (Zep) is here too, as a temporal knowledge graph of entities and relations extracted from ingested text.
- Procedural: how to do things. Skills files, `CLAUDE.md` and `AGENTS.md`, handoff notes.
- Episodic: what happened and what you made. The work itself, with its intent, its artifacts and where it stands. Nobody holds this in a form the user owns and other tools can read.

COAL is the episodic one. It does not compete with the semantic stores; it sits beside them, and a later adapter may read preferences from one to improve summaries and surfacing. Graphiti's "episode" is an ingested message that gets decomposed into facts. COAL's episode is the work product, kept whole, content-addressed, with provenance and a next step. That difference is the whole project.

Product copy still says "record of work," not "memory." That is positioning, not disagreement with the map.

## Prior art

Several projects overlap parts of COAL. None of them holds the cross-tool record of work.

| Project | What it does | How COAL differs |
|---|---|---|
| [claude-mem](https://github.com/thedotmack/claude-mem) | Captures Claude Code sessions through hooks, summarizes them into local SQLite with vector search, injects context at session start. Around 46K GitHub stars in April 2026. | Resume inside Claude Code is already solved there. COAL's difference is the record across tools, artifacts kept whole with provenance, and a schema the user owns. |
| [Basic Memory](https://github.com/basicmachines-co/basic-memory) | Markdown files as truth, SQLite FTS5 as a derived index, MCP as the interface. | Same storage pattern, different object: notes, not work products with state. |
| [Beads](https://github.com/steveyegge/beads) | A git-backed issue tracker for coding agents, kept as JSONL in the repo. | Overlaps `state`, `nextStep` and `openQuestions` for coding work. COAL also covers chat work and artifacts. A Beads exporter is a candidate second writer. |
| [Mem0 OpenMemory](https://mem0.ai/openmemory) | A fact store about the user, shared across tools over MCP. | Semantic, not episodic. COAL sits beside it (ADR-008). |
| [Agent Trace](https://agent-trace.dev/) | Links code ranges to the conversations and contributors behind them. | COAL references it for line-level provenance instead of growing its own (ADR-015). |
| [Open Memory Protocol](https://github.com/smjai/open-memory-protocol) and similar specs | Portable formats for context across tools. | Aimed at facts. COAL's format is for intent, artifacts and state. |

## Why open

The layer only works if people trust it with everything they are doing. A closed service that reads your sessions across every AI tool is a surveillance product. An open, local-first reference implementation with a published schema is infrastructure. The analogy is RSS: the spec was the product, and readers were interchangeable.

## What would make this wrong

- The major platforms converge on a shared, user-exportable intent format on their own. (Unlikely; it works against their lock-in.)
- Users do not actually want cross-tool continuity, and single-tool consolidation wins. (Possible for consumers; unlikely for people who build things.)
- Capture is too lossy without platform cooperation. (Real risk; the MVP tests it with one tool that has lifecycle hooks, Claude Code.)
- Nobody outside this repo writes manifests, so the format stays one implementation's file layout. RSS spread because publishers had a reason to emit feeds. Here the only writers are COAL's own adapters, and the platforms have a lock-in reason not to write manifests. The answer has two parts. Import (ADR-012) turns vendor data into manifests without vendor cooperation. And Phase 4's second implementer gets a concrete, cheap target instead of waiting for one to appear: a manifest exporter for Beads or Basic Memory, or a manifest writer in a popular agent plugin. Either gives the spec a writer this repo does not control.
