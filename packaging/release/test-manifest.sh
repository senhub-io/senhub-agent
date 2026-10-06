#!/usr/bin/env bash
# Tests for gen-manifest.sh and publish-manifest.sh, run against the real
# GitHub release JSON of 0.6.2-beta.1 (beta, Windows zips not yet signed)
# and 0.6.1 (stable, everything signed, two MSIs) kept in testdata/.
#
#   packaging/release/test-manifest.sh
set -uo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
GEN="$HERE/gen-manifest.sh"
PUB="$HERE/publish-manifest.sh"
BETA="$HERE/testdata/release-0.6.2-beta.1.json"
STABLE="$HERE/testdata/release-0.6.1.json"
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

fail=0
ok()  { echo "ok   $1"; }
bad() { echo "FAIL $1"; fail=1; }
refused() { # refused <label> <command...>: the command must fail
    local label=$1; shift
    if "$@" >/dev/null 2>&1; then bad "$label"; else ok "$label"; fi
}
same() { # same <label> <file> <file>
    if cmp -s "$2" "$3"; then ok "$1"; else bad "$1"; fi
}
check() { # check <label> <jq filter> <file>
    if jq -e "$2" "$3" >/dev/null; then ok "$1"; else bad "$1"; fi
}

# The schema as executable assertions: jq has no JSON Schema validator, so
# the constraints of manifest.schema.json are restated here and the test
# below pins the schema's own enums to the same lists.
CONFORMS='
  def sha: test("^[0-9a-f]{64}$");
  .schema_version == 1
  and (.version | test("^[0-9]+\\.[0-9]+\\.[0-9]+(-beta\\.[0-9]+)?$"))
  and (.channel | IN("stable","beta"))
  and (.date | test("^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9:]{8}Z$"))
  and (.commit | test("^[0-9a-f]{40}$"))
  and (.complete | type == "boolean")
  and (.signing_pending | type == "array")
  and (.artifacts | length > 0)
  and (.artifacts | all(
        (.os | IN("linux","windows")) and (.arch | IN("amd64","arm64"))
        and (.edition | IN("full","oss")) and (.kind | IN("zip","msi","deb","rpm"))
        and (.url | startswith("https://")) and (.sha256 | sha)
        and (.size | type == "number" and . >= 0 and . == floor)
        and (.minisig_url == null or (.minisig_url | startswith("https://")))
        and (.name | length > 0)))
  and (.images | all(.repo and (.edition | IN("full","oss")) and .tag and (.digest | test("^sha256:[0-9a-f]{64}$"))))
  and (.charts | all(.repo and .version and (.digest | test("^sha256:[0-9a-f]{64}$"))))'

"$GEN" --release-json "$BETA" > "$WORK/beta.json" || bad "generate beta"
"$GEN" --release-json "$STABLE" > "$WORK/stable.json" || bad "generate stable"

check "beta manifest conforms"   "$CONFORMS" "$WORK/beta.json"
check "stable manifest conforms" "$CONFORMS" "$WORK/stable.json"

# shellcheck disable=SC2016
check "schema enums match the assertions" '
  (.properties.channel.enum == ["stable","beta"])
  and ."$defs".artifact.properties.kind.enum == ["zip","msi","deb","rpm"]
  and ."$defs".artifact.properties.os.enum == ["linux","windows"]
  and ."$defs".artifact.properties.arch.enum == ["amd64","arm64"]
  and ."$defs".artifact.properties.edition.enum == ["full","oss"]
  and (.required | sort) == (["schema_version","version","channel","date","commit","complete","signing_pending","artifacts","images"] | sort)' \
  "$HERE/manifest.schema.json"

check "beta identity" '.version == "0.6.2-beta.1" and .channel == "beta"
  and .commit == "aac44564b4536f978a79d63d4492ebe52a53c408"' "$WORK/beta.json"
check "beta lists 6 zips and 8 packages, no helper files" '
  (.artifacts | length) == 14
  and ([.artifacts[] | select(.kind == "zip")] | length) == 6
  and ([.artifacts[] | select(.kind == "deb")] | length) == 4
  and ([.artifacts[] | select(.kind == "rpm")] | length) == 4
  and ([.artifacts[].name | select(test("jt400|metadata|releases|minisig"))] | length) == 0' "$WORK/beta.json"
check "beta is incomplete until the Windows zips are signed" '
  .complete == false
  and (.signing_pending | sort) == ["senhub-agent-oss-windows-amd64.zip","senhub-agent-windows-amd64.zip"]' "$WORK/beta.json"
check "linux zips carry their signature, packages carry none" '
  ([.artifacts[] | select(.kind == "zip" and .os == "linux") | .minisig_url | . != null] | all)
  and ([.artifacts[] | select(.kind == "deb" or .kind == "rpm") | .minisig_url == null] | all)' "$WORK/beta.json"
check "rpm architectures are mapped to amd64/arm64" '
  [.artifacts[] | select(.kind == "rpm") | "\(.edition) \(.arch)"] | sort
  == ["full amd64","full arm64","oss amd64","oss arm64"]' "$WORK/beta.json"
check "sha256 and size come from the release" '
  (.artifacts[] | select(.name == "senhub-agent-linux-amd64.zip"))
  | .sha256 == "f8ecc56d046e4e6587b1c6422c64ff2ff2286c1235d069919c91e24ddebca957"
    and .size == 28994471 and .edition == "full"
    and .url == "https://github.com/senhub-io/senhub-agent/releases/download/0.6.2-beta.1/senhub-agent-linux-amd64.zip"
    and .minisig_url == (.url + ".minisig")' "$WORK/beta.json"

check "stable is complete with two MSIs" '
  .channel == "stable" and .complete == true and .signing_pending == []
  and ([.artifacts[] | select(.kind == "msi") | "\(.edition) \(.arch)"] | sort) == ["full amd64","oss amd64"]' "$WORK/stable.json"

