#!/usr/bin/env bash
# Replay the recorded Claude Code session through `coal hook`. Then
# search for it and start a second session that prints the resume JSON.
#
#   COAL_HOME  data dir, default .sandbox/coal
#   PROJECT    project dir, default .sandbox/projects/budget; recreated
#              on each run when it is inside .sandbox/
#   RESUME=0   stop after the first session (no find, no second start)
#
# Both paths must be inside .sandbox/ unless COAL_ALLOW_OUTSIDE_SANDBOX=1.
# Hook timings go to stderr.
set -euo pipefail
. "$(dirname "$0")/sandbox-guard.sh"

coal="$sandbox/bin/coal"
payloads="$repo/testdata/claude-code"
export COAL_HOME="${COAL_HOME:-$sandbox/coal}"
project="${PROJECT:-$sandbox/projects/budget}"
in_sandbox_or_die COAL_HOME "$COAL_HOME"
in_sandbox_or_die PROJECT "$project"
project=$(realpath -m -- "$project")

[ -x "$coal" ] || { echo "build first: make build" >&2; exit 1; }

# A fresh project for each run. Deletion only ever happens inside
# .sandbox/; outside it, the directory must be new or empty.
if inside_sandbox "$project"; then
	rm -rf -- "$project"
elif [ -n "$(ls -A -- "$project" 2>/dev/null)" ]; then
	echo "refusing to reuse $project: it is outside .sandbox/ and not empty" >&2
	exit 2
fi
mkdir -p "$project"
git -C "$project" init -q
cat > "$project/budget.html" <<'HTML'
<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>Household budget</title></head>
<body>
<h1>Household budget</h1>
<table id="rows"><tr><th>Item</th><th>Amount</th></tr></table>
<p>Total: <span id="total">0</span></p>
<button id="add">Add row</button>
</body>
</html>
HTML

send() { # send <hook> <payload file>
	local start end
	start=$(date +%s%N)
	sed "s#/PROJECT/budget#$project#g" "$payloads/$2" | "$coal" hook "$1"
	end=$(date +%s%N)
	printf 'hook %-20s %4d ms\n' "$1" $(((end - start) / 1000000)) >&2
}

echo "== replaying session 1 in $project" >&2
send session-start session-start.json
send user-prompt-submit user-prompt-submit.json
send post-tool-use post-tool-use-write.json
send post-tool-use post-tool-use-edit.json
send stop stop.json
send session-end session-end.json

[ "${RESUME:-1}" = 0 ] && exit 0

echo "== coal find budget" >&2
"$coal" find budget

echo "== session 2 starts in the same project" >&2
send session-start session-start-resume.json
echo
