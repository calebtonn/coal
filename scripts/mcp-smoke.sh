#!/usr/bin/env bash
# Pipe initialize, tools/list and a coal_find call into `coal mcp` over
# stdio against the sandbox data dir, print the responses, and fail if
# coal_find returns no result. Builds nothing; run `make build` first.
#
#   COAL_HOME  data dir, default .sandbox/coal; must be inside .sandbox/
#              unless COAL_ALLOW_OUTSIDE_SANDBOX=1
set -euo pipefail
. "$(dirname "$0")/sandbox-guard.sh"
cd "$repo"
export COAL_HOME="${COAL_HOME:-$sandbox/coal}"
in_sandbox_or_die COAL_HOME "$COAL_HOME"
bin=.sandbox/bin/coal
[ -x "$bin" ] || { echo "missing $bin; run make build" >&2; exit 1; }

coproc MCP { timeout 10 "$bin" mcp; }
cat >&"${MCP[1]}" <<'JSON'
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"mcp-smoke","version":"1"}}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"coal_find","arguments":{"query":"budget"}}}
JSON
# Read until the coal_find response arrives, then close stdin so the
# server exits. The timeout above bounds a hung server.
out=""
while IFS= read -r line <&"${MCP[0]}"; do
  out+="$line"$'\n'
  case "$line" in *'"id":3'*) break ;; esac
done
exec {MCP[1]}>&-
wait "$MCP_PID" 2>/dev/null || true
printf '%s' "$out"

find_line=$(printf '%s' "$out" | grep '"id":3' || true)
if [ -z "$find_line" ] || printf '%s' "$find_line" | grep -q '"isError":true' || printf '%s' "$find_line" | grep -q '"results":\[\]'; then
  echo "mcp-smoke: coal_find returned no result" >&2
  exit 1
fi
echo "mcp-smoke: ok"
