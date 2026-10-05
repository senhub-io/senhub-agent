#!/bin/sh
# Checks that the given package versions are in strictly increasing order,
# with dpkg (where it exists) or with rpm's own comparison. Run inside a
# Debian or a Fedora container by test-packages.sh.
#   vercmp-check.sh v1 v2 v3 ...   (each must sort before the next)
set -u

rc=0
while [ $# -ge 2 ]; do
    a=$1; b=$2
    if command -v dpkg >/dev/null 2>&1; then
        if dpkg --compare-versions "$a" lt "$b"; then r=-1; else r=other; fi
    else
        r=$(rpm --eval "%{lua: print(rpm.vercmp('$a', '$b'))}")
    fi
    if [ "$r" = -1 ]; then
        echo "    ok   $a < $b"
    else
        echo "    FAIL $a < $b"
        rc=1
    fi
    shift
done
exit $rc
