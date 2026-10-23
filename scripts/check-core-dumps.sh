#!/usr/bin/env bash
# Refuses ELF core dumps in the tree. A test child killed by a signal in a
# container with core dumps enabled writes one into the package directory,
# and `git add -A` then commits it (92a58de08, 741e15f74). The check reads
# the ELF header (e_type == ET_CORE), so it does not depend on the name.
#
#   check-core-dumps.sh           every tracked file (CI)
#   check-core-dumps.sh --staged  the files staged for commit (pre-commit)
set -euo pipefail

mode=${1:-}

# header prints the first 18 bytes of a path, from the index or the tree.
header() {
  if [ "$mode" = --staged ]; then
    git show ":$1" 2>/dev/null | head -c 18 | od -An -tx1 | tr -d ' \n'
  else
    head -c 18 "$1" 2>/dev/null | od -An -tx1 | tr -d ' \n'
  fi
}

is_core() {
  local h
  h=$(header "$1")
  [ "${h:0:8}" = 7f454c46 ] || return 1
  # EI_DATA (byte 5) gives the byte order of e_type (bytes 16-17).
  case "${h:10:2}" in
    01) [ "${h:32:4}" = 0400 ] ;;
    02) [ "${h:32:4}" = 0004 ] ;;
    *) return 1 ;;
  esac
}

if [ "$mode" = --staged ]; then
  list() { git diff --cached --name-only -z --diff-filter=ACMR; }
else
  list() { git ls-files -z; }
fi

found=0
while IFS= read -r -d '' path; do
  if is_core "$path"; then
    echo "core dump: $path" >&2
    found=1
  fi
done < <(list)

if [ "$found" -ne 0 ]; then
  echo "Core dumps must not be committed; remove them (git rm --cached <path>)." >&2
  exit 1
fi
