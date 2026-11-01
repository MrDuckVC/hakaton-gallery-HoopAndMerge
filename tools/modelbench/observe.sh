#!/bin/sh
# Capture system memory around an explicit opt-in command. No model downloads.
set -eu
report_dir=$1
shift
mkdir -p "$(dirname "$report_dir")"
mkdir -m 700 "$report_dir"
nvidia-smi --query-gpu=timestamp,memory.total,memory.used,memory.free \
  --format=csv,noheader,nounits --loop=1 > "$report_dir/gpu.csv" 2> "$report_dir/gpu-errors.log" &
gpu_pid=$!
(
  while :; do
    awk '/^MemTotal:/ {total=$2} /^MemAvailable:/ {available=$2} END {printf "{\"total_kib\":%d,\"available_kib\":%d}\n",total,available}' \
      /proc/meminfo >> "$report_dir/wsl.jsonl"
    powershell.exe -NoProfile -Command \
      'Get-CimInstance Win32_OperatingSystem | Select-Object TotalVisibleMemorySize,FreePhysicalMemory | ConvertTo-Json -Compress' \
      >> "$report_dir/windows.jsonl" 2> "$report_dir/windows-errors.log" || true
    sleep 1
  done
) &
ram_pid=$!
trap 'kill "$gpu_pid" "$ram_pid" 2>/dev/null || true; wait "$gpu_pid" "$ram_pid" 2>/dev/null || true' EXIT HUP INT TERM
if "$@" > "$report_dir/command.log" 2>&1; then
  status=0
else
  status=$?
fi
printf '%s\n' "$status" > "$report_dir/exit-status.txt"
printf '%s\n' "$report_dir"
exit "$status"
