#!/bin/sh
# Online preparation only. Source checkout stays outside the backend repository.
set -eu
repository=https://github.com/ManInTheCoat/Hope-Merge-Frontend.git
revision=c7dbfdc62057fabf06d50bce2a0c3e8e0f386150
output=${1:?Usage: sh tools/build-frontend.sh NEW_DIST_DIRECTORY}
if [ -e "$output" ] || [ -L "$output" ]; then
    echo 'Output already exists; choose a new dist directory.' >&2
    exit 1
fi
checkout=$(mktemp -d)
trap 'rm -rf "$checkout"' EXIT HUP INT TERM
git -C "$checkout" init --quiet
git -C "$checkout" remote add origin "$repository"
git -C "$checkout" fetch --depth 1 origin "$revision"
git -C "$checkout" checkout --quiet --detach FETCH_HEAD
(
    cd "$checkout"
    bun install --frozen-lockfile
    VITE_USE_MOCK=false bun run build
)
# Provenance is public build metadata, not configuration or secrets.
printf '{"repository":"%s","branch":"main","commit":"%s","mock":false}\n' \
    "$repository" "$revision" > "$checkout/dist/build-info.json"
mkdir -p "$(dirname "$output")"
mkdir "$output"
cp -R "$checkout/dist/." "$output/"
printf 'Frontend %s built into %s\n' "$revision" "$output"
