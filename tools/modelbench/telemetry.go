package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

type gpuMemory struct {
	TotalMiB int64 `json:"total_mib"`
	UsedMiB  int64 `json:"used_mib"`
	FreeMiB  int64 `json:"free_mib"`
}
type hostMemory struct {
	TotalKiB     int64 `json:"total_kib"`
	UsedKiB      int64 `json:"used_kib"`
	AvailableKiB int64 `json:"available_kib"`
}

func parseGPU(s string) *gpuMemory {
	fields := strings.Split(strings.TrimSpace(s), ",")
	if len(fields) != 3 {
		return nil
	}
	var nums [3]int64
	for i, f := range fields {
		n, err := strconv.ParseInt(strings.TrimSpace(f), 10, 64)
		if err != nil || n < 0 {
			return nil
		}
		nums[i] = n
	}
	return &gpuMemory{nums[0], nums[1], nums[2]}
}

func parseMemory(s string) *hostMemory {
	values := map[string]int64{}
	for line := range strings.SplitSeq(s, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			n, err := strconv.ParseInt(fields[1], 10, 64)
			if err == nil {
				values[fields[0]] = n
			}
		}
	}
	total, tok := values["MemTotal:"]
	available, aok := values["MemAvailable:"]
	if !tok || !aok || total <= 0 || available > total {
		return nil
	}
	return &hostMemory{total, total - available, available}
}

func command(ctx context.Context, name string, args ...string) []byte {
	child, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	b, err := exec.CommandContext(child, name, args...).Output()
	if err != nil {
		return nil
	}
	return b
}

// startTelemetry samples system-wide usage, not just Ollama allocations.
// Unavailable sensors remain null, and peaks are sampled lower bounds.
func startTelemetry(ctx context.Context) func(*result) {
	var mu sync.Mutex
	var peak result
	sample := func() {
		g := parseGPU(string(command(ctx, "nvidia-smi", "--query-gpu=memory.total,memory.used,memory.free", "--format=csv,noheader,nounits")))
		b, _ := os.ReadFile("/proc/meminfo")
		m := parseMemory(string(b))
		var w *hostMemory
		var win struct {
			TotalVisibleMemorySize int64
			FreePhysicalMemory     int64
		}
		b = command(ctx, "powershell.exe", "-NoProfile", "-Command", "Get-CimInstance Win32_OperatingSystem | Select-Object TotalVisibleMemorySize,FreePhysicalMemory | ConvertTo-Json -Compress")
		if json.Unmarshal(b, &win) == nil && win.TotalVisibleMemorySize > 0 {
			w = &hostMemory{win.TotalVisibleMemorySize, win.TotalVisibleMemorySize - win.FreePhysicalMemory, win.FreePhysicalMemory}
		}
		mu.Lock()
		defer mu.Unlock()
		peak.observe(g, m, w)
	}
	sample()
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				sample()
			}
		}
	}()
	return func(r *result) {
		close(stop)
		<-done
		sample()
		mu.Lock()
		defer mu.Unlock()
		r.PeakGPU = peak.PeakGPU
		r.PeakWSL = peak.PeakWSL
		r.PeakWindows = peak.PeakWindows
		r.Samples = peak.Samples
	}
}

func (r *result) observe(g *gpuMemory, m, w *hostMemory) {
	r.Samples++
	if g != nil && (r.PeakGPU == nil || g.UsedMiB > r.PeakGPU.UsedMiB) {
		r.PeakGPU = g
	}
	if m != nil && (r.PeakWSL == nil || m.UsedKiB > r.PeakWSL.UsedKiB) {
		r.PeakWSL = m
	}
	if w != nil && (r.PeakWindows == nil || w.UsedKiB > r.PeakWindows.UsedKiB) {
		r.PeakWindows = w
	}
}
