#!/usr/bin/env bash
# End-to-end proof of packaging/repo/publish-repo.sh: build packages, publish
# them into a throwaway site signed with a THROWAWAY key, serve it over HTTP
# from a container, and use it from real distributions exactly as index.html
# tells users to. The real signing key is never involved: keys are generated
# here, inside a container, and discarded.
#
# Usage: packaging/repo/test-repo.sh
# Env:
#   DISTROS    subset of: debian12 ubuntu2204 ubuntu2404 rocky9 leap156 (default all)
#   KEYTYPES   subset of: ed25519 rsa4096 (default both; the whole test runs per type)
#   STRICT=1   count an incompatible key type (a matrix FAIL) as a test failure
#              for ed25519 too (rsa4096 is always strict)
#   KEEP=1     keep the work directory and containers for inspection
#
# Needs docker and a go toolchain (packages are built with `make packages`;
# already built versions in dist/packages are reused).
#
# What runs per key type and distribution:
#   - stable channel: install, signature verified
#   - beta channel: repository checks isolated (APT Release; rpm package
#     signature; repomd.xml), wrongly-signed and unsigned repositories
#     refused, install of 0.6.2~beta.1, then 0.6.2~beta.2 published and
#     installed as a plain upgrade
# Once per key type: idempotence of a second publish, the other channel left
# untouched. Once: pruning to the 5 newest versions.
#
# The client containers run on the docker host's architecture, so only that
# architecture's packages are installed; both are published and indexed.
# shellcheck disable=SC2015
set -u

cd "$(dirname "$0")/../.." || exit 2
ROOT=$(pwd)

V0=0.6.1
V1=0.6.2-beta.1
V2=0.6.2-beta.2
PRUNE_VERSIONS="0.6.2-beta.3 0.6.2-beta.4 0.6.2-beta.5 0.6.2-beta.9 0.6.2-beta.10"
DISTROS=${DISTROS:-"debian12 ubuntu2204 ubuntu2404 rocky9 leap156"}
KEYTYPES=${KEYTYPES:-"ed25519 rsa4096"}
PASS="throwaway-test-passphrase"

spec() { # image|family|prepare
    case "$1" in
        debian12)   echo "debian:12|apt|apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq curl gnupg ca-certificates" ;;
        ubuntu2204) echo "ubuntu:22.04|apt|apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq curl gnupg ca-certificates" ;;
        ubuntu2404) echo "ubuntu:24.04|apt|apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq curl gnupg ca-certificates" ;;
        rocky9)     echo "rockylinux:9|dnf|command -v curl" ;;
        leap156)    echo "opensuse/leap:15.6|zypper|command -v curl" ;;
        *) return 1 ;;
    esac
}

# Under dist/ (not /tmp): the docker VM only shares the home directory.
mkdir -p dist
WORK=$(mktemp -d "$ROOT/dist/repotest.XXXXXX")
NAME="senhub-repotest-$$"
SRV="$NAME-srv"
FAILED=0
MATRIX=()
FINDINGS=()

# shellcheck disable=SC2329
cleanup() {
    docker rm -f "$SRV" >/dev/null 2>&1
    for c in $(docker ps -aq --filter "name=$NAME-c"); do docker rm -f "$c" >/dev/null 2>&1; done
    docker network rm "$NAME" >/dev/null 2>&1
    if [ "${KEEP:-0}" = 1 ]; then echo "work dir kept: $WORK"; else rm -rf "$WORK"; fi
}
trap cleanup EXIT

say()  { echo "== $*"; }
fail() { echo "FAIL: $*"; FAILED=1; }

# ---- packages ---------------------------------------------------------------

pkgver() { make --no-print-directory package-version VERSION="$1"; }

