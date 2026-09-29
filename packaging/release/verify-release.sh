#!/usr/bin/env bash
#
# Says whether a published release is finished.
#
# On GitHub an incomplete release is indistinguishable from a finished
# one: a missing installer, an unsigned executable and a signature that
# no longer matches its archive all render as a normal page. Twice in a
# row the Windows signing round ended without its files reaching the
# release, and both times it was found by hand, days later, because
# somebody thought to look.
#
# This asks the three questions that hand check asked:
#   1. is every asset a release of this kind should carry present;
#   2. does each archive's detached minisign signature verify;
#   3. does the Windows executable, and each MSI, carry an Authenticode
#      signature.
#
# It reports every problem rather than stopping at the first, and exits
# non-zero when any is found.
#
#   usage:  verify-release.sh <tag> [--repo owner/name]
#
set -uo pipefail

REPO="senhub-io/senhub-agent"
TAG=""
while [ $# -gt 0 ]; do
  case "$1" in
    --repo) REPO="$2"; shift 2 ;;
    -*) echo "ERROR: unknown option: $1" >&2; exit 2 ;;
    *) TAG="$1"; shift ;;
  esac
done
[ -n "$TAG" ] || { echo "usage: verify-release.sh <tag> [--repo owner/name]" >&2; exit 2; }

for tool in gh minisign unzip; do
  command -v "$tool" >/dev/null || { echo "ERROR: $tool is required" >&2; exit 2; }
done
HAVE_AUTHENTICODE=1
command -v osslsigncode >/dev/null || HAVE_AUTHENTICODE=0

# The public half of the release signing key, the same one every
# published binary embeds. It is public by construction: it is what
# verifies a signature, never what makes one.
RELEASE_PUBKEY="RWRlfkyeLpjI0MjTSfuvT/bDNHHaVJhRirQN8Z8LTAM+n4LKVbpjrlRh"

WORK="$(mktemp -d -t senhub-verify.XXXXXX)"
trap 'rm -rf "$WORK"' EXIT
printf 'untrusted comment: senhub release key\n%s\n' "$RELEASE_PUBKEY" > "$WORK/pub.key"

PROBLEMS=0
problem() { printf '  FAIL  %s\n' "$1"; PROBLEMS=$((PROBLEMS + 1)); }
ok()      { printf '  ok    %s\n' "$1"; }

# A beta carries no installer: the MSI round runs on production tags only.
EXPECT_MSI=1
case "$TAG" in *-beta) EXPECT_MSI=0 ;; esac

echo "Verifying $REPO@$TAG"
echo

ASSETS="$(gh release view "$TAG" --repo "$REPO" --json assets -q '.assets[].name' 2>/dev/null)"
[ -n "$ASSETS" ] || { echo "ERROR: no release $TAG on $REPO, or no assets" >&2; exit 2; }
has() { printf '%s\n' "$ASSETS" | grep -qx "$1"; }

ARCHIVES="senhub-agent-linux-amd64.zip senhub-agent-linux-arm64.zip senhub-agent-windows-amd64.zip
senhub-agent-oss-linux-amd64.zip senhub-agent-oss-linux-arm64.zip senhub-agent-oss-windows-amd64.zip"
if [ "$EXPECT_MSI" = "1" ]; then
  ARCHIVES="$ARCHIVES
senhub-agent-${TAG}-amd64.msi senhub-agent-oss-${TAG}-amd64.msi"
fi

echo "1. every expected asset is present"
for a in $ARCHIVES; do
  if has "$a"; then ok "$a"; else problem "$a is missing"; fi
  if has "$a.minisig"; then ok "$a.minisig"; else problem "$a.minisig is missing"; fi
done
echo

echo "2. every signature verifies against the release key"
for a in $ARCHIVES; do
  has "$a" && has "$a.minisig" || { problem "$a cannot be verified, it or its signature is missing"; continue; }
  gh release download "$TAG" --repo "$REPO" --pattern "$a" --pattern "$a.minisig" --dir "$WORK" --clobber >/dev/null 2>&1
  if minisign -V -p "$WORK/pub.key" -m "$WORK/$a" >/dev/null 2>&1; then
    ok "$a"
  else
    problem "$a: its signature does not verify — the archive was replaced after it was signed"
  fi
done
echo

echo "3. what Windows runs carries an Authenticode signature"
if [ "$HAVE_AUTHENTICODE" = "0" ]; then
  echo "  SKIP  osslsigncode is not installed; install it to check this"
  PROBLEMS=$((PROBLEMS + 1))
else
  for e in senhub-agent senhub-agent-oss; do
    z="${e}-windows-amd64.zip"
    [ -f "$WORK/$z" ] || { problem "$z was not downloaded, cannot look inside"; continue; }
    rm -rf "$WORK/x" && mkdir -p "$WORK/x"
    unzip -oq "$WORK/$z" -d "$WORK/x" 2>/dev/null
    if [ ! -f "$WORK/x/senhub-agent.exe" ]; then
      problem "$z holds no senhub-agent.exe"
    elif osslsigncode verify "$WORK/x/senhub-agent.exe" 2>&1 | grep -qi "no signature found"; then
      problem "$z holds an UNSIGNED senhub-agent.exe — the signing round did not reach the release"
    else
      ok "$z: senhub-agent.exe is signed"
    fi
  done
  if [ "$EXPECT_MSI" = "1" ]; then
    for m in "senhub-agent-${TAG}-amd64.msi" "senhub-agent-oss-${TAG}-amd64.msi"; do
      [ -f "$WORK/$m" ] || { problem "$m was not downloaded, cannot look inside"; continue; }
      if osslsigncode verify "$WORK/$m" 2>&1 | grep -qi "no signature found"; then
        problem "$m is UNSIGNED"
      else
        ok "$m is signed"
      fi
    done
  fi
fi
echo

if [ "$PROBLEMS" = "0" ]; then
  echo "$TAG is finished: every asset is present, signed, and its signature matches."
  exit 0
fi
echo "$TAG is NOT finished: $PROBLEMS problem(s) above."
echo
echo "The usual cause is a Windows signing round that ran locally without"
echo "reaching the release. See packaging/windows/sign-windows-release-exe.sh."
exit 1
