#!/usr/bin/env bash
# Runs set-version on a temporary copy of the tracked files and verifies
# that every pin follows, that nothing outside the pin list changed, and
# that a malformed version is refused.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

(cd "$root" && git ls-files -co --exclude-standard -z | tar -cf - --null -T -) | tar -xf - -C "$work"

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
for args in "--set" "--set ''" "--root"; do
  if eval "\"\$work/scripts/check-version-consistency.sh\" $args" >/dev/null 2>&1; then
    echo "FAIL: check-version-consistency.sh $args accepted" >&2
    exit 1
  fi
done

# A pin with nothing after the version must not accept a longer version.
open_pin=$(grep -v '^#' "$root/scripts/version-pins.tsv" | awk -F'\t' '$2 ~ /@V@$/ {print $1; exit}')
if [ -n "$open_pin" ]; then
  sed -i.bak "s/${target//./\\.}/${target}0/" "$work/$open_pin" && rm -f "$work/$open_pin.bak"
  if "$work/scripts/check-version-consistency.sh" --root "$work" >/dev/null 2>&1; then
    echo "FAIL: $open_pin pinned at ${target}0 passed for $target" >&2
    exit 1
  fi
fi
echo "set-version: OK"
