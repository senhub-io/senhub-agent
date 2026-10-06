#!/usr/bin/env bash
# Build the release manifest (schema: manifest.schema.json) of one release
# from the GitHub release JSON, which already carries each asset's size and
# sha256 digest. Nothing is downloaded.
#
#   gen-manifest.sh --release-json <file|-> [--image repo,edition,tag,digest]...
#                    [--chart repo,version,digest]...
#
# The release JSON is what `gh api repos/senhub-io/senhub-agent/releases/tags/<tag>`
# returns. The manifest goes to stdout. --image and --chart can be repeated.
#
# Assets that are not an installable file (metadata.json, releases.json,
# jt400runner-*, the .minisig files themselves) are not listed.
set -euo pipefail

die() { echo "gen-manifest: $*" >&2; exit 1; }

RELEASE_JSON="" IMAGES='[]' CHARTS='[]'
while [ $# -gt 0 ]; do
    case "$1" in
        --release-json) RELEASE_JSON=${2:-}; shift 2 ;;
        --image)
            IFS=, read -r repo edition tag digest extra <<<"${2:-}"
            [ -n "${digest:-}" ] && [ -z "${extra:-}" ] || die "--image wants repo,edition,tag,digest"
            IMAGES=$(jq -c --arg r "$repo" --arg e "$edition" --arg t "$tag" --arg d "$digest" \
                '. + [{repo:$r, edition:$e, tag:$t, digest:$d}]' <<<"$IMAGES")
            shift 2 ;;
        --chart)
            IFS=, read -r repo version digest extra <<<"${2:-}"
            [ -n "${digest:-}" ] && [ -z "${extra:-}" ] || die "--chart wants repo,version,digest"
            CHARTS=$(jq -c --arg r "$repo" --arg v "$version" --arg d "$digest" \
                '. + [{repo:$r, version:$v, digest:$d}]' <<<"$CHARTS")
            shift 2 ;;
        *) die "unknown argument: $1" ;;
    esac
done
[ -n "$RELEASE_JSON" ] || die "--release-json is required"
command -v jq >/dev/null || die "jq is required"
if [ "$RELEASE_JSON" = "-" ]; then RELEASE_JSON=/dev/stdin; fi
[ -r "$RELEASE_JSON" ] || die "cannot read $RELEASE_JSON"

jq --argjson images "$IMAGES" --argjson charts "$CHARTS" '
def edition($n): if ($n | startswith("senhub-agent-oss")) then "oss" else "full" end;

def classify:
  . as $n
  | if   ($n | test("^senhub-agent(-oss)?-(linux|windows)-(amd64|arm64)\\.zip$")) then
      ($n | capture("-(?<os>linux|windows)-(?<arch>amd64|arm64)\\.zip$")) + {kind: "zip"}
    elif ($n | test("^senhub-agent(-oss)?-[0-9][^ ]*-amd64\\.msi$")) then
      {os: "windows", arch: "amd64", kind: "msi"}
    elif ($n | test("^senhub-agent(-oss)?_[^ ]+_(amd64|arm64)\\.deb$")) then
      {os: "linux", arch: ($n | capture("_(?<a>amd64|arm64)\\.deb$").a), kind: "deb"}
    elif ($n | test("^senhub-agent(-oss)?-[^ ]+\\.(x86_64|aarch64)\\.rpm$")) then
      {os: "linux", arch: (if ($n | test("\\.x86_64\\.rpm$")) then "amd64" else "arm64" end), kind: "rpm"}
    else null end;

if .draft == true then error("release is a draft") else . end
| . as $rel
| ($rel.tag_name | sub("^v"; "")) as $version
| (if ($version | test("^[0-9]+\\.[0-9]+\\.[0-9]+$")) then "stable"
   elif ($version | test("^[0-9]+\\.[0-9]+\\.[0-9]+-beta\\.[0-9]+$")) then "beta"
   else error("unsupported tag: " + $version) end) as $channel
| (if ($channel == "beta") == ($rel.prerelease == true) then true
   else error("tag " + $version + " and the prerelease flag disagree") end) as $consistent
| ($rel.target_commitish | if test("^[0-9a-f]{40}$") then . else error("target_commitish is not a commit: " + .) end) as $commit
| [ $rel.assets[]
    | . as $a
    | ($a.name | classify) as $c
    | select($c != null)
    | {
        name: $a.name,
        os: $c.os,
        arch: $c.arch,
        edition: edition($a.name),
        kind: $c.kind,
        url: $a.browser_download_url,
        sha256: (($a.digest // error("no digest on " + $a.name)) | sub("^sha256:"; "")),
        size: $a.size,
        minisig_url: ([ $rel.assets[] | select(.name == ($a.name + ".minisig")) | .browser_download_url ] | first // null)
      }
  ] | sort_by(.name) as $artifacts
| ([ $artifacts[] | select((.kind == "zip" or .kind == "msi") and .minisig_url == null) | .name ]) as $pending
| ([ $artifacts[] | select(.kind == "msi") ] | length) as $msis
| {
    schema_version: 1,
    version: $version,
    channel: $channel,
    date: $rel.published_at,
    commit: $commit,
    complete: (($pending | length) == 0 and ($channel == "beta" or $msis == 2)),
    signing_pending: $pending,
    artifacts: $artifacts,
    images: ($images | sort_by(.repo)),
    charts: ($charts | sort_by(.repo))
  }
' "$RELEASE_JSON"
