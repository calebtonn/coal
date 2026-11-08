# Contributing to COAL

Thanks for looking. COAL is being built in the open, and this page is about the kind of help that is useful right now.

## Where the project is

COAL is pre-alpha. There is no release and no install path; `coal` is built from source with the Makefile (`CLAUDE.md` lists the commands). `main` holds the spec, the decisions behind it, an ordered build plan (`docs/05-MVP-TASKS.md`), and the Go implementation, which ADR-018 adopted from Dave Nicoll's proof of concept on the `poc/go` branch. It builds from a fresh clone with Go 1.27, runs the demo from a recorded Claude Code session, and has been run against one live Claude Code session so far (macOS, 2026-11-07): capture, state recorded by the model over MCP, the resume offer in a plain new session, and `coal resume` all worked. The web UI does not build outside the maintainers' machines yet, because `web/` depends on a package that is not published; the binary builds without it and serves a placeholder page. The nine known bugs, including three Go test packages that fail on macOS, are open issues #20 to #28, one issue each; each gets its own pull request to `main`. The rest of the MVP is built in the plan's order, one task at a time. At the end of the MVP there is a decision gate (task T14): after a week of real use, the project continues to Phase 2, changes direction, or stops. The bar is in the README: if COAL does not get used unprompted at least five times that week, the project stops. If it stops, the repo is archived and everything in it stays available under Apache 2.0.

## What helps now

- Read `docs/02-SPEC.md`, `docs/04-INTENT-MANIFEST.md` and `docs/06-DECISIONS.md`, and open an issue for anything that is wrong, unclear, or contradicts another doc. Say where (file and section), what the problem is, and what you would change. Issues #1 to #9 are good examples.
- Look through `cmd/` and `internal/` and open an issue where the code disagrees with the spec, or where it does something the docs do not say.
- If you think the design should change, open a pull request that adds an ADR to `docs/06-DECISIONS.md` with status Proposed. Say what problem it solves and what you considered instead. PR #10 is an example.
- If you would write manifests from another tool, say so in an issue before you build anything. A second writer is what the format needs most.

## What does not help yet

- Pull requests that build MVP tasks out of order, or that nobody asked for. Ask in an issue first.
- Code for Phases 2 to 4 (session import, adapters for other tools, browser capture, semantic search, sync, teams). It starts after the decision gate. `coal import claude-code` already exists as a spike; it stays as it is until the gate.
- Changes to the fixed stack in `CLAUDE.md` (ADR-018), or splitting the one binary into more modules or packages than `CLAUDE.md` lists.

## House rules

- Prose in docs and UI copy is plain and direct, with no em-dashes.
- COAL is a record of work. Do not call it memory in UI copy, tool descriptions or docs (ADR-008). `docs/01-THESIS.md` is the one exception: it uses the word to place COAL among memory systems.
- `spec/intent-manifest.schema.json` is the source of truth for the manifest. The web validator is generated from it with `bun run gen:validator` in `web/`, and a test fails if the two drift. Change the schema, then regenerate. When documents disagree, accepted ADRs come first and the schema second (docs/07-DRIFT.md has the full order).
- Never delete user data, never bind to anything but `127.0.0.1`, and never add telemetry or network calls the user did not configure.

Before you open a pull request, run `make GO=/path/to/go check` and fill in the template in `.github/pull_request_template.md`. `docs/07-DRIFT.md` says what the check enforces, which document wins when two disagree, and how a change to the vision is made on purpose.

## Using AI tools

COAL is built with Claude Code. A person directs the work and reviews everything it produces. Issues and pull requests written with AI tools are fine if you have read and checked them yourself. Say a tool was used; a footer line is enough. Large batches of unchecked generated changes will be closed.

`CLAUDE.md` tells agents to work only on the issue you point them at.

## License of contributions

COAL is licensed under Apache 2.0. By submitting a contribution you agree it is licensed under the same terms (Apache 2.0, section 5). There is no CLA.

## Security

Do not report security problems in public issues. Use GitHub's private vulnerability reporting on this repository (Security tab, "Report a vulnerability").
