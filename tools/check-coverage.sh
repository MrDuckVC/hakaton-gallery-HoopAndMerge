#!/bin/sh
set -eu

# Calculate from statement counts, so rounding cannot turn 59.99% into a pass.
LC_ALL=C awk '
NR == 1 { next }
{ statements += $2; if ($3 > 0) covered += $2 }
END {
    if (statements == 0) {
        print "No statements found in coverage profile" > "/dev/stderr"
        exit 1
    }
    printf "Total statement coverage: %.2f%% (minimum 60%%)\n", 100 * covered / statements
    if (100 * covered < 60 * statements) exit 1
}' "${1:-coverage.out}"
