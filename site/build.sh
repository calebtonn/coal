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
cp "$root/site/404.html" "$out/"
# The GitHub button shows the star count when STARS is set (the Pages workflow
# fetches it at build time, so visitors' browsers never call GitHub).
python3 -I - "$root/site/index.html" "$out/index.html" "${STARS:-}" <<'PY'
import sys
src, dst, stars = sys.argv[1], sys.argv[2], sys.argv[3].strip()
html = open(src, encoding="utf-8").read()
badge = ""
if stars.isascii() and stars.isdigit() and int(stars) > 0:
    n = int(stars)
    k = round(n / 1000, 1)
    if k >= 1000:
        label = f"{n / 1_000_000:.1f}M".replace(".0M", "M")
    elif n >= 1000:
        label = f"{k:.1f}k".replace(".0k", "k")
    else:
        label = str(n)
    noun = "star" if n == 1 else "stars"
    badge = f'<span class="stars"><svg aria-hidden="true"><use href="#i-star"/></svg><span aria-hidden="true">{label}</span><span class="sr-only">, {n} {noun}</span></span>'
open(dst, "w", encoding="utf-8").write(html.replace("<!--STARS-->", badge))
PY
for size in 32 64 128 256 512; do cp "$root/assets/icon-$size.png" "$out/assets/"; done
cp "$root/spec/intent-manifest.schema.json" "$out/schema/intent-manifest/0.1.json"
echo "Built site in $out"
