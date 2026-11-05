#!/usr/bin/env bash
# Assemble the coalmine.io site into _site/ (or the directory given as $1).
# The schema is copied byte for byte so it is served at its $id (T12b).
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
out="${1:-$root/_site}"
if [ -e "$out" ] && [ ! -d "$out/schema/intent-manifest" ]; then
  echo "refusing to delete $out: it does not look like a built site" >&2
  exit 1
fi
# The schema is published at its $id. A new version gets its own path; 0.1.json stays frozen.
python3 -I -c 'import json,sys; i=json.load(open(sys.argv[1]))["$id"]; sys.exit(0 if i=="https://coalmine.io/schema/intent-manifest/0.1.json" else "schema $id is "+i+"; publish it at its own path and keep 0.1.json frozen")' "$root/spec/intent-manifest.schema.json"
rm -rf "$out"
mkdir -p "$out/assets" "$out/schema/intent-manifest"
cp "$root/site/index.html" "$root/site/404.html" "$out/"
for size in 32 64 128 256 512; do cp "$root/assets/icon-$size.png" "$out/assets/"; done
cp "$root/spec/intent-manifest.schema.json" "$out/schema/intent-manifest/0.1.json"
echo "Built site in $out"
