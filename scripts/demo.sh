#!/usr/bin/env bash
# End-to-end demo of the five definition-of-done steps, in a fresh
# .sandbox/demo-<timestamp>/ holding coal/ (the data dir) and
# projects/budget. Prints PASS or FAIL per step and exits non-zero on
# any FAIL. SKIP_BUILD=1 skips `make web build`. DEMO_SKIP_UI=1 marks the
# built-UI assertion (step 5b) SKIP instead of FAIL, for machines without
# the UI toolchain (#26); every other step still runs.
set -uo pipefail
. "$(dirname "$0")/sandbox-guard.sh"
cd "$repo"
echo "bash: $BASH_VERSION"
echo "realpath: $(command -v realpath)"
# The scripts need GNU realpath (-m). Apple's /bin/realpath lacks it, and
# the failure would otherwise surface only inside step 1's replay output.
realpath -m / >/dev/null 2>&1 || {
	echo "realpath -m is not supported by $(command -v realpath); put GNU coreutils first on PATH (on macOS: brew install coreutils and add \$(brew --prefix coreutils)/libexec/gnubin)" >&2
	exit 1
}

coal="$sandbox/bin/coal"
if [ "${SKIP_BUILD:-}" != 1 ]; then
	make -s web build >/dev/null || { echo "build failed" >&2; exit 1; }
fi
[ -x "$coal" ] || { echo "missing $coal; run make build" >&2; exit 1; }

demo="$sandbox/demo-$(date -u +%Y%m%dT%H%M%S)-$$"
export COAL_HOME="$demo/coal"
project="$demo/projects/budget"
mkdir -p "$demo"
"$coal" init >/dev/null

results=()
failed=0
step() { printf '\n== %s\n' "$1"; }
check() { # check <step label> <ok?> <detail>
	if [ "$2" = 0 ]; then
		results+=("PASS  $1")
		echo "PASS  $1"
	else
		results+=("FAIL  $1: $3")
		echo "FAIL  $1: $3"
		failed=1
	fi
}
skip() { # skip <step label> <reason>: recorded in the summary, never a failure
	results+=("SKIP  $1: $2")
	echo "SKIP  $1: $2"
}
timings="$demo/timings.txt"

