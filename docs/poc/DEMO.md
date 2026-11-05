# COAL POC demo

## What the demo shows

COAL keeps a record of what you made with AI tools, why, and what is still open. The demo walks the five steps of the definition of done in `CLAUDE.md`, using the issue fixes as amended in `docs/02-SPEC.md` F5:

1. A Claude Code session is captured by hooks. A manifest appears with your intent in your own words, the HTML you built as an artifact, and a next step.
2. A different tool, connected over MCP, finds it with `coal_find` and reads the HTML.
3. A new session in the same project shows "You left off at ..." before you type, and the model is told to offer to continue.
4. `coal find budget` on the command line returns the same manifest.
5. The web UI at `http://127.0.0.1:4242` shows the manifest with a preview of the HTML.

Everything runs locally inside the repo's `.sandbox/` directory. Nothing touches `~/.coal/` or `~/.claude/`.

## Run it

```
scripts/demo.sh        # or: make demo
```

The script builds the web UI and the binary (set `SKIP_BUILD=1` to skip that), then creates a fresh `.sandbox/demo-<timestamp>/` holding a data dir `coal/` and a project `projects/budget`. It prints a numbered heading, details and a PASS or FAIL line for each step. At the end it prints hook timings, the `coal stats` counters and a summary. It exits non-zero if any step fails.

The capture step replays recorded Claude Code hook payloads from `testdata/claude-code/` through the real `coal hook` commands. It does not start Claude Code. A live run is described below and is optional.

## What each step proves

1. **Capture.** `scripts/replay-session.sh` feeds SessionStart, UserPromptSubmit, two PostToolUse writes, Stop and SessionEnd to `coal hook`. The check passes when there is exactly one manifest, and it has these properties. `intent.summary` is the user's first prompt with `source: user`. `budget.html` is registered as a `text/html` artifact. The project has a `.coal` marker naming the manifest, and the marker is listed in `.git/info/exclude`. The next step "add CSV export" comes from the last assistant message, because no model recorded one. This is definition-of-done step 1, with SessionEnd instead of Stop (#1).
2. **Another tool finds it.** The script starts `coal mcp`, sends `initialize`, calls `coal_find` with `budget` the way a model would phrase it, then calls `coal_read_artifact` on the result. It passes when the manifest comes back and the artifact text contains "Household budget". This is definition-of-done step 2.
3. **Resume.** A new session starts in the project. `coal hook session-start` prints JSON whose `systemMessage` says `COAL: You left off at "add CSV export".` and whose `additionalContext` carries the intent, where it stands, the next step, the artifacts and the manifest id. Claude Code shows the `systemMessage` in the terminal before you type, and the model reads the context before its first reply. This is definition-of-done step 3 as rewritten in #3: a hook cannot produce an assistant turn before the user types, so the terminal line is the deterministic part and the model's offer to continue is the other.
4. **Command line.** `coal find budget` lists the manifest first. This is definition-of-done step 4.
5. **Web UI.** `coal serve` runs on a free port on 127.0.0.1. The check passes when `/health` is ok, `/api/manifests?q=budget` returns the manifest, `/` serves the built UI with a Content-Security-Policy, and the artifact URL returns `text/html` with `Content-Security-Policy: sandbox`, so the HTML preview cannot run scripts against the UI. The server is stopped by its PID when the step ends. This is definition-of-done step 5.

## Try it by hand

From the repo root:

```
make web build
export COAL_HOME=$PWD/.sandbox/coal
alias coal=$PWD/.sandbox/bin/coal

coal init
coal add --intent "Household budget tracker as a single HTML page" --artifact README.md
coal find budget
coal show <id>              # an id prefix of 8 or more characters works
coal open <id>              # opens a copy of the first artifact
coal serve                  # then open http://127.0.0.1:4242
```

To replay the recorded Claude Code session into the same data dir and see the resume JSON:

```
scripts/replay-session.sh
```

To connect an MCP client such as Claude Desktop or Cursor, point it at the sandbox binary and data dir. Replace `/path/to/coal` with the absolute path of this repo:

```json
{
  "mcpServers": {
    "coal": {
      "command": "/path/to/coal/.sandbox/bin/coal",
      "args": ["mcp"],
      "env": { "COAL_HOME": "/path/to/coal/.sandbox/coal" }
    }
  }
}
```

Then ask the tool to "find the budget thing I built". It should call `coal_find` and can read the HTML with `coal_read_artifact`. `scripts/mcp-smoke.sh` does the same over stdio without a client.

The scripts refuse a `COAL_HOME` or `PROJECT` outside `.sandbox/` unless `COAL_ALLOW_OUTSIDE_SANDBOX=1` is set.

## Optional: a live Claude Code run in a sandbox

This step uses Dave's Claude Code account, so it is left for him to run. A fresh `CLAUDE_CONFIG_DIR` has no login, so Claude Code asks you to sign in the first time. That is the reason it is not part of `scripts/demo.sh`.

```
make build
export COAL_HOME=$PWD/.sandbox/coal
mkdir -p .sandbox/claude-config .sandbox/projects/live-budget
.sandbox/bin/coal adapter install claude-code --settings .sandbox/claude-config/settings.json
cd .sandbox/projects/live-budget && git init -q
CLAUDE_CONFIG_DIR=$PWD/../../claude-config claude
```

`coal adapter install` writes the five hooks into that settings file only, with `COAL_HOME` baked into each command. The `--settings` flag is required, so it can never edit `~/.claude/`.

In the session:

1. Ask: "Build me a household budget tracker as a single HTML page." Let Claude write the file. If it ends with a line like "Next step: add CSV export", that becomes the next step. Otherwise ask it to record the next step with `coal_update_state` if you have connected the MCP server too.
2. Exit with `/exit`.
3. Look in `.sandbox/coal/manifests/`: there should be one manifest with your prompt as the intent and the HTML as an artifact. `.coal` should sit in the project directory.
4. Start Claude Code again in the same directory with the same command. Before you type, the terminal should show a line starting `COAL: You left off at`. Send any message, and the first reply should offer to continue from that step.

To remove the hooks: `.sandbox/bin/coal adapter uninstall claude-code --settings .sandbox/claude-config/settings.json`. A copy of the previous settings is kept next to the file as `settings.json.bak`.

## Not in the POC yet

- Import from chat apps such as claude.ai and ChatGPT. Only Claude Code transcripts on disk are covered (M10).
- Cursor and Codex adapters. The capture contract (ADR-011) is ready for them, but only Claude Code is wired.
- A model summarizer over the transcript (#2). Summaries come from the user's first prompt, and state comes from the model's `coal_update_state` calls or from the last assistant message.
