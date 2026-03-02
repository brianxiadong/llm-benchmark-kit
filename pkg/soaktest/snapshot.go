package soaktest

import (
	"strings"
	"time"

	"github.com/brianxiadong/llm-benchmark-kit/pkg/stats"
)

// WindowSnapshot holds aggregated statistics for a time window.
type WindowSnapshot struct {
	WindowIndex int       `json:"window_index"`
	StartTime   time.Time `json:"start_time"`
	EndTime     time.Time `json:"end_time"`

	// Request stats
	TotalRequests int     `json:"total_requests"`
	Success       int     `json:"success"`
	Failure       int     `json:"failure"`
	SuccessRate   float64 `json:"success_rate"`

	// TTFT stats (ms)
	AvgTTFTMs float64 `json:"avg_ttft_ms"`
	P50TTFTMs int64   `json:"p50_ttft_ms"`
	P95TTFTMs int64   `json:"p95_ttft_ms"`
	P99TTFTMs int64   `json:"p99_ttft_ms"`

	// Latency stats (ms)
	AvgLatencyMs float64 `json:"avg_latency_ms"`
	P50LatencyMs int64   `json:"p50_latency_ms"`
	P95LatencyMs int64   `json:"p95_latency_ms"`
	P99LatencyMs int64   `json:"p99_latency_ms"`

	// Throughput
	RPS             float64 `json:"rps"`
	TokenThroughput float64 `json:"token_throughput"` // tokens/s

	// Token stats
	TotalOutTokens int `json:"total_out_tokens"`
	TotalOutChars  int `json:"total_out_chars"`

	// System metrics (sampled during this window)
	SystemMetrics *SystemMetrics `json:"system_metrics,omitempty"`

	// Error summary
	ErrorCounts map[string]int `json:"error_counts,omitempty"`
}

// RequestRecord is a lightweight record for each completed request.
type RequestRecord struct {
	Timestamp    time.Time     `json:"timestamp"`
	Success      bool          `json:"success"`
	TTFT         time.Duration `json:"ttft_ns"`
	Latency      time.Duration `json:"latency_ns"`
	OutTokens    int           `json:"out_tokens"`
	OutChars     int           `json:"out_chars"`
	Error        string        `json:"error,omitempty"`
	WorkloadType string        `json:"workload_type,omitempty"` // "short" or "long"
}

// ComputeSnapshot computes a WindowSnapshot from a slice of request records.
func ComputeSnapshot(index int, start, end time.Time, records []RequestRecord, sysMetrics *SystemMetrics) WindowSnapshot {
	snap := WindowSnapshot{
		WindowIndex:   index,
		StartTime:     start,
		EndTime:       end,
		TotalRequests: len(records),
		SystemMetrics: sysMetrics,
	}

	if len(records) == 0 {
		return snap
	}

	snap.ErrorCounts = make(map[string]int)
	var ttfts, latencies []time.Duration
	for _, r := range records {
		if r.Success {
			snap.Success++
			ttfts = append(ttfts, r.TTFT)
			latencies = append(latencies, r.Latency)
			snap.TotalOutTokens += r.OutTokens
			snap.TotalOutChars += r.OutChars
		} else {
			snap.Failure++
			if r.Error != "" {
				errKey := classifyError(r.Error)
				snap.ErrorCounts[errKey]++
			}
		}
	}

	if snap.TotalRequests > 0 {
		snap.SuccessRate = float64(snap.Success) / float64(snap.TotalRequests)
	}

	wallSec := end.Sub(start).Seconds()
	if wallSec > 0 {
		snap.RPS = float64(snap.TotalRequests) / wallSec
		snap.TokenThroughput = float64(snap.TotalOutTokens) / wallSec
	}

	if len(ttfts) > 0 {
		snap.AvgTTFTMs = stats.AverageMs(ttfts)
		snap.P50TTFTMs = stats.PercentileMs(ttfts, 50)
		snap.P95TTFTMs = stats.PercentileMs(ttfts, 95)
		snap.P99TTFTMs = stats.PercentileMs(ttfts, 99)
	}

	if len(latencies) > 0 {
		snap.AvgLatencyMs = stats.AverageMs(latencies)
		snap.P50LatencyMs = stats.PercentileMs(latencies, 50)
		snap.P95LatencyMs = stats.PercentileMs(latencies, 95)
		snap.P99LatencyMs = stats.PercentileMs(latencies, 99)
	}

	// Remove empty error counts
	if len(snap.ErrorCounts) == 0 {
		snap.ErrorCounts = nil
	}

	return snap
}

// classifyError extracts a short error category from an error message.
func classifyError(errMsg string) string {
	lower := strings.ToLower(errMsg)
	switch {
	case strings.Contains(lower, "context deadline exceeded"):
		return "timeout"
	case strings.Contains(lower, "context canceled"):
		return "canceled"
	case strings.Contains(lower, "503") || strings.Contains(lower, "service unavailable"):
		return "503_service_unavailable"
	case strings.Contains(lower, "429") || strings.Contains(lower, "too many requests"):
		return "429_rate_limited"
	case strings.Contains(lower, "502") || strings.Contains(lower, "bad gateway"):
		return "502_bad_gateway"
	case strings.Contains(lower, "500") || strings.Contains(lower, "internal server error"):
		return "500_server_error"
	case strings.Contains(lower, "connection refused"):
		return "connection_refused"
	case strings.Contains(lower, "connection reset"):
		return "connection_reset"
	case strings.Contains(lower, "eof"):
		return "unexpected_eof"
	case strings.Contains(lower, "no such host"):
		return "dns_error"
	default:
		// Truncate to 80 chars for unknown errors
		if len(errMsg) > 80 {
			return errMsg[:80]
		}
		return errMsg
	}
}
