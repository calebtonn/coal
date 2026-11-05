# Contributing to COAL

Thanks for looking. Nothing in COAL runs yet, so the useful help right now is mostly about the spec, not code.

## Where the project is

COAL is pre-alpha. The repo holds the spec, the decisions behind it, and an ordered build plan (`docs/05-MVP-TASKS.md`). The MVP is built in that order, one task at a time. At the end of the MVP there is a decision gate (task T14): after a week of real use, the project continues to Phase 2, changes direction, or stops. The bar is in the README: if COAL does not get used unprompted at least five times that week, the project stops. If it stops, the repo is archived and everything in it stays available under Apache 2.0.

## What helps now

- Read `docs/02-SPEC.md`, `docs/04-INTENT-MANIFEST.md` and `docs/06-DECISIONS.md`, and open an issue for anything that is wrong, unclear, or contradicts another doc. Say where (file and section), what the problem is, and what you would change. Issues #1 to #9 are good examples.
- If you think the design should change, open a pull request that adds an ADR to `docs/06-DECISIONS.md` with status Proposed. Say what problem it solves and what you considered instead. PR #10 is an example.
- If you would write manifests from another tool, say so in an issue before you build anything. A second writer is what the format needs most.

## What does not help yet

- Pull requests that build MVP tasks out of order, or that nobody asked for. Ask in an issue first.
- Code for Phases 2 to 4 (session import, adapters for other tools, browser capture, semantic search, sync, teams). It starts after the decision gate.
- Changes to the fixed stack in `CLAUDE.md`, or splitting the two packages further.

## House rules

- Prose in docs and UI copy is plain and direct, with no em-dashes.
- COAL is a record of work. Do not call it memory in UI copy, tool descriptions or docs (ADR-008). `docs/01-THESIS.md` is the one exception: it uses the word to place COAL among memory systems.
- `spec/intent-manifest.schema.json` is generated from the Zod schema once that exists (T02). Change the Zod source, not the JSON.
- Never delete user data, never bind to anything but `127.0.0.1`, and never add telemetry or network calls the user did not configure.

## Using AI tools

COAL is built with Claude Code. A person directs the work and reviews everything it produces. Issues and pull requests written with AI tools are fine if you have read and checked them yourself. Say a tool was used; a footer line is enough. Large batches of unchecked generated changes will be closed.

`CLAUDE.md` tells agents to work only on the issue you point them at.

## License of contributions

COAL is licensed under Apache 2.0. By submitting a contribution you agree it is licensed under the same terms (Apache 2.0, section 5). There is no CLA.

## Security

Do not report security problems in public issues. Use GitHub's private vulnerability reporting on this repository (Security tab, "Report a vulnerability").
