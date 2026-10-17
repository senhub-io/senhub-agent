#!/usr/bin/env bash
# Single version source: the VERSION file. This script checks that every pin
# listed in scripts/version-pins.tsv agrees with it, or rewrites them all.
#
#   check-version-consistency.sh                 report, exit 1 on any mismatch
#   check-version-consistency.sh --set X.Y.Z     rewrite VERSION and every pin
#   --root DIR                                   operate on another tree
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
new=""
while [ $# -gt 0 ]; do
  case "$1" in
    --root) root=$(cd "$2" && pwd); shift 2 ;;
    --set) new="${2:-}"; shift 2 ;;
    *) echo "usage: $0 [--root DIR] [--set X.Y.Z]" >&2; exit 2 ;;
  esac
done

semver='^[0-9]+\.[0-9]+\.[0-9]+$'
generic='[0-9]+\.[0-9]+\.[0-9]+'
manifest="$root/scripts/version-pins.tsv"

if [ -n "$new" ]; then
  if ! printf '%s' "$new" | grep -Eq "$semver"; then
    echo "set-version: '$new' is not X.Y.Z (a prerelease suffix belongs to the tag, not to VERSION)" >&2
    exit 2
  fi
  printf '%s\n' "$new" > "$root/VERSION"
fi

version=$(tr -d ' \n\r' < "$root/VERSION")
if ! printf '%s' "$version" | grep -Eq "$semver"; then
  echo "VERSION '$version' is not X.Y.Z" >&2
  exit 2
fi
escaped=${version//./\\.}

bad=0
while IFS=$'\t' read -r file pattern; do
  case "$file" in ''|'#'*) continue ;; esac
  target="$root/$file"
  if [ ! -f "$target" ]; then
    echo "MISSING $file (listed in version-pins.tsv)" >&2
    bad=1
    continue
  fi
  prefix=${pattern%%@V@*}
  suffix=${pattern#*@V@}
  any="${prefix}${generic}${suffix}"
  exact="${prefix}${escaped}${suffix}"

  hits=$(grep -nE -- "$any" "$target" || true)
  if [ -z "$hits" ]; then
    echo "NOPIN   $file: no line matches /$pattern/ (pattern out of date?)" >&2
    bad=1
    continue
  fi

  if [ -n "$new" ]; then
    # \x01 as delimiter: patterns contain /, | and #.
    d=$'\001'
    sed -E -i.bak "s${d}(${prefix})${generic}(${suffix})${d}\\1${version}\\2${d}g" "$target"
    rm -f "$target.bak"
    continue
  fi

  while IFS= read -r line; do
    text=${line#*:}
    if ! printf '%s\n' "$text" | grep -Eq -- "$exact"; then
      echo "MISMATCH $file:${line%%:*} wants $version: ${text}" >&2
      bad=1
    fi
  done <<< "$hits"
done < "$manifest"

if [ -n "$new" ]; then
  exec "$0" --root "$root"
fi
if [ "$bad" -ne 0 ]; then
  echo "version consistency: FAILED against VERSION=$version" >&2
  exit 1
fi
echo "version consistency: OK ($version)"
