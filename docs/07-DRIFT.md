# Keeping the docs, the site and the code in line

COAL is described in several places: the ADRs, the schema, the spec, the site, the README, the task list, and the code itself. They drift apart when one is changed and the others are not. In the week before this document was written, every doc and the site described a TypeScript product while the code was Go, the schema on the site lacked fields the code wrote, and three Go test packages failed on macOS without anyone knowing. This document says which source wins when two disagree, what `make check` enforces, how the drift review works, and how to change the vision on purpose and bring everything else along.

## Sources of truth, in order

When two documents disagree, the one higher on this list is right and the other is a bug:

1. Accepted ADRs in `docs/06-DECISIONS.md`. A decision lives here or it is not a decision.
2. `spec/intent-manifest.schema.json`, for the manifest's fields, its `$id` and the path `site/build.sh` publishes it at.
3. `docs/02-SPEC.md` and `docs/04-INTENT-MANIFEST.md`, the functional spec and the standard.
4. `site/index.html`, the vision as written for people who do not read the repo.
5. `README.md` and `docs/05-MVP-TASKS.md`, the status and the build order.
6. The code.

The order answers what is intended. The code, `coal help`, `go.mod` and the schema file answer what exists today. Drift is a disagreement between the two, and the review classifies it: either the code is behind the intent, or a document is behind the code, or the intent itself has moved and nobody wrote it down.

Code that disagrees with an accepted ADR is a bug, and it gets an issue. An ADR that nobody wants any more is changed by a new ADR, never by editing the code or the site first.

## Two ways to change a decision

- A new ADR that says `Supersedes ADR-NNN` or `Amends ADR-NNN` in its Status line, when a decision is reversed or its scope changes. ADR-018 superseded ADR-005 this way, and ADR-011 amended ADR-003.
- A dated note in the existing ADR's Status line, when the implementation fills in a detail the decision left open. ADR-013 and ADR-014 took this path on 2026-11-07, when the marker shape and the history mechanism were filled in from the implementation.

Either way the site and `docs/02-SPEC.md` change in the same PR as the acceptance, never before it.

## What `make check` enforces

`make GO=/path/to/go check` runs after `vet` and `build`:

- `gofmt -l` over `cmd`, `internal` and `spec` must print nothing.
- `go vet` over the module.
- The drift tests in `internal/drift`, with `-v` so known failures are visible in the output.
- The site build: `site/build.sh` into a temporary directory, then `cmp` of the published schema against `spec/intent-manifest.schema.json`. This needs bash and python3.

The drift tests read the repo and fail when a document and the code disagree. Each is a named subtest with a stable id: `commands` (every `coal` subcommand named in the docs and on the site exists in the CLI), `deps` (the direct dependencies in `go.mod` are the ones ADR-018 allows), `schema-published` (the schema's `$id` matches the path the site publishes it at, and the marker schema URL is published), `examples` (every file in `spec/examples` validates), `adr-code-locations` (every accepted ADR names where it lives in the code, and every path exists), `house-rules` (no em-dashes or en-dashes, and no forbidden words outside the allowlist), `stack-words` (no words from the retired stack in the documents that describe the current one), `phases` (the phases on the site are a prefix of the phases in the README), `site-facts` (the manifest excerpt on the site uses only fields the schema has), `allow` (every `allow.txt` entry still matches a line in its file) and `known` (every `known.txt` line names a real check id and an issue number). The package itself is the reference for what each one checks.

CI (`.github/workflows/ci.yml`) runs vet, build, the tests and the demo without the UI step on Ubuntu and macOS, and `make check` on Ubuntu. On macOS the test packages with known failures (#22, #23, #24, #31) run in a separate step that may fail; the PR that closes each issue removes its package from that step.

## How to read a failure

Every failure message names the file and line and says what to change. A failure in `commands` means a document names a command that does not exist: fix the document, or add the command if the ADR or spec calls for it. A failure in `deps` means a new direct dependency: it needs an ADR that supersedes ADR-018, not a PR that adds it. A failure in `adr-code-locations` means an ADR was accepted without saying where it lives, or a path it names has moved: update the paragraph. A failure in `house-rules` or `stack-words` is a sentence to rewrite, or, when the use is legitimate, an entry in `allow.txt`. A failure in `phases` or `site-facts` means the site and the README or the schema disagree: the higher source on the list wins. Below a floor (for example, no phases found on the site), the message points at the selector in the test to update.

Two text files beside the tests carry the exceptions:

- `internal/drift/known.txt` lists checks that fail today because of an open issue. Each line is tab-separated: the check id, the issue number as `#N`, and a note. A listed check that fails is logged as a known failure and does not fail the test. A listed check that passes fails the test with a message to remove the line. The PR that closes the issue removes the line, and the PR template asks for it. Today it holds `schema-published/marker` for #27.
- `internal/drift/allow.txt` lists the places where a forbidden word is allowed. Each line is tab-separated: a path relative to the repo root and the exact substring that is allowed; a path with no substring allows the whole file. An entry whose substring matches nothing in its file fails the test, so an allowance goes away with the text it covered. `docs/01-THESIS.md` is allowed wholesale, because it has to name the systems COAL is not (ADR-008).

Two checks are enforced at runtime rather than in `internal/drift`: `internal/mcpserver/server_test.go` fails if a tool description or the server instructions call COAL a "memory", and `internal/capture/capture_test.go` fails if hook output does. ADR-008 is the rule.

## The drift review

`make check` catches what a program can check. It does not notice that the site promises a feature the code does not have, that a task in `docs/05-MVP-TASKS.md` describes a design an ADR has since changed, or that a sentence in `docs/02-SPEC.md` is true of the old stack and false of the new one. That is the drift review: a semantic pass an agent runs by following `.claude/skills/drift-review/SKILL.md`. In Claude Code at the repo root it is `/drift-review`, or `/drift-review 019` for one ADR.

The review reads `site/index.html`, `README.md`, `CONTRIBUTING.md`, `docs/01` to `docs/07`, `CLAUDE.md`, then `cmd/coal`, `internal/*`, `spec/` and `scripts/demo.sh`. It writes `docs/drift/YYYY-MM-DD.md` with one row per claim: the claim, where it is made (file and line), what the code or another document says, a classification, and a one-line verdict. The three classifications are: code drifted (open an issue), docs drifted (fix in a docs PR), vision changed (needs an ADR). The review verifies every row against the code before listing it, fixes nothing, and ends by printing the issues to open. It opens none.

The review runs on these events, not on a calendar: before the gate week (T14 in `docs/05-MVP-TASKS.md`), in the PR that accepts an ADR, and at each phase retro.

Reports in `docs/drift/` quote the text they flag, so the drift checks skip that folder; `docs/drift/README.md` is still checked. The maintainer commits reports. A contributor who runs the review files the issues from the rows and does not open a PR for the report.

## Changing the vision on purpose

When the intent itself moves, this is the procedure. Each step is its own PR unless it says otherwise.

(a) A PR that adds the ADR to `docs/06-DECISIONS.md` with status Proposed and changes nothing else. For anyone but the maintainer, this is the whole PR.

(b) The PR that flips the Status to Accepted also updates `site/index.html`, `docs/02-SPEC.md`, and every document the ADR's Consequences name. The site and the spec change only on acceptance.

(c) Run the drift review with the ADR number. It produces the snap list, `docs/drift/YYYY-MM-DD-adr-NNN.md`: every paragraph, task, test and code path that still describes the old vision.

(d) One issue per code change on the snap list, one PR per issue.

(e) `make check` green and a drift review with no rows for that ADR means the snap is done.
