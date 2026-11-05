# Contributing to COAL

Thanks for looking. COAL is early, so this page is mostly about what kind of help is useful right now.

## Where the project is

COAL is pre-alpha. The repo holds the spec, the decisions behind it, and an ordered build plan (`docs/05-MVP-TASKS.md`). The maintainer builds the MVP in that order, one task at a time. At the end of the MVP there is a decision gate (task T14): after a week of real use, the project continues to Phase 2, changes direction, or stops. If it stops, the repo is archived and everything in it stays available under Apache 2.0.

## What helps now

- **Spec review.** Read `docs/02-SPEC.md`, `docs/04-INTENT-MANIFEST.md` and `docs/06-DECISIONS.md` and open an issue for anything that is wrong, unclear, or contradicts another doc. The most useful issues have three parts: **Where** (file and section), **Problem**, and **Suggested fix**. Issues #1 to #9 are good examples.
- **Proposed decisions.** If you think the design should change, open a pull request that adds an ADR to `docs/06-DECISIONS.md` with status **Proposed**. Say what problem it solves and what you considered instead. PR #10 is an example.
- **The Intent Manifest format.** If you would write manifests from another tool, say so in an issue. A second writer is what the format needs most.

## What does not help yet

- Pull requests that build MVP tasks out of order, or that nobody asked for. Ask in an issue first.
- Features from Phases 2 to 4 (browser capture, semantic search, sync, teams). They are planned after the decision gate.
- Changes to the fixed stack in `CLAUDE.md`, or splitting the two packages further.

## House rules

- Prose in docs and UI copy is plain and direct, with no em-dashes.
- COAL is a record of work, not a memory. Do not describe it as memory in UI copy or tool descriptions (ADR-008).
- `spec/intent-manifest.schema.json` is generated from the Zod schema once that exists (T02). Change the Zod source, not the JSON.
- Never delete user data, never bind to anything but `127.0.0.1`, and never add telemetry or network calls the user did not configure.

## Using AI tools

This project is built with Claude Code, under the maintainer's direction and review. AI-assisted issues and pull requests are welcome when a person has read and checked them. Say that a tool was used; a footer line is enough. Large batches of unchecked generated changes will be closed.

If your agent reads `CLAUDE.md`, note the section for outside contributors at the top: work only on the issue you were asked to take.

## License of contributions

COAL is licensed under Apache 2.0. By submitting a contribution you agree it is licensed under the same terms (Apache 2.0, section 5). There is no CLA.

## Security

Do not report security problems in public issues. Use GitHub's private vulnerability reporting on this repository (Security tab, "Report a vulnerability").
