#!/usr/bin/env bash
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
script="$here/coverage-floor.sh"

tmp=$(mktemp -d)
cleanup() {
	if [ -n "${tmp:-}" ]; then
		rm -rf "$tmp"
	fi
}
trap cleanup EXIT

fail() {
	echo "not ok: $1" >&2
	exit 1
}

cat > "$tmp/profile" <<'EOF'
mode: set
senhub-agent.go/app/main.go:1.1,2.1 1 1
senhub-agent.go/app/main.go:3.1,4.1 1 0
EOF

cat > "$tmp/floor-pass" <<'EOF'
# comment line

TOTAL 40
app 40
EOF

out=$("$script" "$tmp/profile" "$tmp/floor-pass") || fail "case (a) should pass"
echo "$out" | grep -Fq "app  measured 50.0%  floor 40.0%" || fail "case (a) app line missing"
echo "$out" | grep -Fq "TOTAL  measured 50.0%  floor 40.0%" || fail "case (a) TOTAL line missing"
echo "ok (a) 50% package with floor 40 passes, TOTAL entry passes"

cat > "$tmp/floor-high" <<'EOF'
app 60
EOF

if out=$("$script" "$tmp/profile" "$tmp/floor-high" 2>&1); then
	fail "case (b) should exit 1"
fi
echo "$out" | grep -Fq "::error::app coverage 50.0% is below its floor 60.0%" || fail "case (b) error line missing"
echo "ok (b) floor 60 fails with exit 1 and the error line"

cat > "$tmp/profile-dup" <<'EOF'
mode: count
senhub-agent.go/app/dup.go:1.1,2.1 2 0
senhub-agent.go/app/dup.go:1.1,2.1 2 3
EOF

cat > "$tmp/floor-dup" <<'EOF'
app 100
EOF

out=$("$script" "$tmp/profile-dup" "$tmp/floor-dup") || fail "case (c) should pass"
echo "$out" | grep -Fq "app  measured 100.0%  floor 100.0%" || fail "case (c) line missing"
echo "ok (c) duplicated block covered in one line counts as covered"

cat > "$tmp/floor-missing" <<'EOF'
no/such/pkg 10
EOF

if out=$("$script" "$tmp/profile" "$tmp/floor-missing" 2>&1); then
	fail "case (d) should exit 1"
fi
echo "$out" | grep -Fq "::error::no/such/pkg: no coverage data" || fail "case (d) error line missing"
echo "ok (d) floor entry for a missing package fails"

if out=$("$script" "$tmp/missing-profile" "$tmp/floor-pass" 2>&1); then
	fail "case (e) should exit 1"
fi
echo "$out" | grep -Fq "::error::$tmp/missing-profile: missing or empty coverage profile" || fail "case (e) error line missing"
echo "ok (e) missing profile fails"

cat > "$tmp/floor-malformed" <<'EOF'
app abc
EOF

if out=$("$script" "$tmp/profile" "$tmp/floor-malformed" 2>&1); then
	fail "case (f) should exit 1"
fi
echo "$out" | grep -Fq "::error::$tmp/floor-malformed: malformed line: app abc" || fail "case (f) error line missing"
echo "ok (f) malformed floor line fails"

cat > "$tmp/floor-lowered" <<'EOF'
app 30
EOF

cat > "$tmp/base-floor-lowered" <<'EOF'
app 40
EOF

if out=$("$script" "$tmp/profile" "$tmp/floor-lowered" "$tmp/base-floor-lowered" 2>&1); then
	fail "case (g) should exit 1"
fi
echo "$out" | grep -Fq "::error::app: floor lowered from 40.0% to 30.0%; a floor only moves up" || fail "case (g) error line missing"
echo "ok (g) lowering a floor from the base fails"

cat > "$tmp/floor-raised" <<'EOF'
app 45
EOF

cat > "$tmp/base-floor-raised" <<'EOF'
app 40
EOF

out=$("$script" "$tmp/profile" "$tmp/floor-raised" "$tmp/base-floor-raised") || fail "case (h) should pass"
echo "$out" | grep -Fq "app  measured 50.0%  floor 45.0%" || fail "case (h) app line missing"
echo "ok (h) raising a floor from the base passes"

cat > "$tmp/floor-removed" <<'EOF'
TOTAL 10
EOF

cat > "$tmp/base-floor-removed" <<'EOF'
app 40
EOF

if out=$("$script" "$tmp/profile" "$tmp/floor-removed" "$tmp/base-floor-removed" 2>&1); then
	fail "case (i) should exit 1"
fi
echo "$out" | grep -Fq "::error::app: its floor was removed; a floor only moves up" || fail "case (i) error line missing"
echo "ok (i) removing a base floor entry fails"
