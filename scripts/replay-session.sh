#!/usr/bin/env bash
# Replay the recorded Claude Code session through `coal hook`, all inside
# .sandbox/. Then search for it and start a second session that prints
# the resume JSON. Nothing outside the repo's .sandbox/ is touched.
set -euo pipefail

repo=$(cd "$(dirname "$0")/.." && pwd)
sandbox="$repo/.sandbox"
coal="$sandbox/bin/coal"
project="$sandbox/projects/budget"
payloads="$repo/testdata/claude-code"
export COAL_HOME="$sandbox/coal"

[ -x "$coal" ] || { echo "build first: make build" >&2; exit 1; }

# A fresh project for each run. Removal stays inside .sandbox/.
case "$project" in "$sandbox"/*) rm -rf "$project" ;; *) exit 1 ;; esac
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
	printf '%-20s %4d ms\n' "$1" $(((end - start) / 1000000)) >&2
}

echo "== replaying session 1" >&2
send session-start session-start.json
send user-prompt-submit user-prompt-submit.json
send post-tool-use post-tool-use-write.json
send post-tool-use post-tool-use-edit.json
send stop stop.json
send session-end session-end.json

echo "== coal find budget" >&2
"$coal" find budget

echo "== session 2 starts in the same project" >&2
send session-start session-start-resume.json
echo
