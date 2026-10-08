#!/usr/bin/env bash
# Turn the built .deb/.rpm packages of one release into signed APT and
# YUM/DNF/Zypper repositories inside a checkout of the GitHub Pages site
# (served at https://packages.senhub.io). It does not commit and does not push.
#
# Usage:
#   packaging/repo/publish-repo.sh --channel stable|beta --version <tag> \
#       --packages-dir <dir> --site-dir <dir>
#
# Environment (all required):
#   PACKAGING_GPG_PRIVATE_KEY   armored, passphrase-protected secret key
#   PACKAGING_GPG_PASSPHRASE    its passphrase
#   PACKAGING_GPG_FINGERPRINT   fingerprint of the signing key
# Optional:
#   PACKAGING_BASE_URL          public URL of the site (default
#                               https://packages.senhub.io); written into the
#                               .repo files and index.html
#
# Tools (Ubuntu package names; ubuntu-latest has none of them preinstalled
# except gpg and dpkg):
#   sudo apt-get install -y apt-utils createrepo-c rpm gnupg
# apt-ftparchive (apt-utils), createrepo_c, rpmsign (rpm), gpg, dpkg-deb.
#
# <packages-dir> holds the eight files of the release (senhub-agent and
# senhub-agent-oss, amd64/arm64 .deb, x86_64/aarch64 .rpm). Layout produced:
#   apt/dists/<channel>/Release, InRelease, Release.gpg
#   apt/dists/<channel>/main/binary-{amd64,arm64}/Packages(.gz)
#   apt/pool/<channel>/main/<package>/<file>.deb
#   rpm/<channel>/{x86_64,aarch64}/<file>.rpm + repodata/ (+ repomd.xml.asc)
#   rpm/<channel>/senhub.repo, rpm/<channel>/senhub-zypper.repo
#   index.html (install instructions)
#
# A file already published under the same name is immutable: it is kept as it
# is (a rebuilt package must carry a new version). Running twice with the same
# inputs therefore leaves the site byte-identical except:
#   - apt/dists/<channel>/Release: the Date: line (apt-ftparchive stamps it);
#   - Release's signatures, InRelease and Release.gpg (a signature carries its
#     creation time), and rpm/<channel>/<arch>/repodata/repomd.xml.asc.
# Everything else, including repomd.xml (its revision and timestamps come from
# the packages' build time), is reproducible.
#
# Pruning: the 5 newest versions per package name are kept in the channel being
# published; the other channel is never read or written.
set -euo pipefail

KEEP_VERSIONS=5
BASE_URL=${PACKAGING_BASE_URL:-https://packages.senhub.io}
BASE_URL=${BASE_URL%/}

die() { echo "publish-repo: $*" >&2; exit 1; }

CHANNEL="" VERSION="" PKG_DIR="" SITE_DIR=""
while [ $# -gt 0 ]; do
    case "$1" in
        --channel) CHANNEL=${2:-}; shift 2 ;;
        --version) VERSION=${2:-}; shift 2 ;;
        --packages-dir) PKG_DIR=${2:-}; shift 2 ;;
        --site-dir) SITE_DIR=${2:-}; shift 2 ;;
        *) die "unknown argument: $1" ;;
    esac
done

case "$CHANNEL" in stable|beta) ;; *) die "--channel must be stable or beta" ;; esac
[ -n "$VERSION" ] || die "--version is required"
[ -d "$PKG_DIR" ] || die "--packages-dir is not a directory: $PKG_DIR"
[ -d "$SITE_DIR" ] || die "--site-dir is not a directory: $SITE_DIR"
for v in PACKAGING_GPG_PRIVATE_KEY PACKAGING_GPG_PASSPHRASE PACKAGING_GPG_FINGERPRINT; do
    [ -n "${!v:-}" ] || die "$v is not set"
