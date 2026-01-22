// Package result defines result and report types.
package result

import "time"

// RequestStatus represents the status of a benchmark request.
type RequestStatus string

const (
	StatusOK         RequestStatus = "ok"
	StatusHTTPError  RequestStatus = "http_error"
	StatusTimeout    RequestStatus = "timeout"
	StatusParseError RequestStatus = "parse_error"
)

// RequestResult holds the result of a single benchmark request.
type RequestResult struct {
	ID        string        `json:"id"`
	Status    RequestStatus `json:"status"`
	TTFT      time.Duration `json:"ttft_ns"`       // Time to first token
	Latency   time.Duration `json:"latency_ns"`    // Total request latency
	Decode    time.Duration `json:"decode_ns"`     // Decode time (end - first_content)
	OutTokens int           `json:"out_tokens"`    // Output token count
	OutChars  int           `json:"out_chars"`     // Output character count
	Err       string        `json:"err,omitempty"` // Error message if failed

	// Internal timestamps
	StartTime        time.Time `json:"-"`
	FirstContentTime time.Time `json:"-"`
	EndTime          time.Time `json:"-"`

	// Sampling
	FirstContentRaw string   `json:"-"` // First content frame raw data
	MiddleFramesRaw []string `json:"-"` // Middle content frames raw data
	FinalFrameRaw   string   `json:"-"` // Final frame raw data
}

// IsSuccess returns true if the request was successful.
func (r *RequestResult) IsSuccess() bool {
	return r.Status == StatusOK
}

// ErrorStat holds error statistics.
type ErrorStat struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

// BenchmarkReport holds the aggregated benchmark results.
type BenchmarkReport struct {
	// Metadata
	Provider   string `json:"provider"`
	Model      string `json:"model"`
	StartedAt  string `json:"started_at"`
	WallTimeMs int64  `json:"wall_time_ms"`

	// Request Counts
	TotalRequests int     `json:"total_requests"`
	Success       int     `json:"success"`
	Failure       int     `json:"failure"`
	SuccessRate   float64 `json:"success_rate"`

	// TTFT Statistics (milliseconds)
	AvgTTFTMs float64 `json:"avg_ttft_ms"`
	P50TTFTMs int64   `json:"p50_ttft_ms"`
	P95TTFTMs int64   `json:"p95_ttft_ms"`
	P99TTFTMs int64   `json:"p99_ttft_ms"`

	// Latency Statistics (milliseconds)
	AvgLatencyMs float64 `json:"avg_latency_ms"`
	P50LatencyMs int64   `json:"p50_latency_ms"`
	P95LatencyMs int64   `json:"p95_latency_ms"`
	P99LatencyMs int64   `json:"p99_latency_ms"`

	// Throughput
	TokenMode       string  `json:"token_mode"` // usage|chars|disabled
	TokenThroughput float64 `json:"token_throughput"`
	RPS             float64 `json:"rps"`

	// Sampling
	FirstContentRaw string   `json:"first_content_raw,omitempty"`
	MiddleFramesRaw []string `json:"middle_frames_raw,omitempty"`
	FinalFrameRaw   string   `json:"final_frame_raw,omitempty"`

	// Error Breakdown
	ErrorsTopN []ErrorStat `json:"errors_top_n,omitempty"`

	// Raw data for visualization
	TTFTDistribution    []int64 `json:"ttft_distribution_ms,omitempty"`
	LatencyDistribution []int64 `json:"latency_distribution_ms,omitempty"`
}
