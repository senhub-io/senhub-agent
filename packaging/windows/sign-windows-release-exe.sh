#!/usr/bin/env bash
#
# Local half of the Windows EXE signing round (#622).
#
# For a published release tag, downloads the two Windows ZIPs
# (senhub-agent-windows-amd64.zip and the -oss- variant) from the public
# release, Authenticode-signs the senhub-agent.exe inside each with the
# Certum SimplySign session (via sign-release-msi.sh), re-zips, and
# re-uploads the ZIPs to the release (--clobber).
#
# The ZIPs' detached .minisig assets are stale after this — that is
# expected: the second half (enterprise publish-signed-exe.yml, manual
# dispatch) verifies the Authenticode signature, re-minisigns the ZIPs
# with the release key (which lives only in CI), and rebuilds the MSI so
# it embeds the SIGNED exe. Auto-updating agents fail safe on the stale
# .minisig in the interval — they refuse the archive and retry later.
#
# Prerequisites: gh (authenticated with write access to the public
# repo), an OPEN SimplySign Desktop session, unzip + zip.
#
#   usage:  sign-windows-release-exe.sh <tag> [--repo owner/name]
#
set -euo pipefail

REPO="senhub-io/senhub-agent"
TAG=""
while [ $# -gt 0 ]; do
  case "$1" in
    --repo) REPO="$2"; shift 2 ;;
    -*) echo "ERROR: unknown option: $1" >&2; exit 1 ;;
    *) TAG="$1"; shift ;;
  esac
done
[ -n "$TAG" ] || { echo "usage: sign-windows-release-exe.sh <tag> [--repo owner/name]" >&2; exit 1; }
case "$TAG" in
  *[!0-9.a-z-]*) echo "ERROR: suspicious tag: $TAG" >&2; exit 1 ;;
esac

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SIGNER="$SCRIPT_DIR/sign-release-msi.sh"
[ -x "$SIGNER" ] || { echo "ERROR: signer not found: $SIGNER" >&2; exit 1; }
command -v gh >/dev/null    || { echo "ERROR: gh CLI required" >&2; exit 1; }
command -v unzip >/dev/null || { echo "ERROR: unzip required" >&2; exit 1; }
command -v zip >/dev/null   || { echo "ERROR: zip required" >&2; exit 1; }

WORK="$(mktemp -d -t senhub-exe-sign.XXXXXX)"
trap 'rm -rf "$WORK"' EXIT
echo ">> workdir: $WORK"

for EDITION in senhub-agent senhub-agent-oss; do
  ZIP="${EDITION}-windows-amd64.zip"
  DIR="$WORK/$EDITION"
  mkdir -p "$DIR"

  echo ">> [$EDITION] downloading $ZIP from $REPO@$TAG"
  gh release download "$TAG" --repo "$REPO" --pattern "$ZIP" --dir "$DIR"

  echo ">> [$EDITION] extracting"
  unzip -q "$DIR/$ZIP" -d "$DIR/x"
  [ -f "$DIR/x/senhub-agent.exe" ] || { echo "ERROR: senhub-agent.exe not found in $ZIP" >&2; exit 1; }

  echo ">> [$EDITION] signing senhub-agent.exe"
  "$SIGNER" "$DIR/x/senhub-agent.exe" --in-place

  echo ">> [$EDITION] re-zipping"
  rm -f "$DIR/$ZIP"
  ( cd "$DIR/x" && zip -q "$DIR/$ZIP" senhub-agent.exe )

  echo ">> [$EDITION] uploading (replacing release asset)"
  gh release upload "$TAG" "$DIR/$ZIP" --repo "$REPO" --clobber
done

echo
echo "OK   both Windows ZIPs now carry an Authenticode-signed exe."
echo
echo "NEXT: dispatch the second half so the ZIPs are re-minisigned and the"
echo "MSI is rebuilt from the signed exe:"
echo
echo "  gh workflow run publish-signed-exe.yml \\"
echo "    --repo senhub-io/senhub-agent-enterprise -f tag=$TAG"
echo
echo "Then (production tags) sign + publish the MSI as usual:"
echo "  sign-release-msi.sh <downloaded unsigned MSI artifact> && \\"
echo "  gh release upload $TAG <signed msi> --repo $REPO && \\"
echo "  gh workflow run publish-signed-msi.yml --repo senhub-io/senhub-agent-enterprise -f tag=$TAG"