done
for t in apt-ftparchive createrepo_c rpmsign rpm gpg gpgconf dpkg-deb dpkg gzip; do
    command -v "$t" >/dev/null || die "missing tool: $t (sudo apt-get install -y apt-utils createrepo-c rpm gnupg)"
done

VERSION=${VERSION#v}
case "$CHANNEL:$VERSION" in
    stable:*-*) die "stable channel refuses a prerelease tag ($VERSION)" ;;
esac
# Tag 0.6.2-beta.1 becomes package version 0.6.2~beta.1 (see Makefile PKG_VERSION).
PKG_VERSION=$(printf '%s' "$VERSION" | sed 's/-/~/')

SITE_DIR=$(cd "$SITE_DIR" && pwd)
PKG_DIR=$(cd "$PKG_DIR" && pwd)
FPR=$(printf '%s' "$PACKAGING_GPG_FINGERPRINT" | tr -d ' ' | tr a-f A-F)

WORK=$(mktemp -d)
export GNUPGHOME="$WORK/gnupg"
mkdir -m 700 "$GNUPGHOME"
cleanup() {
    gpgconf --kill gpg-agent >/dev/null 2>&1 || true
    rm -rf "$WORK"
}
trap cleanup EXIT

# ---- the eight release files ------------------------------------------------

EDITIONS="senhub-agent senhub-agent-oss"
DEB_ARCHES="amd64 arm64"
RPM_ARCHES="x86_64 aarch64"
for ed in $EDITIONS; do
    for a in $DEB_ARCHES; do
        [ -f "$PKG_DIR/${ed}_${PKG_VERSION}-1_${a}.deb" ] || die "missing ${ed}_${PKG_VERSION}-1_${a}.deb in $PKG_DIR"
    done
    for a in $RPM_ARCHES; do
        [ -f "$PKG_DIR/${ed}-${PKG_VERSION}-1.${a}.rpm" ] || die "missing ${ed}-${PKG_VERSION}-1.${a}.rpm in $PKG_DIR"
    done
done
count=$(find "$PKG_DIR" -maxdepth 1 \( -name '*.deb' -o -name '*.rpm' \) | wc -l | tr -d ' ')
[ "$count" = 8 ] || die "expected 8 packages in $PKG_DIR, found $count"

# ---- key: temporary keyring, passphrase preset in a private gpg-agent -------

printf 'allow-preset-passphrase\ndefault-cache-ttl 7200\nmax-cache-ttl 7200\n' > "$GNUPGHOME/gpg-agent.conf"
gpgconf --launch gpg-agent
printf '%s\n' "$PACKAGING_GPG_PRIVATE_KEY" |
    gpg --batch --quiet --pinentry-mode loopback --passphrase-file <(printf '%s' "$PACKAGING_GPG_PASSPHRASE") --import 2>/dev/null ||
    die "could not import PACKAGING_GPG_PRIVATE_KEY"
have=$(gpg --batch --list-secret-keys --with-colons 2>/dev/null | awk -F: '$1=="fpr"{print $10}')
printf '%s\n' "$have" | grep -qx "$FPR" ||
    die "the imported key does not match PACKAGING_GPG_FINGERPRINT"

PRESET=$(gpgconf --list-dirs libexecdir)/gpg-preset-passphrase
[ -x "$PRESET" ] || die "gpg-preset-passphrase not found at $PRESET"
gpg --batch --list-secret-keys --with-keygrip --with-colons "$FPR" 2>/dev/null |
    awk -F: '$1=="grp"{print $10}' |
    while read -r grip; do
        printf '%s' "$PACKAGING_GPG_PASSPHRASE" | "$PRESET" --preset "$grip"
    done
echo probe | gpg --batch --local-user "$FPR" --detach-sign --output /dev/null 2>/dev/null ||
    die "the passphrase does not unlock the signing key"

