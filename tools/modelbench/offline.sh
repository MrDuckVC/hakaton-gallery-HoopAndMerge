#!/bin/sh
set -eu
# Run from the repository root using unshare --user --map-root-user --net.
repo_root=$PWD
if [ "$(ip -o link show | awk -F': ' '{print $2}')" != lo ]; then
 echo 'Refusing: expected an isolated network namespace containing only lo.' >&2
 exit 1
fi
if [ ! -x bin/modelbench ] || [ ! -x bin/ollama-live.test ]; then
 echo 'Build bin/modelbench and bin/ollama-live.test before entering the namespace.' >&2
 exit 1
fi
report_dir=${A01_REPORT_DIR:-data/model-benchmark/offline-$(date -u +%Y%m%dT%H%M%SZ)}
mkdir -p "$(dirname "$report_dir")"
mkdir -m 700 "$report_dir"
case "$report_dir" in /*) ;; *) report_dir="$repo_root/$report_dir" ;; esac

ip link set lo up
ip -j address > "$report_dir"/offline-interfaces.json
ip -j route > "$report_dir"/offline-routes.json
ip -6 -j route > "$report_dir"/offline-routes6.json
if curl --noproxy '*' --connect-timeout 2 --max-time 3 -fsS https://ollama.com > /dev/null 2> "$report_dir"/offline-external-check.log; then
  echo 'Unexpected external connectivity' >&2
  exit 1
fi
export OLLAMA_MODELS="$repo_root/data/ollama/models"
export OLLAMA_HOST=127.0.0.1:11434
export OLLAMA_NO_CLOUD=1
export OLLAMA_NUM_PARALLEL=1
export OLLAMA_MAX_LOADED_MODELS=1
"$repo_root/bin/ollama-runtime/bin/ollama" serve > "$report_dir"/offline-ollama.log 2>&1 &
a01_server_pid=$!
trap 'kill "$a01_server_pid" 2>/dev/null || true; wait "$a01_server_pid" 2>/dev/null || true' EXIT HUP INT TERM
for attempt in 1 2 3 4 5 6 7 8 9 10; do
 kill -0 "$a01_server_pid"
 if curl --max-time 2 -fsS http://127.0.0.1:11434/api/version > "$report_dir"/offline-version.json; then break; fi
 sleep 1
done
for tag in gallery-guide-main:a01 gallery-guide-reserve:a01; do
 label=$(printf '%s' "$tag" | tr ':' '-')
 (cd internal/adapters/outbound/ollama; B08_OLLAMA_MODEL="$tag" B08_OLLAMA_URL=http://127.0.0.1:11434 "$repo_root/bin/ollama-live.test" -test.run '^TestLocalModelFixtures$' -test.v -test.timeout 10m) > "$report_dir/offline-fixture-$label.log" 2>&1
 "$repo_root/bin/modelbench" -model "$tag" -phase offline-namespace-scene -repeats 1 -answers "$report_dir/offline-$label-answers.jsonl" > "$report_dir/offline-$label.jsonl"
 curl -fsS http://127.0.0.1:11434/api/ps > "$report_dir/offline-ps-$label.json"
 if [ ! -s "$report_dir/offline-$label.jsonl" ] || grep -qv '"status":"completed"' "$report_dir/offline-$label.jsonl"; then
  echo 'Offline model benchmark contains failures; inspect report.' >&2
  exit 1
 fi
 "$repo_root/bin/ollama-runtime/bin/ollama" stop "$tag" > /dev/null
 done
printf '%s\n' 'PASS: both models answered with only loopback and no external routes.' > "$report_dir/result.txt"
printf '%s\n' "$report_dir"