# A stable release missing an MSI is not complete, even though every file
# that is there is signed.
jq '.assets |= map(select(.name | test("oss-0.6.1-amd64.msi") | not))' "$STABLE" > "$WORK/nomsi.json"
"$GEN" --release-json "$WORK/nomsi.json" > "$WORK/nomsi.out"
check "stable missing an MSI is incomplete" '.complete == false' "$WORK/nomsi.out"

# Images are passed in, sorted, and validated.
"$GEN" --release-json "$STABLE" \
  --image "ghcr.io/senhub-io/senhub-agent-oss,oss,0.6.1,sha256:$(printf 'b%.0s' {1..64})" \
  --image "ghcr.io/senhub-io/senhub-agent,full,0.6.1,sha256:$(printf 'a%.0s' {1..64})" > "$WORK/img.json"
check "images are listed and conform" "$CONFORMS"' and (.images | length) == 2 and .images[0].repo == "ghcr.io/senhub-io/senhub-agent"' "$WORK/img.json"
"$GEN" --release-json "$STABLE" \
  --chart "ghcr.io/senhub-io/charts/senhub-agent,0.6.1,sha256:$(printf 'c%.0s' {1..64})" > "$WORK/chart.json"
check "a chart is listed and conforms" "$CONFORMS"' and (.charts | length) == 1 and .charts[0].version == "0.6.1"' "$WORK/chart.json"
check "no chart gives an empty list" '.charts == []' "$WORK/img.json"
refused "a malformed --chart is refused" "$GEN" --release-json "$STABLE" --chart "only,two"
refused "a malformed --image is refused" "$GEN" --release-json "$STABLE" --image "only,three,fields"

# Refusals.
jq '.draft = true' "$STABLE" > "$WORK/draft.json"
refused "a draft is refused" "$GEN" --release-json "$WORK/draft.json"
jq '.prerelease = true' "$STABLE" > "$WORK/pre.json"
refused "a stable tag flagged prerelease is refused" "$GEN" --release-json "$WORK/pre.json"
jq '.target_commitish = "master"' "$STABLE" > "$WORK/branch.json"
refused "a branch name as commit is refused" "$GEN" --release-json "$WORK/branch.json"
jq '(.assets[] | select(.name == "senhub-agent-linux-amd64.zip") | .digest) = null' "$STABLE" > "$WORK/nodigest.json"
refused "an asset without digest is refused" "$GEN" --release-json "$WORK/nodigest.json"

# Determinism: same input, same bytes.
"$GEN" --release-json "$BETA" > "$WORK/beta2.json"
same "generation is deterministic" "$WORK/beta.json" "$WORK/beta2.json"

# Publishing into a site checkout.
SITE="$WORK/site"; mkdir -p "$SITE"
"$PUB" --manifest "$WORK/beta.json" --site-dir "$SITE" >/dev/null || bad "publish beta"
"$PUB" --manifest "$WORK/stable.json" --site-dir "$SITE" >/dev/null || bad "publish stable"
check "latest.json of each channel" '.version == "0.6.2-beta.1"' "$SITE/releases/beta/latest.json"
check "latest.json of stable"      '.version == "0.6.1"' "$SITE/releases/stable/latest.json"
same "latest.json is a copy of the manifest" "$SITE/releases/0.6.1/manifest.json" "$SITE/releases/stable/latest.json"
same "schema is published" "$HERE/manifest.schema.json" "$SITE/releases/manifest.schema.json"

# An older release regenerated later does not move latest back.
jq '.tag_name = "0.6.2-beta.0" | .target_commitish = "'"$(jq -r .target_commitish "$BETA")"'"' "$BETA" > "$WORK/b0.json"
"$GEN" --release-json "$WORK/b0.json" > "$WORK/b0m.json" && "$PUB" --manifest "$WORK/b0m.json" --site-dir "$SITE" >/dev/null
check "regenerating an older beta keeps latest" '.version == "0.6.2-beta.1"' "$SITE/releases/beta/latest.json"
jq '.tag_name = "0.6.2-beta.10"' "$BETA" > "$WORK/b10.json"
"$GEN" --release-json "$WORK/b10.json" > "$WORK/b10m.json" && "$PUB" --manifest "$WORK/b10m.json" --site-dir "$SITE" >/dev/null
check "beta.10 outranks beta.1 (numeric order)" '.version == "0.6.2-beta.10"' "$SITE/releases/beta/latest.json"
jq '.tag_name = "0.6.2" | .prerelease = false' "$STABLE" > "$WORK/s2.json"
"$GEN" --release-json "$WORK/s2.json" > "$WORK/s2m.json" && "$PUB" --manifest "$WORK/s2m.json" --site-dir "$SITE" >/dev/null
check "stable latest follows the highest version" '.version == "0.6.2"' "$SITE/releases/stable/latest.json"
check "the index lists newest first" '
  (.channels.beta.releases | map(.version)) == ["0.6.2-beta.10","0.6.2-beta.1","0.6.2-beta.0"]
  and (.channels.stable.releases | map(.version)) == ["0.6.2","0.6.1"]
  and .channels.stable.latest_url == "https://packages.senhub.io/releases/stable/latest.json"' "$SITE/releases/index.json"

# Re-running with unchanged input changes nothing.
before=$(cd "$SITE" && find . -type f -exec shasum {} + | sort)
"$PUB" --manifest "$WORK/stable.json" --site-dir "$SITE" >/dev/null
after=$(cd "$SITE" && find . -type f -exec shasum {} + | sort)
if [ "$before" = "$after" ]; then ok "publishing twice is a no-op"; else bad "publishing twice is a no-op"; fi

exit $fail
