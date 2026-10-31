#!/bin/sh
set -eu

profile=$(mktemp)
trap 'rm -f "$profile"' EXIT HUP INT TERM

check() {
    expected=$1
    shift
    printf '%s\n' 'mode: atomic' "$@" > "$profile"
    actual=0
    sh tools/check-coverage.sh "$profile" >/dev/null 2>&1 || actual=$?
    if [ "$actual" -ne "$expected" ]; then
        printf 'Coverage gate: expected exit %s, got %s\n' "$expected" "$actual" >&2
        exit 1
    fi
}

check 0 'example/main.go:1.1,2.1 3 1' 'example/main.go:3.1,4.1 2 0'
check 0 'example/main.go:1.1,2.1 1 1'
check 1 'example/main.go:1.1,2.1 5999 1' 'example/main.go:3.1,4.1 4001 0'
check 1 'example/main.go:1.1,2.1 1 0'
# -coverpkg repeats blocks across package test binaries; combine coverage by block.
check 0 'example/main.go:1.1,2.1 3 0' 'example/main.go:1.1,2.1 3 1' 'example/main.go:3.1,4.1 2 0'
check 1 'example/main.go:1.1,2.1 1 0' 'example/main.go:1.1,2.1 1 1' 'example/main.go:3.1,4.1 1 0'
check 1 'example/main.go:1.1,2.1 1 1' 'example/main.go:1.1,2.1 2 1'
check 1
printf '%s\n' 'PASS: coverage gate accepts 60%/100%, merges repeated blocks, rejects 59.99%/0%/empty/inconsistent profiles.'
