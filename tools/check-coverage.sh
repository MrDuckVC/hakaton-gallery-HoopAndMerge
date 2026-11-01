#!/bin/sh
set -eu

# Calculate from statement counts, so rounding cannot turn 59.99% into a pass.
LC_ALL=C awk '
NR == 1 { next }
{
    block = $1
    if (block in counts && counts[block] != $2) invalid = 1
    counts[block] = $2
    if ($3 > 0) hits[block] = 1
}
END {
    if (invalid) {
        print "Inconsistent statement counts for repeated coverage block" > "/dev/stderr"
        exit 1
    }
    for (block in counts) {
        statements += counts[block]
        if (hits[block]) covered += counts[block]
    }
    if (statements == 0) {
        print "No statements found in coverage profile" > "/dev/stderr"
        exit 1
    }
    printf "Total statement coverage: %.2f%% (minimum 60%%)\n", 100 * covered / statements
    if (100 * covered < 60 * statements) exit 1
}' "${1:-coverage.out}"