ensure_packages() { # <version>
    local v=$1 pv
    pv=$(pkgver "$v")
    local have=(dist/packages/*"${pv}-1"[._]*)
    if [ "${#have[@]}" != 8 ]; then
        say "building packages $v"
        make packages EDITION=oss VERSION="$v" >/dev/null 2>&1 || { echo "make packages oss $v failed" >&2; exit 2; }
        make packages EDITION=full BINARY_DIR="$ROOT/dist" VERSION="$v" >/dev/null 2>&1 || { echo "make packages full $v failed" >&2; exit 2; }
    fi
    mkdir -p "$WORK/pk/$v"
    cp dist/packages/*"${pv}-1"[._]* "$WORK/pk/$v/"
    [ "$(find "$WORK/pk/$v" -type f | wc -l | tr -d ' ')" = 8 ] || { echo "expected 8 packages for $v" >&2; exit 2; }
}

# ---- server container -------------------------------------------------------

in_srv() { docker exec "$SRV" "$@"; }

start_server() {
    docker network create "$NAME" >/dev/null
    docker run -d --name "$SRV" --network "$NAME" -v "$WORK":/work -v "$ROOT":/src:ro ubuntu:24.04 sleep infinity >/dev/null
    in_srv bash -c 'apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq apt-utils createrepo-c rpm gnupg dpkg-dev python3 curl ca-certificates' >/dev/null 2>&1 ||
        { echo "tool install in the server container failed" >&2; exit 2; }
    mkdir -p "$WORK/www" "$WORK/keys"
    docker exec -d "$SRV" python3 -m http.server 8000 --directory /work/www
}

gen_key() { # <name> <algo>
    # shellcheck disable=SC2016
    in_srv bash -c '
        set -e
        export GNUPGHOME=$(mktemp -d)
        gpg --batch --quiet --pinentry-mode loopback --passphrase "$1" --quick-generate-key "SenHub repo test $2 <repo-test@example.invalid>" "$3" sign never
        fpr=$(gpg --list-keys --with-colons | awk -F: "\$1==\"fpr\"{print \$10; exit}")
        gpg --batch --pinentry-mode loopback --passphrase "$1" --armor --export-secret-keys "$fpr" > /work/keys/$2.sec
        gpg --armor --export "$fpr" > /work/keys/$2.pub
        echo "$fpr" > /work/keys/$2.fpr
        gpgconf --kill gpg-agent; rm -rf "$GNUPGHOME"
    ' _ "$PASS" "$1" "$2"
}

init_site() { # <site> <key>
    local d="$WORK/www/$1"
    rm -rf "$d"; mkdir -p "$d"
    cp "$WORK/keys/$2.pub" "$d/gpg.key"
    echo "# stub" > "$d/README.md"; echo packages.senhub.io > "$d/CNAME"; : > "$d/.nojekyll"; echo "<html>stub</html>" > "$d/index.html"
}

publish() { # <site> <key> <channel> <version> [urlname]
    local site=$1 key=$2 ch=$3 v=$4 urlname=${5:-$1}
    PACKAGING_GPG_PRIVATE_KEY=$(cat "$WORK/keys/$key.sec") \
    PACKAGING_GPG_PASSPHRASE=$PASS \
    PACKAGING_GPG_FINGERPRINT=$(cat "$WORK/keys/$key.fpr") \
    PACKAGING_BASE_URL="http://$SRV:8000/$urlname" \
    docker exec -e PACKAGING_GPG_PRIVATE_KEY -e PACKAGING_GPG_PASSPHRASE -e PACKAGING_GPG_FINGERPRINT -e PACKAGING_BASE_URL "$SRV" \
        /src/packaging/repo/publish-repo.sh --channel "$ch" --version "$v" --packages-dir "/work/pk/$v" --site-dir "/work/www/$site" >"$WORK/publish.log" 2>&1 ||
        { cat "$WORK/publish.log"; return 1; }
}

# Re-sign the repository metadata with <key> after stripping every package signature.
strip_package_sigs() { # <site> <key>
    PACKAGING_GPG_PRIVATE_KEY=$(cat "$WORK/keys/$2.sec") PACKAGING_GPG_PASSPHRASE=$PASS \
    docker exec -e PACKAGING_GPG_PRIVATE_KEY -e PACKAGING_GPG_PASSPHRASE "$SRV" bash -c '
        set -e
        export GNUPGHOME=$(mktemp -d)
        printf "%s\n" "$PACKAGING_GPG_PRIVATE_KEY" | gpg --batch --quiet --pinentry-mode loopback --passphrase-file <(printf "%s" "$PACKAGING_GPG_PASSPHRASE") --import 2>/dev/null
        fpr=$(gpg --list-keys --with-colons | awk -F: "\$1==\"fpr\"{print \$10; exit}")
        for dir in /work/www/'"$1"'/rpm/*/*/; do
            rpmsign --delsign "$dir"*.rpm >/dev/null
            rm -rf "$dir/repodata"
            createrepo_c --quiet --general-compress-type gz "$dir"
            echo "$PACKAGING_GPG_PASSPHRASE" | gpg --batch --pinentry-mode loopback --passphrase-fd 0 --local-user "$fpr" --armor --detach-sign --output "$dir/repodata/repomd.xml.asc" "$dir/repodata/repomd.xml"
        done
        gpgconf --kill gpg-agent; rm -rf "$GNUPGHOME"
    '
}

# ---- client script (runs inside each distribution) --------------------------

write_client() {
    cat > "$WORK/client.sh" <<'CLIENT'
#!/bin/bash
# Runs inside a distribution container. Prints T:<name>:<PASS|FAIL|INFO>:<detail>.
# shellcheck disable=SC2015
set -u
FAMILY=$1; PHASE=$2
IDX="http://$SRV/good/index.html"
t() { echo "T:$1:$2:${3:-}" | tee -a /tmp/t.log; }

getblock() {
    curl -fsS "$IDX" | awk -v id="$1" '
        index($0, "<code id=\"" id "\">") { f = 1; sub(/.*<code id="[^"]*">/, "") }
        f { if (sub(/<\/code>.*/, "")) { print; exit } print }' | sed 's/&lt;/</g; s/&gt;/>/g; s/&amp;/\&/g'
}

# Run an index.html block as a user would, minus sudo and the interactive prompts.
run_block() {
    getblock "$1" | sed -e 's/sudo //g' \
        -e 's/apt install /apt-get install -y /' -e 's/dnf install /dnf install -y /' \
        -e 's/zypper /zypper --non-interactive /' > /tmp/block.sh
    [ -s /tmp/block.sh ] || { echo "block $1 not found in index.html"; return 99; }
    bash -e /tmp/block.sh
}

setup_id()   { echo "$FAMILY-$1"; }
install_id() { echo "install-$FAMILY-oss-$1"; }

pkg_version() {
    case $FAMILY in
        apt) dpkg-query -W -f='${Version}' senhub-agent-oss 2>/dev/null ;;
        *) rpm -q --qf '%{VERSION}-%{RELEASE}' senhub-agent-oss 2>/dev/null ;;
    esac
}
pkg_installed() {
    case $FAMILY in
        apt) dpkg-query -W -f='${Status}' senhub-agent-oss 2>/dev/null | grep -q 'install ok installed' ;;
        *) rpm -q senhub-agent-oss >/dev/null 2>&1 ;;
    esac
}
pkg_remove() {
    case $FAMILY in
        apt) apt-get remove -y -q senhub-agent-oss >/dev/null 2>&1 ;;
        dnf) dnf remove -y -q senhub-agent-oss >/dev/null 2>&1 ;;
        zypper) zypper --non-interactive remove senhub-agent-oss >/dev/null 2>&1 ;;
    esac
}
repofile() { case $FAMILY in dnf) echo /etc/yum.repos.d/senhub.repo ;; zypper) echo /etc/zypp/repos.d/senhub.repo ;; apt) echo /etc/apt/sources.list.d/senhub.list ;; esac; }
point_at() { # <site>: switch the repo URL only; the trusted key stays the good one
    case $FAMILY in
        apt) sed -i -E "s#/(good|evil|nosig|nopkg)/apt#/$1/apt#" "$(repofile)"; rm -rf /var/lib/apt/lists/* ;;
        *) sed -i -E "/^baseurl=/ s#/(good|evil|nosig|nopkg)/rpm#/$1/rpm#" "$(repofile)"; clean_caches ;;
    esac
}
clean_caches() {
    case $FAMILY in
        dnf) dnf clean all >/dev/null 2>&1 ;;
        zypper) zypper --non-interactive clean -a >/dev/null 2>&1 ;;
    esac
}
set_gpg() { # <pkg 0|1> <repo 0|1>
    local f; f=$(repofile)
    sed -i '/^gpgcheck=/d; /^repo_gpgcheck=/d; /^pkg_gpgcheck=/d' "$f"
    if [ "$FAMILY" = zypper ]; then
        printf 'gpgcheck=0\nrepo_gpgcheck=%s\npkg_gpgcheck=%s\n' "$2" "$1" >> "$f"
    else
        printf 'gpgcheck=%s\nrepo_gpgcheck=%s\n' "$1" "$2" >> "$f"
    fi
    clean_caches
}
refresh_cmd() {
    case $FAMILY in
        apt) apt-get update ;;
        dnf) dnf -y makecache ;;
        zypper) zypper --non-interactive refresh -f ;;
    esac
}
install_cmd() { run_block "$(install_id beta)"; }
last() { grep -iE 'error|E:|W:|fail|sign|key|gpg' "$1" | tail -2 | tr '\n' ' ' | cut -c1-240; }

negative() { # <variant> <label>
    point_at "$1"
    refresh_cmd >/tmp/neg.log 2>&1; local r=$?
    install_cmd >>/tmp/neg.log 2>&1; local i=$?
    if pkg_installed; then
        t "reject_$2" FAIL "package installed from a $2 repository"; pkg_remove
    elif [ $r -ne 0 ] || [ $i -ne 0 ]; then
        t "reject_$2" PASS "$(last /tmp/neg.log)"
    else
        t "reject_$2" FAIL "no error and no package"
    fi
    point_at good
}

phase1_apt() {
    run_block apt-beta >/tmp/setup.log 2>&1 || { t setup FAIL "$(tail -3 /tmp/setup.log | tr '\n' ' ')"; return; }
    apt-get update >/tmp/up.log 2>&1; local rc=$?
    if [ $rc -eq 0 ] && ! grep -qE '^(W|E):' /tmp/up.log; then
        t a_release PASS "apt update clean, InRelease verified"
    else
        t a_release FAIL "$(grep -E '^(W|E):' /tmp/up.log | head -2 | tr '\n' ' ')"; return
    fi
    mv /etc/apt/keyrings/senhub.gpg /tmp/senhub.gpg; rm -rf /var/lib/apt/lists/*
    apt-get update >/tmp/nokey.log 2>&1 && ! grep -qE '^(W|E):' /tmp/nokey.log &&
        t requires_key FAIL "updated without the key" || t requires_key PASS "refused without the key: $(grep -E '^(W|E):' /tmp/nokey.log | head -1)"
    mv /tmp/senhub.gpg /etc/apt/keyrings/senhub.gpg; rm -rf /var/lib/apt/lists/*
    negative evil wrongly-signed
    negative nosig unsigned
    run_block apt-beta >/dev/null 2>&1
    run_block "$(install_id beta)" >/tmp/inst.log 2>&1 || { t install FAIL "$(tail -3 /tmp/inst.log | tr '\n' ' ')"; return; }
    [ "$(pkg_version)" = "0.6.2~beta.1-1" ] && t install_beta1 PASS "$(pkg_version)" || t install_beta1 FAIL "got '$(pkg_version)'"
}

phase1_rpm() {
    run_block "$(setup_id beta)" >/tmp/setup.log 2>&1 || { t setup FAIL "$(tail -3 /tmp/setup.log | tr '\n' ' ')"; return; }
    grep -q '^repo_gpgcheck=1' "$(repofile)" && grep -qE '^(pkg_)?gpgcheck=1' "$(repofile)" && t checks_enabled PASS "gpgcheck and repo_gpgcheck on" || t checks_enabled FAIL "$(cat "$(repofile)")"
    # (c): metadata signature only
    set_gpg 0 1
    refresh_cmd >/tmp/c.log 2>&1 && t c_repomd PASS "repomd.xml.asc verified" || { t c_repomd FAIL "$(last /tmp/c.log)"; }
    # (b): package signature only
    set_gpg 1 0
    refresh_cmd >/tmp/b0.log 2>&1
    install_cmd >/tmp/b.log 2>&1
    if pkg_installed; then
        t b_pkgsig PASS "installed with package signature check on: $(rpm -q --qf '%{SIGPGP:pgpsig}|%{RSAHEADER:pgpsig}' senhub-agent-oss)"
        pkg_remove
    else
        t b_pkgsig FAIL "$(grep -iE 'senhub|error|fail|key|sign' /tmp/b.log | head -8 | tr '\n' ' ' | cut -c1-500)"
    fi
    local arch rpmf; arch=$(uname -m); rpmf="senhub-agent-oss-0.6.2~beta.1-1.$arch.rpm"
    curl -fsS -o /tmp/p.rpm "http://$SRV/good/rpm/beta/$arch/$rpmf" &&
        t rpm_K INFO "$(rpm -K -v /tmp/p.rpm 2>&1 | tr '\n' ' ' | cut -c1-300) | $(rpm -qip /tmp/p.rpm 2>&1 | grep -i '^Signature' | cut -c1-200)"
    run_block "$(setup_id beta)" >/dev/null 2>&1; clean_caches
    if grep -q 'T:c_repomd:FAIL\|T:b_pkgsig:FAIL' /tmp/t.log; then return; fi
    negative evil wrongly-signed
    negative nosig unsigned-metadata
    negative nopkg unsigned-packages
    run_block "$(setup_id beta)" >/dev/null 2>&1; clean_caches
    run_block "$(install_id beta)" >/tmp/inst.log 2>&1 || { t install FAIL "$(tail -4 /tmp/inst.log | tr '\n' ' ')"; return; }
    [ "$(pkg_version)" = "0.6.2~beta.1-1" ] && t install_beta1 PASS "$(pkg_version)" || t install_beta1 FAIL "got '$(pkg_version)'"
}

phase2() {
    case $FAMILY in
        apt) apt-get update -q >/dev/null 2>&1; apt-get upgrade -y -q >/tmp/up.log 2>&1 ;;
        dnf) dnf upgrade --refresh -y >/tmp/up.log 2>&1 ;;
        zypper) zypper --non-interactive refresh >/dev/null 2>&1; zypper --non-interactive update >/tmp/up.log 2>&1 ;;
    esac
    [ "$(pkg_version)" = "0.6.2~beta.2-1" ] && t upgrade_beta2 PASS "$(pkg_version)" || t upgrade_beta2 FAIL "got '$(pkg_version)': $(tail -3 /tmp/up.log | tr '\n' ' ')"
    # stable channel last: remove the beta, install the stable build
    pkg_remove
    case $FAMILY in apt) rm -rf /var/lib/apt/lists/*; rm -f /etc/apt/sources.list.d/senhub.list ;; *) rm -f "$(repofile)"; clean_caches ;; esac
    run_block "$(setup_id stable)" >/tmp/setup.log 2>&1 || { t stable FAIL "$(tail -3 /tmp/setup.log | tr '\n' ' ')"; return; }
    run_block "$(install_id stable)" >/tmp/inst.log 2>&1 || { t stable FAIL "$(tail -4 /tmp/inst.log | tr '\n' ' ')"; return; }
    [ "$(pkg_version)" = "0.6.1-1" ] && t stable_install PASS "$(pkg_version)" || t stable_install FAIL "got '$(pkg_version)'"
}

case $PHASE in
    1) if [ "$FAMILY" = apt ]; then phase1_apt; else phase1_rpm; fi ;;
    2) phase2 ;;
esac
CLIENT
}

# ---- one distribution ---------------------------------------------------------

run_distro() { # <distro> <key>
    local distro=$1 key=$2 s image family prep c out="$WORK/out-$1-$2.txt"
    s=$(spec "$distro"); image=${s%%|*}; s=${s#*|}; family=${s%%|*}; prep=${s#*|}
    say "$key / $distro ($image)"
    c="$NAME-c-$distro-$key"
    rm -rf "$WORK/www/good"; cp -a "$WORK/base-good-$key" "$WORK/www/good"
    docker run -d --name "$c" --network "$NAME" -e SRV="$SRV:8000" "$image" sleep infinity >/dev/null
    docker cp "$WORK/client.sh" "$c:/client.sh" >/dev/null
    if ! docker exec "$c" sh -c "$prep" >/dev/null 2>&1; then fail "$key/$distro: container preparation failed"; docker rm -f "$c" >/dev/null; return; fi
    : > "$out"
    timeout 1200 docker exec -e SRV="$SRV:8000" "$c" bash -c "bash /client.sh $family 1 2>&1 </dev/null" >> "$out"
    publish good "$key" beta "$V2" good || fail "$key/$distro: publishing beta.2 failed"
    timeout 1200 docker exec -e SRV="$SRV:8000" "$c" bash -c "bash /client.sh $family 2 2>&1 </dev/null" >> "$out"
    docker rm -f "$c" >/dev/null
    grep '^T:' "$out" | sed "s#^T:#   #"
    local a b cc
    a=$(grep '^T:a_release:' "$out" | cut -d: -f3); b=$(grep '^T:b_pkgsig:' "$out" | cut -d: -f3); cc=$(grep '^T:c_repomd:' "$out" | cut -d: -f3)
    MATRIX+=("$key|$distro|${a:--}|${b:--}|${cc:--}")
    local matrix_fail=0
    grep -qE '^T:(a_release|b_pkgsig|c_repomd):FAIL' "$out" && matrix_fail=1
    if grep '^T:' "$out" | grep -v ':\(a_release\|b_pkgsig\|c_repomd\):' | grep -q ':FAIL:'; then
        if [ $matrix_fail = 1 ] && [ "$key" = ed25519 ] && [ "${STRICT:-0}" != 1 ]; then :; else fail "$key/$distro: failing checks (see above)"; fi
    fi
    if [ $matrix_fail = 1 ]; then
        if [ "$key" = ed25519 ] && [ "${STRICT:-0}" != 1 ]; then FINDINGS+=("$key is NOT accepted on $distro (flow stopped there)"); else fail "$key/$distro: key type rejected"; fi
    fi
    if ! grep -q '^T:upgrade_beta2:PASS' "$out" && [ $matrix_fail = 0 ]; then fail "$key/$distro: beta.1 to beta.2 upgrade not proven"; fi
}

# ---- per key type -------------------------------------------------------------

site_hash() { # <dir> <relative paths...>: content hash of a tree
    (cd "$1" && shift && find "$@" -type f 2>/dev/null | LC_ALL=C sort | xargs sha256sum | sha256sum | cut -c1-16)
}

run_key() { # <key>
    local key=$1 evil="$1-evil"
    say "key type $key: generating throwaway keys and publishing"
    case $key in ed25519) gen_key "$key" ed25519; gen_key "$evil" ed25519 ;; rsa4096) gen_key "$key" rsa4096; gen_key "$evil" rsa4096 ;; esac ||
        { fail "$key: key generation"; return; }

    init_site good "$key"
    publish good "$key" stable "$V0" good && publish good "$key" beta "$V1" good || { fail "$key: initial publish"; return; }
    cp -a "$WORK/www/good" "$WORK/base-good-$key"

    # idempotence
    local before="$WORK/idem-$key"
    rm -rf "$before"; cp -a "$WORK/www/good" "$before"
    publish good "$key" beta "$V1" good && publish good "$key" stable "$V0" good || fail "$key: second publish"
    local diffs
    diffs=$(diff -rq "$before" "$WORK/www/good" | grep -vE 'dists/(beta|stable)/(Release|InRelease|Release\.gpg)|repomd\.xml\.asc' || true)
    if [ -z "$diffs" ]; then
        say "$key: idempotence PASS (only Release/InRelease/Release.gpg and repomd.xml.asc differ: $(diff -rq "$before" "$WORK/www/good" | wc -l | tr -d ' ') files)"
        diff -r "$before/apt/dists/beta/Release" "$WORK/www/good/apt/dists/beta/Release" | grep '^[<>]' | sed 's/^/     /'
    else
        fail "$key: second publish changed files: $diffs"
    fi
    rm -rf "$before"; cp -a "$WORK/base-good-$key" "$WORK/www/good"

    # the other channel is never touched
    local s0 s1
    s0=$(site_hash "$WORK/www/good" apt/dists/stable apt/pool/stable rpm/stable)
    publish good "$key" beta "$V2" good || fail "$key: beta.2 publish"
    s1=$(site_hash "$WORK/www/good" apt/dists/stable apt/pool/stable rpm/stable)
    [ "$s0" = "$s1" ] && say "$key: stable channel untouched by a beta publish PASS" || fail "$key: stable channel changed by a beta publish"

    # wrongly signed and unsigned variants
    init_site evil "$evil"
    publish evil "$evil" stable "$V0" evil && publish evil "$evil" beta "$V1" evil || { fail "$key: evil publish"; return; }
    rm -rf "$WORK/www/nosig"; cp -a "$WORK/base-good-$key" "$WORK/www/nosig"
    find "$WORK/www/nosig" \( -name InRelease -o -name Release.gpg -o -name repomd.xml.asc \) -delete
    rm -rf "$WORK/www/nopkg"; cp -a "$WORK/base-good-$key" "$WORK/www/nopkg"
    strip_package_sigs nopkg "$key" || { fail "$key: nopkg variant"; return; }
    cp -a "$WORK/www/evil" "$WORK/base-evil-$key"; cp -a "$WORK/www/nosig" "$WORK/base-nosig-$key"; cp -a "$WORK/www/nopkg" "$WORK/base-nopkg-$key"

    for d in $DISTROS; do
        rm -rf "$WORK/www/evil" "$WORK/www/nosig" "$WORK/www/nopkg"
        cp -a "$WORK/base-evil-$key" "$WORK/www/evil"; cp -a "$WORK/base-nosig-$key" "$WORK/www/nosig"; cp -a "$WORK/base-nopkg-$key" "$WORK/www/nopkg"
        run_distro "$d" "$key"
    done
}

run_prune() {
    local key=$1
    say "pruning ($key): publishing beta.1..beta.10 into one channel"
    init_site prune "$key"
    for v in $V1 $V2 $PRUNE_VERSIONS; do publish prune "$key" beta "$v" prune || { fail "prune: publish $v"; return; }; done
    publish prune "$key" stable "$V0" prune || fail "prune: stable publish"
    local kept
    kept=$(find "$WORK/www/prune/rpm/beta/x86_64" -name '*.rpm' | grep -o '0\.6\.2~beta\.[0-9]*' | sort -u | tr '\n' ' ')
    local debs
    debs=$(find "$WORK/www/prune/apt/pool/beta/main/senhub-agent-oss" -name '*.deb' | grep -o '0\.6\.2~beta\.[0-9]*' | sort -u | tr '\n' ' ')
    local want="0.6.2~beta.10 0.6.2~beta.3 0.6.2~beta.4 0.6.2~beta.5 0.6.2~beta.9 "
    if [ "$kept" = "$want" ] && [ "$debs" = "$want" ]; then say "prune PASS: kept $kept(rpm and deb); stable channel kept $V0"
    else fail "prune: rpm kept '$kept', deb kept '$debs', wanted '$want'"; fi
    in_srv grep -q 'beta.1-1\|beta.2-1' /work/www/prune/apt/dists/beta/main/binary-amd64/Packages && fail "prune: Packages still lists a pruned version"
    find "$WORK/www/prune/rpm/stable/x86_64" -name '*0.6.1*' | grep -q . || fail "prune: stable lost its package"
}

# ---- main ---------------------------------------------------------------------

for v in $V0 $V1 $V2 $PRUNE_VERSIONS; do ensure_packages "$v"; done
start_server
write_client
for k in $KEYTYPES; do run_key "$k"; done
run_prune "$(echo "$KEYTYPES" | cut -d' ' -f1)"

say "key type matrix (distro x checks; PASS = accepted, - = not applicable)"
printf '%-9s %-11s %-14s %-12s %-12s\n' key distro "a:APT-Release" "b:rpm-sig" "c:repomd"
for m in "${MATRIX[@]:-}"; do [ -n "$m" ] || continue; IFS='|' read -r k d a b c <<<"$m"; printf '%-9s %-11s %-14s %-12s %-12s\n' "$k" "$d" "$a" "$b" "$c"; done
for f in "${FINDINGS[@]:-}"; do [ -n "$f" ] && echo "FINDING: $f"; done
[ $FAILED = 0 ] && echo "ALL PASS" || echo "FAILURES"
exit $FAILED