# The published gpg.key must be the key we sign with, otherwise every client
# would reject the repositories.
if [ -f "$SITE_DIR/gpg.key" ]; then
    have=$(gpg --batch --show-keys --with-colons "$SITE_DIR/gpg.key" 2>/dev/null | awk -F: '$1=="fpr"{print $10}')
    printf '%s\n' "$have" | grep -qx "$FPR" ||
        die "$SITE_DIR/gpg.key does not carry the signing key $FPR"
else
    gpg --batch --armor --export "$FPR" > "$SITE_DIR/gpg.key"
fi

gpg_sign() { gpg --batch --yes --local-user "$FPR" "$@"; }

# ---- helpers -----------------------------------------------------------------

# ver_gt A B: dpkg ordering, which orders '~' prereleases the way rpm does for
# the names used here (0.6.2~beta.1-1 < 0.6.2-1).
ver_gt() { dpkg --compare-versions "$1" gt "$2"; }

# keep_newest <dir> <ext> <name-fn> <version-fn>: delete the files of every
# package version beyond the $KEEP_VERSIONS newest, for each package name.
prune_dir() {
    local dir=$1 ext=$2 f name ver
    local -A versions=()
    while IFS= read -r f; do
        if [ "$ext" = deb ]; then
            name=$(dpkg-deb -f "$f" Package); ver=$(dpkg-deb -f "$f" Version)
        else
            name=$(rpm -qp --qf '%{NAME}' "$f" 2>/dev/null); ver=$(rpm -qp --qf '%{VERSION}-%{RELEASE}' "$f" 2>/dev/null)
        fi
        case " ${versions[$name]:-} " in *" $ver "*) ;; *) versions[$name]="${versions[$name]:-} $ver" ;; esac
        printf '%s\t%s\t%s\n' "$name" "$ver" "$f"
    done < <(find "$dir" -type f -name "*.$ext" | LC_ALL=C sort) > "$WORK/prune.list"
    for name in "${!versions[@]}"; do
        # shellcheck disable=SC2206
        local vs=(${versions[$name]}) i j t
        for ((i = 0; i < ${#vs[@]}; i++)); do
            for ((j = i + 1; j < ${#vs[@]}; j++)); do
                if ver_gt "${vs[j]}" "${vs[i]}"; then t=${vs[i]}; vs[i]=${vs[j]}; vs[j]=$t; fi
            done
        done
        for ((i = KEEP_VERSIONS; i < ${#vs[@]}; i++)); do
            echo "publish-repo: pruning $name ${vs[i]} from $dir"
            awk -F'\t' -v n="$name" -v v="${vs[i]}" '$1==n && $2==v {print $3}' "$WORK/prune.list" | xargs rm -f
        done
    done
}

# ---- APT ----------------------------------------------------------------------

APT_DIR="$SITE_DIR/apt"
POOL="pool/$CHANNEL"
mkdir -p "$APT_DIR/$POOL/main"
for ed in $EDITIONS; do
    mkdir -p "$APT_DIR/$POOL/main/$ed"
    for a in $DEB_ARCHES; do
        f="${ed}_${PKG_VERSION}-1_${a}.deb"
        [ -f "$APT_DIR/$POOL/main/$ed/$f" ] || cp "$PKG_DIR/$f" "$APT_DIR/$POOL/main/$ed/$f"
    done
done
prune_dir "$APT_DIR/$POOL" deb
find "$APT_DIR/$POOL" -type d -empty -delete
mkdir -p "$APT_DIR/$POOL/main"

DISTS="$APT_DIR/dists/$CHANNEL"
rm -rf "$DISTS"
(
    cd "$APT_DIR"
    export LC_ALL=C
    for a in $DEB_ARCHES; do
        mkdir -p "dists/$CHANNEL/main/binary-$a"
        apt-ftparchive --arch "$a" packages "$POOL" > "dists/$CHANNEL/main/binary-$a/Packages"
        gzip -9nc "dists/$CHANNEL/main/binary-$a/Packages" > "dists/$CHANNEL/main/binary-$a/Packages.gz"
    done
    apt-ftparchive \
        -o "APT::FTPArchive::Release::Origin=Sensor Factory" \
        -o "APT::FTPArchive::Release::Label=SenHub Agent" \
        -o "APT::FTPArchive::Release::Suite=$CHANNEL" \
        -o "APT::FTPArchive::Release::Codename=$CHANNEL" \
        -o "APT::FTPArchive::Release::Architectures=$DEB_ARCHES" \
        -o "APT::FTPArchive::Release::Components=main" \
        -o "APT::FTPArchive::Release::Description=SenHub Agent packages ($CHANNEL channel)" \
        release "dists/$CHANNEL" > "$WORK/Release"
    mv "$WORK/Release" "dists/$CHANNEL/Release"
)
gpg_sign --clearsign --output "$DISTS/InRelease" "$DISTS/Release"
gpg_sign --armor --detach-sign --output "$DISTS/Release.gpg" "$DISTS/Release"

# ---- RPM ----------------------------------------------------------------------

RPM_DIR="$SITE_DIR/rpm/$CHANNEL"
mkdir -p "$RPM_DIR"
for a in $RPM_ARCHES; do
    mkdir -p "$RPM_DIR/$a"
    for ed in $EDITIONS; do
        f="${ed}-${PKG_VERSION}-1.${a}.rpm"
        if [ ! -f "$RPM_DIR/$a/$f" ]; then
            cp "$PKG_DIR/$f" "$WORK/$f"
            rpmsign --addsign \
                --define "__gpg $(command -v gpg)" \
                --define "_gpg_name $FPR" \
                --define "_gpg_path $GNUPGHOME" \
                --define "_gpg_sign_cmd_extra_args --batch" \
                "$WORK/$f" >/dev/null || die "rpmsign failed on $f"
            mv "$WORK/$f" "$RPM_DIR/$a/$f"
        fi
    done
done
prune_dir "$RPM_DIR" rpm

for a in $RPM_ARCHES; do
    newest=0
    while IFS= read -r f; do
        t=$(rpm -qp --qf '%{BUILDTIME}' "$f" 2>/dev/null)
        [ "$t" -gt "$newest" ] && newest=$t
    done < <(find "$RPM_DIR/$a" -name '*.rpm' | LC_ALL=C sort)
    rm -rf "$RPM_DIR/$a/repodata"
    createrepo_c --quiet --general-compress-type gz --revision "$newest" --set-timestamp-to-revision "$RPM_DIR/$a"
    gpg_sign --armor --detach-sign --output "$RPM_DIR/$a/repodata/repomd.xml.asc" "$RPM_DIR/$a/repodata/repomd.xml"
done

cat > "$RPM_DIR/senhub.repo" <<EOF
[senhub-$CHANNEL]
name=SenHub Agent ($CHANNEL)
baseurl=$BASE_URL/rpm/$CHANNEL/\$basearch
enabled=1
gpgcheck=1
repo_gpgcheck=1
gpgkey=$BASE_URL/gpg.key
EOF
cat > "$RPM_DIR/senhub-zypper.repo" <<EOF
[senhub-$CHANNEL]
name=SenHub Agent ($CHANNEL)
baseurl=$BASE_URL/rpm/$CHANNEL/\$basearch
type=rpm-md
enabled=1
autorefresh=1
gpgcheck=1
repo_gpgcheck=1
pkg_gpgcheck=1
gpgkey=$BASE_URL/gpg.key
EOF

# ---- index.html ---------------------------------------------------------------

FPR_SPACED=$(printf '%s' "$FPR" | sed 's/.\{4\}/& /g; s/ $//')

block() { # block <id> <commands...>
    local id=$1; shift
    printf '<pre><code id="%s">' "$id"
    printf '%s\n' "$@" | sed 's/&/\&amp;/g; s/</\&lt;/g; s/>/\&gt;/g'
    printf '</code></pre>\n'
}

{
cat <<EOF
<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>SenHub Agent packages</title>
<style>
body{font:16px/1.5 system-ui,sans-serif;max-width:52rem;margin:2rem auto;padding:0 1rem;color:#1b1f24}
pre{background:#f3f4f6;padding:.8rem 1rem;overflow-x:auto;border-radius:4px;font-size:14px}
h1,h2,h3{line-height:1.25}
code{font-family:ui-monospace,Menlo,Consolas,monospace}
@media (prefers-color-scheme:dark){body{background:#12151a;color:#e6e8eb}pre{background:#1d222a}a{color:#7fb2ff}}
</style>
</head>
<body>
<h1>SenHub Agent packages</h1>
<p>Signed APT and YUM/DNF/Zypper repositories for the SenHub Agent on Linux (amd64 and arm64).</p>

<h2>Editions</h2>
<ul>
<li><code>senhub-agent-oss</code>: the open-source edition (Apache-2.0).</li>
<li><code>senhub-agent</code>: the full edition, under the SenHub commercial licence; a licence key enables its paid probes.</li>
</ul>
<p>The two editions install the same files and the same service: install one or the other. To switch, use <code>sudo apt install senhub-agent</code> on Debian and Ubuntu, <code>sudo dnf swap senhub-agent-oss senhub-agent</code> on RHEL and derivatives, or <code>sudo zypper install --force-resolution senhub-agent</code> on openSUSE and SLES (names exchanged to go back).</p>

<h2>Channels</h2>
<ul>
<li><code>stable</code>: final releases.</li>
<li><code>beta</code>: pre-releases (<code>X.Y.Z~beta.N</code>), published before the final release.</li>
</ul>
<p>Pick one channel per machine. The signing key fingerprint is <code>$FPR_SPACED</code>; it is served at <a href="$BASE_URL/gpg.key">$BASE_URL/gpg.key</a>.</p>
EOF
for ch in stable beta; do
    echo "<h2>Channel $ch</h2>"
    echo "<h3>Debian, Ubuntu (apt)</h3>"
    block "apt-$ch" \
        "sudo install -d -m 0755 /etc/apt/keyrings" \
        "curl -fsSL $BASE_URL/gpg.key | sudo gpg --dearmor --yes -o /etc/apt/keyrings/senhub.gpg" \
        "echo \"deb [arch=\$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/senhub.gpg] $BASE_URL/apt $ch main\" | sudo tee /etc/apt/sources.list.d/senhub.list" \
        "sudo apt update"
    block "install-apt-oss-$ch" "sudo apt install senhub-agent-oss"
    block "install-apt-full-$ch" "sudo apt install senhub-agent"
    echo "<h3>RHEL, Rocky Linux, AlmaLinux, Fedora (dnf)</h3>"
    block "dnf-$ch" "sudo curl -fsSLo /etc/yum.repos.d/senhub.repo $BASE_URL/rpm/$ch/senhub.repo"
    block "install-dnf-oss-$ch" "sudo dnf install senhub-agent-oss"
    block "install-dnf-full-$ch" "sudo dnf install senhub-agent"
    echo "<h3>openSUSE, SLES (zypper)</h3>"
    block "zypper-$ch" \
        "sudo rpm --import $BASE_URL/gpg.key" \
        "sudo curl -fsSLo /etc/zypp/repos.d/senhub.repo $BASE_URL/rpm/$ch/senhub-zypper.repo" \
        "sudo zypper refresh"
    block "install-zypper-oss-$ch" "sudo zypper install senhub-agent-oss"
    block "install-zypper-full-$ch" "sudo zypper install senhub-agent"
done
cat <<EOF
<p>Packages and repository metadata are signed with the same key. Each client checks both: the repository metadata (<code>repo_gpgcheck=1</code>, or the signed <code>InRelease</code> for apt) and every package (<code>gpgcheck=1</code>).</p>
</body>
</html>
EOF
} > "$SITE_DIR/index.html"

echo "publish-repo: $CHANNEL $VERSION published into $SITE_DIR"
