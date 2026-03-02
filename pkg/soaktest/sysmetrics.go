package soaktest

import (
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// SystemMetrics holds a snapshot of system resource usage.
type SystemMetrics struct {
	Timestamp  time.Time `json:"timestamp"`
	CPUPercent float64   `json:"cpu_percent"`
	MemUsedMB  float64   `json:"mem_used_mb"`
	MemTotalMB float64   `json:"mem_total_mb"`
	MemPercent float64   `json:"mem_percent"`
	GPUMetrics []GPUInfo `json:"gpu_metrics,omitempty"`
}

// GPUInfo holds metrics for a single GPU.
type GPUInfo struct {
	Index       int     `json:"index"`
	Name        string  `json:"name"`
	UtilPercent float64 `json:"util_percent"`
	MemUsedMB   float64 `json:"mem_used_mb"`
	MemTotalMB  float64 `json:"mem_total_mb"`
	MemPercent  float64 `json:"mem_percent"`
	TempC       float64 `json:"temp_c"`
	PowerW      float64 `json:"power_w"`
}

// CollectSystemMetrics gathers current system resource metrics.
func CollectSystemMetrics() SystemMetrics {
	m := SystemMetrics{
		Timestamp: time.Now(),
	}

	switch runtime.GOOS {
	case "linux":
		m.CPUPercent = getLinuxCPU()
		m.MemUsedMB, m.MemTotalMB, m.MemPercent = getLinuxMemory()
	case "darwin":
		m.CPUPercent = getDarwinCPU()
		m.MemUsedMB, m.MemTotalMB, m.MemPercent = getDarwinMemory()
	}

	m.GPUMetrics = getNvidiaGPUMetrics()
	return m
}

func getLinuxCPU() float64 {
	out, err := exec.Command("sh", "-c",
		`grep 'cpu ' /proc/stat | awk '{usage=($2+$4)*100/($2+$4+$5)} END {print usage}'`).Output()
	if err != nil {
		return 0
	}
	val, _ := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	return val
}

func getDarwinCPU() float64 {
	out, err := exec.Command("sh", "-c",
		`top -l 1 -n 0 | grep "CPU usage" | awk '{print $3}' | tr -d '%'`).Output()
	if err != nil {
		return 0
	}
	val, _ := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	return val
}

func getLinuxMemory() (usedMB, totalMB, percent float64) {
	out, err := exec.Command("sh", "-c",
		`free -m | grep Mem | awk '{print $2, $3}'`).Output()
	if err != nil {
		return 0, 0, 0
	}
	parts := strings.Fields(strings.TrimSpace(string(out)))
	if len(parts) >= 2 {
		totalMB, _ = strconv.ParseFloat(parts[0], 64)
		usedMB, _ = strconv.ParseFloat(parts[1], 64)
		if totalMB > 0 {
			percent = usedMB / totalMB * 100
		}
	}
	return
}

func getDarwinMemory() (usedMB, totalMB, percent float64) {
	out, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
	if err == nil {
		bytes, _ := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
		totalMB = bytes / 1024 / 1024
	}

	out, err = exec.Command("sh", "-c",
		`vm_stat | awk '/Pages active|Pages wired/ {sum += $NF} END {print sum}'`).Output()
	if err == nil {
		pages, _ := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
		usedMB = pages * 4096 / 1024 / 1024
	}

	if totalMB > 0 {
		percent = usedMB / totalMB * 100
	}
	return
}

func getNvidiaGPUMetrics() []GPUInfo {
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		return nil
	}

	out, err := exec.Command("nvidia-smi",
		"--query-gpu=index,name,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw",
		"--format=csv,noheader,nounits").Output()
	if err != nil {
		return nil
	}

	var gpus []GPUInfo
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, ", ")
		if len(parts) < 7 {
			continue
		}

		idx, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
		util, _ := strconv.ParseFloat(strings.TrimSpace(parts[2]), 64)
		memUsed, _ := strconv.ParseFloat(strings.TrimSpace(parts[3]), 64)
		memTotal, _ := strconv.ParseFloat(strings.TrimSpace(parts[4]), 64)
		temp, _ := strconv.ParseFloat(strings.TrimSpace(parts[5]), 64)
		power, _ := strconv.ParseFloat(strings.TrimSpace(parts[6]), 64)

		var memPct float64
		if memTotal > 0 {
			memPct = memUsed / memTotal * 100
		}

		gpus = append(gpus, GPUInfo{
			Index:       idx,
			Name:        strings.TrimSpace(parts[1]),
			UtilPercent: util,
			MemUsedMB:   memUsed,
			MemTotalMB:  memTotal,
			MemPercent:  memPct,
			TempC:       temp,
			PowerW:      power,
		})
	}

	return gpus
}

// FormatMetrics returns a human-readable string of system metrics.
func FormatMetrics(m SystemMetrics) string {
	s := fmt.Sprintf("CPU: %.1f%% | Mem: %.0f/%.0fMB (%.1f%%)",
		m.CPUPercent, m.MemUsedMB, m.MemTotalMB, m.MemPercent)

	for _, gpu := range m.GPUMetrics {
		s += fmt.Sprintf(" | GPU%d: %.0f%% Mem: %.0f/%.0fMB (%.1f%%) Temp: %.0f°C Power: %.0fW",
			gpu.Index, gpu.UtilPercent, gpu.MemUsedMB, gpu.MemTotalMB, gpu.MemPercent, gpu.TempC, gpu.PowerW)
	}

	return s
}
