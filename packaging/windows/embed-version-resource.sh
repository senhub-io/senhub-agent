#!/bin/sh
# Writes the Windows version resource (VERSIONINFO + icon) of one exe as
# <main-package-dir>/rsrc_windows_amd64.syso. The next
# `GOOS=windows GOARCH=amd64 go build` of that package links it in; builds
# for any other target ignore the file (its name carries the constraint).
#
#   embed-version-resource.sh <version> <main-package-dir> <exe-name> <description>
#
# Environment:
#   BUILD_NUMBER  fourth field of the numeric file version. Defaults to the
#                 commit count of this repository, so a later build of the
#                 same X.Y.Z always carries a higher file version: Windows
#                 Installer only replaces a versioned file with a strictly
#                 higher one.
#
# Remove the .syso after the build: a stale one would stamp the next
# Windows build with the previous version.
#
# Called by `make build-windows` here, and by the release workflows of the
# enterprise repository against their core checkout.
set -eu

if [ $# -ne 4 ]; then
	echo "usage: $0 <version> <main-package-dir> <exe-name> <description>" >&2
	exit 2
fi
version=$1
pkgdir=$2
name=$3
description=$4

# Pinned: go run with an explicit version neither reads nor edits go.mod.
GO_WINRES=github.com/tc-hib/go-winres@v0.3.3

core=$(cd "$(dirname "$0")/../.." && pwd)
build=${BUILD_NUMBER:-$(git -C "$core" rev-list --count HEAD)}

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

cp "$core/packaging/windows/senhub.ico" "$tmp/senhub.ico"

# Both tools run on the build host, whatever target the caller builds for.
env -u GOOS -u GOARCH go -C "$core" run ./packaging/windows/winversion json \
	-version "$version" -build "$build" -name "$name" \
	-description "$description" -icon senhub.ico >"$tmp/winres.json"

env -u GOOS -u GOARCH GOFLAGS= GOWORK=off go run "$GO_WINRES" make \
	--in "$tmp/winres.json" --arch amd64 --out "$pkgdir/rsrc"

echo "version resource $version (build $build) -> $pkgdir/rsrc_windows_amd64.syso"