step "1. Capture: replay a recorded Claude Code session through the hooks"
COAL_HOME="$COAL_HOME" PROJECT="$project" RESUME=0 scripts/replay-session.sh 2>>"$timings"
id="" why=""
shopt -s nullglob
files=("$COAL_HOME"/manifests/*.json)
if [ "${#files[@]}" != 1 ]; then
	why="expected 1 manifest, found ${#files[@]}"
else
	m="${files[0]}"
	id=$(jq -r .id "$m")
	jq -r '"  intent:  \(.intent.summary) (source \(.intent.source))",
		(.artifacts[] | "  artifact: \(.title) \(.mime) \(.versions | length) version(s)")' "$m"
	echo "  marker:  $(tr -d '\n ' <"$project/.coal")"
	queued=("$COAL_HOME"/queue/*.json)
	echo "  queued:  ${#queued[@]} event(s); SessionEnd only queues, and the next SessionStart folds it"
	if [ "$(jq -r .intent.source "$m")" != user ]; then why="intent.source is not user"
	elif ! jq -e '.artifacts[] | select(.title == "budget.html" and (.mime | startswith("text/html")))' "$m" >/dev/null; then why="no budget.html artifact"
	elif ! grep -q "$id" "$project/.coal" 2>/dev/null; then why="no .coal marker for $id"
	fi
fi
check "1. a manifest with the user's intent, the HTML artifact and the .coal marker" "$([ -z "$why" ]; echo $?)" "$why"
if [ -n "$why" ]; then
	# The replay's own output, without the timing lines, says what went wrong.
	grep -v '^hook ' "$timings" | sed 's/^/  replay: /'
fi

step "2. Another tool finds it over MCP and reads the artifact"
why=""
coproc MCP { timeout 15 "$coal" mcp 2>>"$demo/mcp.err"; }
mcp_send() { printf '%s\n' "$1" >&"${MCP[1]}"; }
mcp_wait() { # mcp_wait <id>: print the response line for that request id
	local line
	while IFS= read -r -t 10 line <&"${MCP[0]}"; do
		if [ "$(jq -r '.id // empty' <<<"$line" 2>/dev/null)" = "$1" ]; then
			printf '%s' "$line"
			return 0
		fi
	done
	return 1
}
mcp_send '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"coal-demo","version":"1"}}}'
init=$(mcp_wait 1)
mcp_send '{"jsonrpc":"2.0","method":"notifications/initialized"}'
mcp_send '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"coal_find","arguments":{"query":"budget"}}}'
found=$(mcp_wait 2)
found_id=$(jq -r '.result.structuredContent.results[0].id // empty' <<<"$found" 2>/dev/null)
echo "  initialize: $(jq -r '.result.serverInfo | "\(.name) \(.version)"' <<<"$init" 2>/dev/null)"
echo "  coal_find \"budget\": ${found_id:-nothing}"
if [ -n "$found_id" ]; then
	mcp_send "{\"jsonrpc\":\"2.0\",\"id\":3,\"method\":\"tools/call\",\"params\":{\"name\":\"coal_read_artifact\",\"arguments\":{\"id\":\"$found_id\"}}}"
	read_out=$(mcp_wait 3)
	echo "  coal_read_artifact: $(jq -r '[.result.content[]?.text // empty] | join(" ") | .[0:80]' <<<"$read_out" 2>/dev/null | tr '\n' ' ')..."
fi
exec {MCP[1]}>&-
wait "$MCP_PID" 2>/dev/null
if [ -z "$init" ]; then why="no initialize response"
elif [ "$found_id" != "$id" ]; then why="coal_find returned '${found_id}', want $id"
elif ! grep -q "Household budget" <<<"${read_out:-}"; then why="coal_read_artifact did not return the HTML"
fi
check "2. coal_find over MCP returns the manifest and coal_read_artifact returns the HTML" "$([ -z "$why" ]; echo $?)" "$why"

step "3. A new session in the project shows where the work left off"
why=""
newsid=$(python3 -I -c 'import uuid; print(uuid.uuid4())')
start=$(date +%s%N)
resume=$(sed -e "s#/PROJECT/budget#$project#g" -e "s#8d2a4f3b-6c5e-4f90-8b12-3c4d5e6f7a81#$newsid#g" \
	testdata/claude-code/session-start-resume.json | "$coal" hook session-start)
printf 'hook %-20s %4d ms (new session)\n' session-start $((($(date +%s%N) - start) / 1000000)) >>"$timings"
msg=$(jq -r .systemMessage <<<"$resume" 2>/dev/null)
ctx=$(jq -r .hookSpecificOutput.additionalContext <<<"$resume" 2>/dev/null)
echo "  systemMessage: $msg"
echo "  additionalContext:"
sed 's/^/    /' <<<"$ctx"
if ! grep -q "add CSV export" <<<"$msg"; then why="systemMessage lacks 'add CSV export'"
elif [ -z "$ctx" ] || ! grep -q "$id" <<<"$ctx"; then why="additionalContext missing or lacks the manifest id"
fi
check "3a. Plain claude: SessionStart prints the next step and gives the model the context" "$([ -z "$why" ]; echo $?)" "$why"

# (b) coal resume starts claude with the resume as the first turn. A
# stand-in claude earlier on PATH records its argv instead of starting a
# session, so the demo never touches a real Claude Code install.
why=""
fakebin="$demo/fakebin"
argv="$demo/claude-argv.txt"
mkdir -p "$fakebin"
printf '#!/bin/sh\nfor a in "$@"; do printf "%%s\\n" "$a"; done > "%s"\n' "$argv" >"$fakebin/claude"
chmod 700 "$fakebin/claude"
(cd "$project" && PATH="$fakebin:$PATH" "$coal" resume) || why="coal resume exited $?"
prompt=$(cat "$argv" 2>/dev/null)
echo "  claude was started with: $prompt"
if [ -z "$why" ] && ! grep -qi "left off at: add CSV export" <<<"$prompt"; then
	why="the first prompt lacks the next step"
fi
check "3b. coal resume starts claude with a first prompt that opens from the next step" "$([ -z "$why" ]; echo $?)" "$why"

step "4. coal find budget on the command line"
why=""
find_out=$("$coal" find budget)
printf '%s\n' "$find_out" | sed 's/^/  /'
short=$("$coal" find --json budget | jq -r '.[0].id // empty')
[ "$short" = "$id" ] || why="first result is '$short', want $id"
grep -qi "household budget tracker" <<<"$find_out" || why="${why:-the summary is not listed}"
check "4. coal find budget lists the manifest" "$([ -z "$why" ]; echo $?)" "$why"

step "5. The web UI and API on 127.0.0.1"
why=""
port=$(python3 -I -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])')
"$coal" serve --port "$port" >"$demo/serve.log" 2>&1 &
serve_pid=$!
trap 'kill "$serve_pid" 2>/dev/null; wait "$serve_pid" 2>/dev/null' EXIT
base="http://127.0.0.1:$port"
for _ in $(seq 50); do curl -fs "$base/health" >/dev/null 2>&1 && break; sleep 0.1; done
health=$(curl -fs "$base/health")
api=$(curl -fs "$base/api/manifests?q=budget")
ui_head=$(curl -fs -D - -o "$demo/index.html" "$base/")
hash=$(jq -r '.artifacts[] | select(.title == "budget.html") | .hash' "$COAL_HOME/manifests/$id.json" 2>/dev/null)
art_head=$(curl -fs -D - -o "$demo/artifact.html" "$base/api/artifacts/${hash#sha256:}")
echo "  url:      $base"
echo "  health:   $health"
echo "  api:      $(jq -r '[.results[].id] | join(", ")' <<<"$api" 2>/dev/null)"
echo "  ui:       $(grep -i '^content-security-policy' <<<"$ui_head" | cut -c1-90 | tr -d '\r')..."
echo "  artifact: $(grep -iE '^(content-type|content-security-policy)' <<<"$art_head" | tr -d '\r' | paste -sd ' ')"
if [ "$(jq -r .ok <<<"$health" 2>/dev/null)" != true ]; then why="/health is not ok"
elif ! jq -e --arg id "$id" '.results[] | select(.id == $id)' <<<"$api" >/dev/null 2>&1; then why="/api/manifests?q=budget lacks $id"
elif ! grep -qi '^content-security-policy:' <<<"$ui_head"; then why="/ has no CSP header"
elif ! grep -qi '^content-type: text/html' <<<"$art_head" || ! grep -qi '^content-security-policy: sandbox' <<<"$art_head"; then why="artifact headers are wrong"
elif ! grep -q "Household budget" "$demo/artifact.html"; then why="artifact body is not the HTML"
fi
kill "$serve_pid" 2>/dev/null
wait "$serve_pid" 2>/dev/null
trap - EXIT
check "5a. the daemon serves the search API and the sandboxed HTML preview" "$([ -z "$why" ]; echo $?)" "$why"

# (b) The built UI. Building it needs Bun and the unpublished
# @davenicoll/web-framework (#26), so DEMO_SKIP_UI=1 records a SKIP here.
if [ "${DEMO_SKIP_UI:-}" = 1 ]; then
	skip "5b. the UI is built" "not built in this run (#26)"
else
	why=""
	grep -q 'data-wf-theme-init' "$demo/index.html" 2>/dev/null || why="/ is not the built UI"
	check "5b. the UI is built and served at /" "$([ -z "$why" ]; echo $?)" "$why"
fi

step "Hook timings"
grep '^hook ' "$timings" | sed 's/^/  /'
step "coal stats"
"$coal" stats --days 0 2>&1 | sed 's/^/  /'

step "Summary ($demo)"
printf '%s\n' "${results[@]}"
exit "$failed"
