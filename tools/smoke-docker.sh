#!/bin/sh
# Linux/CI smoke test; DATABASE_URL must refer to a dedicated test database.
set -eu
: "${DATABASE_URL:?Set DATABASE_URL to a dedicated test database}"
image=${1:-gallery-backend:local}
network=${SMOKE_DOCKER_NETWORK:-host}
container="gallery-b01-smoke-$$"
response_file=$(mktemp)
world_dir=$(mktemp -d)
cleanup() {
    docker rm --force "$container" >/dev/null 2>&1 || true
    rm -f "$response_file"
    rm -rf "$world_dir"
}
trap cleanup EXIT HUP INT TERM
# This synthetic world is only for this smoke run; never a production fallback.
go run ./tools/worldfixture --dir "$world_dir" --version smoke_v1
chmod 755 "$world_dir"


docker run --rm --network "$network" --entrypoint /migrate --env DATABASE_URL "$image" up
# Host networking is available on Linux CI; a supplied bridge also works on Docker Desktop.
set --
if [ "$network" != host ]; then set -- --publish 127.0.0.1:18080:18080; fi
docker run --detach --name "$container" --network "$network" "$@" \
    --env DATABASE_URL --env HTTP_ADDR=0.0.0.0:18080 \
    --env ALLOWED_ORIGINS=http://localhost:18080 --env COOKIE_SECURE=false \
    --mount "type=bind,source=$world_dir,target=/world,readonly" \
    --env WORLD_DIR=/world --env WORLD_VERSION=smoke_v1 --env OLLAMA_MODEL=smoke:unused "$image" >/dev/null
attempt=0
while :; do
    status=$(curl --silent --max-time 1 --output "$response_file" --write-out '%{http_code}' http://127.0.0.1:18080/api/v1/missing) || status=000
    if [ "$status" = 404 ]; then break; fi
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 50 ] || [ "$(docker inspect --format '{{.State.Running}}' "$container")" != true ]; then
        docker logs "$container"
        echo 'HTTP smoke test failed' >&2
        exit 1
    fi
    sleep 0.1
done
docker stop --time 20 "$container" >/dev/null
docker logs "$container"
[ "$(docker inspect --format '{{.State.ExitCode}}' "$container")" = 0 ]
