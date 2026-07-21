#!/bin/sh

set -eu

profile="${1:-coverage.out}"
minimum_total="${MINIMUM_TOTAL_COVERAGE:-80}"
minimum_package="${MINIMUM_PACKAGE_COVERAGE:-60}"

awk -v minimum_total="$minimum_total" -v minimum_package="$minimum_package" '
	NR == 1 { next }
	index($1, "/internal/") == 0 { next }
	{
		file = $1
		sub(/:.*/, "", file)
		package_name = file
		sub(/\/[^\/]+$/, "", package_name)
		statements[package_name] += $2
		total_statements += $2
		if ($3 > 0) {
			covered[package_name] += $2
			total_covered += $2
		}
	}
	END {
		failed = 0
		for (package_name in statements) {
			percent = 100 * covered[package_name] / statements[package_name]
			printf "%s: %.1f%%\n", package_name, percent
			if (percent + 0.0001 < minimum_package) {
				printf "coverage error: %s is below %.1f%%\n", package_name, minimum_package > "/dev/stderr"
				failed = 1
			}
		}
		if (total_statements == 0) {
			print "coverage error: profile contains no internal package statements" > "/dev/stderr"
			exit 1
		}
		total_percent = 100 * total_covered / total_statements
		printf "internal total: %.1f%%\n", total_percent
		if (total_percent + 0.0001 < minimum_total) {
			printf "coverage error: internal total is below %.1f%%\n", minimum_total > "/dev/stderr"
			failed = 1
		}
		exit failed
	}
' "$profile"
