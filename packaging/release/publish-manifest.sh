#!/usr/bin/env bash
# Put one release manifest into a checkout of the packages site (served at
# https://packages.senhub.io). It does not commit and does not push.
#
#   publish-manifest.sh --manifest <file> --site-dir <dir>
#
# Writes, under <site-dir>/releases/:
#   <version>/manifest.json   this release (rewritten on every run: it is
#                             regenerated when signatures or images land)
#   <channel>/latest.json     a copy of the highest version of the channel;
#                             regenerating an older release never moves it back
#   index.json                every published release, newest first, per channel
#   manifest.schema.json      the schema the manifests follow
set -euo pipefail

die() { echo "publish-manifest: $*" >&2; exit 1; }

MANIFEST="" SITE=""
while [ $# -gt 0 ]; do
    case "$1" in
        --manifest) MANIFEST=${2:-}; shift 2 ;;
        --site-dir) SITE=${2:-}; shift 2 ;;
        *) die "unknown argument: $1" ;;
    esac
done
[ -r "$MANIFEST" ] || die "--manifest is not a readable file: $MANIFEST"
[ -d "$SITE" ] || die "--site-dir is not a directory: $SITE"
command -v jq >/dev/null || die "jq is required"

HERE=$(cd "$(dirname "$0")" && pwd)
BASE_URL=${PACKAGING_BASE_URL:-https://packages.senhub.io}
BASE_URL=${BASE_URL%/}

VERSION=$(jq -er '.version' "$MANIFEST") || die "manifest has no version"
CHANNEL=$(jq -er '.channel' "$MANIFEST") || die "manifest has no channel"
[ "$(jq -r '.schema_version' "$MANIFEST")" = 1 ] || die "unsupported schema_version"
case "$CHANNEL" in stable|beta) ;; *) die "bad channel: $CHANNEL" ;; esac

# Orders versions the way semver does: a release outranks its own betas,
# betas are ordered by their number.
SEMVER_KEY='def key: capture("^(?<a>[0-9]+)\\.(?<b>[0-9]+)\\.(?<c>[0-9]+)(-beta\\.(?<n>[0-9]+))?$")
  | [(.a|tonumber), (.b|tonumber), (.c|tonumber), (if .n then 0 else 1 end), ((.n // "0")|tonumber)];'

REL="$SITE/releases"
mkdir -p "$REL/$VERSION" "$REL/$CHANNEL"
jq -S . "$MANIFEST" > "$REL/$VERSION/manifest.json"
cp "$HERE/manifest.schema.json" "$REL/manifest.schema.json"

LATEST="$REL/$CHANNEL/latest.json"
if [ -f "$LATEST" ]; then
    current=$(jq -r '.version' "$LATEST")
    newer=$(jq -n -r "$SEMVER_KEY"' ("'"$VERSION"'"|key) >= ("'"$current"'"|key)')
else
    newer=true
fi
if [ "$newer" = true ]; then
    cp "$REL/$VERSION/manifest.json" "$LATEST"
fi

manifests=("$REL"/*/manifest.json)
jq -n -S --arg base "$BASE_URL" "$SEMVER_KEY"'
  [ inputs | {version, channel, date, complete} ] as $all
  | {
      schema_version: 1,
      channels: (
        reduce ("stable", "beta") as $c ({};
          .[$c] = {
            latest_url: ($base + "/releases/" + $c + "/latest.json"),
            releases: [ $all[] | select(.channel == $c)
                        | . + {manifest_url: ($base + "/releases/" + .version + "/manifest.json")} ]
                      | sort_by(.version | key) | reverse
          })
      )
    }' "${manifests[@]}" > "$REL/index.json"
echo "publish-manifest: $VERSION ($CHANNEL) written, latest.json of $CHANNEL is $(jq -r .version "$LATEST")"
