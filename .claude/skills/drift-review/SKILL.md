---
name: drift-review
description: Semantic drift review for COAL. Reads the site, the docs and the code, lists every claim they disagree on, classifies each one (code drifted, docs drifted, vision changed) and writes a report to docs/drift/. Use before the gate week, in the PR that accepts an ADR, at a phase retro, or when asked to check whether the docs and the code still agree. Takes an optional ADR number, which turns the review into the snap list for that ADR.
argument-hint: "[ADR number]"
---

# Drift review

You are reviewing whether what COAL says about itself matches what it is. `make check` already catches what a program can check (see `docs/07-DRIFT.md`); this review is the part a program cannot do. You read, you compare, you classify, you write a report. You fix nothing and you open nothing.

The argument, if given, is an ADR number: `$ARGUMENTS`. With no argument, this is a general review. With one, it is the snap list for that ADR (see "Snap list" below).

## Rules

- Cite the file and line for every claim and for every piece of evidence. `path:line` form. A row without both is not a row.
- Verify against the code before listing a row. Read the function, run `.sandbox/bin/coal <cmd> --help`, open the schema. Do not list a disagreement you have not seen with your own eyes in both places.
- Never fix anything inside the review. No edits to docs, site, or code. No issues opened. The report and the printed list are the whole output.
- Reproduce quoted claims as they stand, including their mistakes. Do not paraphrase a claim into the shape that makes your point.
- Report prose follows the house rules: plain and direct, no em-dashes (U+2014) or en-dashes (U+2013), and never the word "memory" except inside a quoted claim (ADR-008). The report lives in `docs/drift/`, which the drift checks skip, so a quoted claim may contain anything; your own prose may not.
- Sources of truth are ordered. Accepted ADRs in `docs/06-DECISIONS.md` win, then `spec/intent-manifest.schema.json`, then `docs/02-SPEC.md` and `docs/04-INTENT-MANIFEST.md`, then `site/index.html`, then `README.md` and `docs/05-MVP-TASKS.md`, then the code. When two sources disagree, the lower one is the one that drifted, unless the disagreement shows the intent itself has moved.

## Procedure

1. Confirm the branch and the state of the tree with `git status` and `git log -1`. Note the commit in the report header.
2. Build the binary if it is not there: `make GO=/path/to/go build` (Go is wherever the person told you; `~/.local/go/bin/go` is the Makefile default). You need `.sandbox/bin/coal help` and `coal <cmd> --help` as evidence for every command claim.
3. Read the vision, in this order, and collect claims as you go. A claim is any sentence that says what COAL does, has, stores, requires, or will do in Phase 1. Keep a running list with `path:line` for each.
   - `site/index.html`, the vision for people who do not read the repo. Read the whole file, including the mockups and the manifest excerpt.
   - `README.md`.
   - `docs/01-THESIS.md`, `docs/02-SPEC.md`, `docs/03-ARCHITECTURE.md`, `docs/04-INTENT-MANIFEST.md`, `docs/05-MVP-TASKS.md`, `docs/06-DECISIONS.md`.
   - `CLAUDE.md`.
4. Read the code, and for each claim find what the code says.
   - `cmd/coal`: the commands, their flags and help text.
   - `internal/*`: each package's exported behavior. `go doc ./internal/<pkg>` and the tests are quick ways in.
   - `spec/intent-manifest.schema.json` and `spec/examples/`: the fields, their types, the enums, the `$id`.
   - `scripts/demo.sh`: the five definition-of-done steps, which are the executable form of the README's Phase 1 list.
5. For each claim, decide whether the sources agree. Drop the claims that agree. For every disagreement, classify it:
   - **code drifted**: a higher source (ADR, schema, spec) says one thing and the code does another. Action: open an issue.
   - **docs drifted**: the code and the higher sources agree, and a lower document (site, README, tasks, a doc paragraph, a tool description) describes something else. Action: fix in a docs PR.
   - **vision changed**: the code and the documents disagree, and the code is what the maintainer now wants, or the question is open. Action: needs an ADR. Do not guess which way it should go; say that the decision is undocumented.
