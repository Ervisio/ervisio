#!/bin/bash
# shot.sh <round> <variant> <out.png> [width] [height]: screenshot a round variant as the gallery renders it (dark).
R=$1; V=$2; OUT=$3; W=${4:-1440}; H=${5:-900}
D=$(cd "$(dirname "$0")/.." && pwd)/rounds/$R
TMP=$(mktemp -d)
cp "$D"/* "$TMP"/
{ echo '<!doctype html><html lang="en" data-theme="dark" class="dark" style="color-scheme:dark"><head><meta charset="utf-8">'
  echo '<style>'; cat ~/.claude/skills/variant-studio/scripts/ui/base.css; echo '</style>'
  [ -f "$D/_shared.css" ] && { echo '<style>'; cat "$D/_shared.css"; echo '</style>'; }
  echo '</head><body>'; cat "$D/$V.html"; echo '</body></html>'; } > "$TMP/__shot.html"
CH=$(ls -d ~/.cache/ms-playwright/chromium-*/chrome-linux*/chrome | head -1)
"$CH" --headless=new --no-sandbox --hide-scrollbars --window-size=$W,$H --virtual-time-budget=5000 --screenshot="$OUT" "file://$TMP/__shot.html" >/dev/null 2>&1
rm -rf "$TMP"
