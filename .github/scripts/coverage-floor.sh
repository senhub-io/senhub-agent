#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -lt 2 ] || [ "$#" -gt 3 ]; then
	echo "usage: $0 <coverprofile> <floor-file> [<base-floor-file>]" >&2
	exit 1
fi

coverprofile=$1
floor_file=$2
base_arg=/dev/null
base_var=

if [ "$#" -eq 3 ] && [ -e "$3" ]; then
	base_arg=$3
	base_var=$3
fi

if [ ! -s "$coverprofile" ]; then
	echo "::error::$coverprofile: missing or empty coverage profile"
	exit 1
fi

awk -v floor_file="$floor_file" -v base_file="$base_var" '
BEGIN {
	fail = 0
}
FILENAME == floor_file {
	line = $0
	sub(/#.*/, "", line)
	gsub(/^[ \t]+/, "", line)
	gsub(/[ \t]+$/, "", line)
	if (line == "")
		next
	if (split(line, fld, /[ \t]+/) != 2 || fld[2] !~ /^[0-9]+(\.[0-9]+)?$/) {
		printf "::error::%s: malformed line: %s\n", floor_file, line
		fail = 1
		next
	}
	n++
	names[n] = fld[1]
	floors[fld[1]] = fld[2] + 0
	next
}
base_file != "" && FILENAME == base_file {
	line = $0
	sub(/#.*/, "", line)
	gsub(/^[ \t]+/, "", line)
	gsub(/[ \t]+$/, "", line)
	if (line == "")
		next
	if (split(line, fld, /[ \t]+/) != 2 || fld[2] !~ /^[0-9]+(\.[0-9]+)?$/) {
		printf "::error::%s: malformed line: %s\n", base_file, line
		fail = 1
		next
	}
	basefloor[fld[1]] = fld[2] + 0
	next
}
/^mode:/ { next }
NF < 3 { next }
{
	key = $1
	stmts = $2 + 0
	cnt = $3 + 0
	file = $1
	sub(/^senhub-agent.go\//, "", file)
	pkg = file
	if (index(pkg, "/") > 0)
		sub(/\/[^\/]*$/, "", pkg)
	else
		pkg = "."
	if (!(key in seen)) {
		seen[key] = 1
		total[pkg] += stmts
		total["TOTAL"] += stmts
		if (cnt > 0) {
			cov[key] = 1
			covered[pkg] += stmts
			covered["TOTAL"] += stmts
		}
	} else if (cnt > 0 && !(key in cov)) {
		cov[key] = 1
		covered[pkg] += stmts
		covered["TOTAL"] += stmts
	}
	next
}
END {
	for (i = 1; i <= n; i++) {
		name = names[i]
		if (!(name in total)) {
			printf "::error::%s: no coverage data\n", name
			fail = 1
			continue
		}
		m = covered[name] * 100.0 / total[name]
		f = floors[name]
		printf "%s  measured %.1f%%  floor %.1f%%\n", name, m, f
		if (m < f) {
			printf "::error::%s coverage %.1f%% is below its floor %.1f%%\n", name, m, f
			fail = 1
		} else if (m >= f + 1.0) {
			printf "::notice::%s is at %.1f%%, %.1f%% in the floor: raise the floor in %s\n", name, m, f, floor_file
		}
	}
	for (name in basefloor) {
		if (!(name in floors)) {
			printf "::error::%s: its floor was removed; a floor only moves up\n", name
			fail = 1
		} else if (floors[name] < basefloor[name]) {
			printf "::error::%s: floor lowered from %.1f%% to %.1f%%; a floor only moves up\n", name, basefloor[name], floors[name]
			fail = 1
		}
	}
	exit fail
}
' "$floor_file" "$base_arg" "$coverprofile"