6. Write the report (format below) to `docs/drift/YYYY-MM-DD.md`, with today's UTC date from `date -u +%F`. If a report with that name exists, append `-2`, `-3`, and so on.
7. Print, as the last thing you say, the list of issues to open: one line per "code drifted" row, in the form `<title> | <path:line> | violates <ADR-NNN or docs/02 section>`. Then stop. Do not open them, even if asked in the same breath; the person decides what to file.

## Report format

```
# Drift review YYYY-MM-DD

Branch `<name>` at `<short sha>`. Reviewed: site/index.html, README.md, docs/01 to 06, CLAUDE.md, cmd/coal, internal/*, spec/, scripts/demo.sh.

Rows: N. Code drifted: a. Docs drifted: b. Vision changed: c.

| # | Claim | Where | What the code or another doc says | Classification | Verdict |
|---|---|---|---|---|---|
| 1 | "<quoted as it stands>" | `path:line` | <evidence, with `path:line`> | code drifted | <one line> |
| 2 | ... | ... | ... | docs drifted | ... |

## Issues to open

- <ready-to-file title> | `<code path:line>` | violates <ADR-NNN or docs/02-SPEC.md section>
```

Rules for rows:

- Claim: the sentence as it appears in the source, in straight double quotes, trimmed to the clause that makes the claim. Reproduce it as it stands.
- Where: `path:line` of the claim.
- What the code or another doc says: the evidence, each piece with its own `path:line`. For a command claim, the `--help` output is evidence; cite `cmd/coal/<file>.go:line` where the command is defined.
- Classification: exactly one of `code drifted`, `docs drifted`, `vision changed`.
- Verdict: one line. For `code drifted` rows, the verdict carries the ready-to-file issue title, the code path with line, and the ADR or spec section violated, in the form `Issue: <title>. <path:line>. Violates <ADR-NNN or docs/02 section>.` For `docs drifted` rows, name the file to change and what it should say. For `vision changed` rows, name the ADR that would need amending or the decision that needs one.

The "Issues to open" section repeats every `code drifted` verdict as one line, so the list can be read without the table. It is also what you print at the end.

## Snap list

With an ADR number as the argument, the review narrows to that one decision:

1. Read `## ADR-NNN:` in `docs/06-DECISIONS.md` in full: Status, Context, Decision, Consequences, and the `**In the code.**` paragraph if it has one. Confirm the Status is Accepted; if it is Proposed, say so and stop, since there is nothing to snap to yet.
2. Write down what the ADR changed: the old vision it replaces or amends, in your own words, and the new one.
3. Read every source in the Procedure, but collect only claims that describe the old vision, or that the ADR's Consequences say must change. Include tasks in `docs/05-MVP-TASKS.md`, tests, help text, tool descriptions, the site, and code paths.
4. Classify each one the same way. For a snap list, most rows will be `code drifted` or `docs drifted`; a `vision changed` row here means the ADR itself is incomplete, and the verdict says what it leaves open.
5. Write the report to `docs/drift/YYYY-MM-DD-adr-NNN.md`, with the header `# Snap list for ADR-NNN, YYYY-MM-DD` and a one-paragraph summary of the ADR after the branch line. The table and the "Issues to open" section are the same as above.
6. Print the issues to open and stop.

## Before you finish

- Every row has `path:line` in both the Where and the evidence column.
- Every `code drifted` row has an issue title, a code path with line, and the ADR or spec section violated.
- `grep -n -e $'\xe2\x80\x94' -e $'\xe2\x80\x93' docs/drift/<report>.md` prints nothing (the two byte sequences are U+2014 and U+2013).
- The word "memory" appears in your own prose nowhere; only inside a quoted claim, if at all.
- You changed nothing outside `docs/drift/`, and nothing in `docs/drift/README.md`.
- The last thing you printed is the list of issues to open, or the sentence "No code drift found" when there are none.
