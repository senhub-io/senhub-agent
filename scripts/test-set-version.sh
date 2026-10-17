#!/usr/bin/env bash
# Runs set-version on a temporary copy of the tracked files and verifies
# that every pin follows, that nothing outside the pin list changed, and
# that a malformed version is refused.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

(cd "$root" && git ls-files -co --exclude-standard -z | xargs -0 tar -cf -) | tar -xf - -C "$work"

target=9.8.7
"$work/scripts/check-version-consistency.sh" --root "$work" --set "$target" >/dev/null

[ "$(tr -d '\n' < "$work/VERSION")" = "$target" ] || { echo "FAIL: VERSION not rewritten" >&2; exit 1; }
"$work/scripts/check-version-consistency.sh" --root "$work" >/dev/null \
  || { echo "FAIL: pins disagree after set-version" >&2; exit 1; }

changed=$(cd "$root" && git ls-files -co --exclude-standard | while read -r f; do cmp -s "$f" "$work/$f" || echo "$f"; done | sort)
expected=$( (echo VERSION; cut -f1 "$root/scripts/version-pins.tsv" | grep -v '^#' | grep -v '^$') | sort -u)
if [ "$changed" != "$expected" ]; then
  echo "FAIL: set-version touched the wrong files" >&2
  diff <(echo "$expected") <(echo "$changed") >&2 || true
  exit 1
fi

if "$work/scripts/check-version-consistency.sh" --root "$work" --set 1.2 >/dev/null 2>&1; then
  echo "FAIL: malformed version accepted" >&2
  exit 1
fi
echo "set-version: OK"
